package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/mvanhorn/agentcookie/internal/chrome"
	"github.com/mvanhorn/agentcookie/internal/chromedirsync"
	"github.com/mvanhorn/agentcookie/internal/cli/httpserver"
	"github.com/mvanhorn/agentcookie/internal/config"
	"github.com/mvanhorn/agentcookie/internal/pairing"
	"github.com/mvanhorn/agentcookie/internal/protocol"
	"github.com/mvanhorn/agentcookie/internal/secretsbus"
	"github.com/mvanhorn/agentcookie/internal/state"
	"github.com/mvanhorn/agentcookie/internal/transport"
	"github.com/mvanhorn/agentcookie/internal/tsclient"
	"github.com/mvanhorn/agentcookie/internal/watcher"
)

var (
	sourceOnce     bool
	sourceWatch    bool
	sourceVerbose  bool
	sourceDryRun   bool
	sourceSkipDBSC bool
	sourceTargets  []string
)

// resolveSinkURL is the sink URL resolver used by pushOnce. Production
// wires it to tsclient.ResolveSinkURL; tests can override it to inject
// specific resolution behaviors (e.g., ErrAmbiguousPeer).
var resolveSinkURL = tsclient.ResolveSinkURL

// SetResolveSinkURLForTesting replaces resolveSinkURL with the given
// function and returns a restore func. Test-only seam.
func SetResolveSinkURLForTesting(f func(ctx context.Context, rawURL string) (string, error)) func() {
	prev := resolveSinkURL
	resolveSinkURL = f
	return func() { resolveSinkURL = prev }
}

type sourcePushTarget struct {
	Name   string
	URL    string
	Secret string
	Policy *config.Blocklist
}

// dbscSummary carries the DBSC-suspect tally from one push back to the caller
// so it can be recorded in SourceState for `doctor` / `status`.
type dbscSummary struct {
	warned  int
	skipped int
	sample  []string
}

var sourceCmd = &cobra.Command{
	Use:   "source",
	Short: "Read local browser cookies, apply policy, and push to configured sinks",
	Long: `Two modes:

  agentcookie source --once   one read+push cycle, then exit. Useful for cron
                              and CI. The legacy v0.1 mode.

  agentcookie source --watch  long-running. fsnotify watches Chrome's Cookies
                              SQLite for write events; on change, debounces
                              500ms and runs a push. Rate-capped at one push
                              every 2 seconds even under continuous Chrome
                              activity. This is the v0.2 default mode and the
                              one a LaunchAgent should run.`,
	RunE: runSource,
}

func init() {
	sourceCmd.Flags().BoolVar(&sourceOnce, "once", false, "single read+push cycle, then exit")
	sourceCmd.Flags().BoolVar(&sourceWatch, "watch", false, "long-running fsnotify watcher; pushes on every Chrome cookie write (debounced)")
	sourceCmd.Flags().BoolVar(&sourceVerbose, "verbose", false, "log per-pattern decisions to stderr")
	sourceCmd.Flags().BoolVar(&sourceDryRun, "dry-run", false, "read + filter but do not contact the sink")
	sourceCmd.Flags().BoolVar(&sourceSkipDBSC, "skip-dbsc-suspect", false, "drop cookies that look device-bound (DBSC) instead of shipping them with a warning; also honored via AGENTCOOKIE_SKIP_DBSC_SUSPECT=1")
	sourceCmd.Flags().StringSliceVar(&sourceTargets, "target", nil, "push only to named target(s) from source.yaml targets (repeat or comma-separate)")
}

func runSource(cmd *cobra.Command, args []string) error {
	if !sourceOnce && !sourceWatch {
		return fmt.Errorf("pass either --once for a single pass or --watch for the long-running watcher")
	}
	if sourceOnce && sourceWatch {
		return fmt.Errorf("--once and --watch are mutually exclusive")
	}

	cfg, err := config.LoadSource(common.ConfigDir)
	if err != nil {
		return err
	}
	// v0.3: sync-all by default. Blocklist is optional; missing file is fine.
	// Explicit policy: allowlist still reloads per push below.
	// Fail fast on a broken file at startup, then reload again for each push.
	if _, err := loadFreshBlocklist(); err != nil {
		return err
	}

	sourceBrowser, err := chrome.LookupBrowser(cfg.Browser.Name)
	if err != nil {
		return err
	}
	password, err := chrome.SafeStoragePasswordFor(sourceBrowser)
	if err != nil {
		// SafeStoragePasswordFor already prefixes its error with
		// "read <service> from Keychain ..."; don't double the prefix.
		return err
	}
	key, err := chrome.DeriveAESKey(password)
	if err != nil {
		return err
	}
	configuredTargets, err := cfg.SelectSourceTargets(sourceTargets)
	if err != nil {
		return err
	}
	pushTargets := make([]sourcePushTarget, 0, len(configuredTargets))
	for _, target := range configuredTargets {
		legacySecret := ""
		if target.Name == "legacy" {
			legacySecret = cfg.Security.SharedSecret
		}
		secret, err := resolveTransportSecret(common.ConfigDir, target.Peer, legacySecret)
		if err != nil {
			return fmt.Errorf("target %s: %w", target.Name, err)
		}
		pushTargets = append(pushTargets, sourcePushTarget{Name: target.Name, URL: target.URL, Secret: secret, Policy: target.Policy})
	}

	// State writer for `agentcookie status` to read.
	home, _ := os.UserHomeDir()
	stateWriter := state.NewWriter(state.SourcePath(home))
	srcState := &state.SourceState{Role: "source", SinkURL: fmt.Sprintf("%d target(s)", len(pushTargets))}

	// --skip-dbsc-suspect is also honored via env var so a LaunchAgent can
	// opt in without a flag edit.
	skipDBSC := sourceSkipDBSC || os.Getenv("AGENTCOOKIE_SKIP_DBSC_SUSPECT") == "1"

	push := func(ctx context.Context) (int, error) {
		return pushWithFreshBlocklist(ctx, cfg, key, pushTargets, sourceDryRun, sourceVerbose, skipDBSC, srcState, stateWriter)
	}

	if sourceOnce {
		// --once mode: bound the whole push by SyncClient's timeout
		// plus a small slack for envelope packing. Pre-v0.12 this was
		// hardcoded at 60s, which was tight even for v0.10-shape
		// payloads. The inner HTTP request also bounds itself; this
		// outer cancel is the belt to the request's suspenders.
		ctx, cancel := context.WithTimeout(cmd.Context(), httpserver.Defaults(httpserver.SyncClient).ClientTimeout+30*time.Second)
		defer cancel()
		_, err := push(ctx)
		return err
	}

	// --watch mode: long-running fsnotify watcher across all three sync
	// surfaces (cookies + Local Storage + IndexedDB). v0.7 single debounce
	// window: a write to any surface coalesces into one full envelope push.
	w, err := watcher.New(watcher.Config{
		CookiesPath:     cfg.Chrome.DBPath,
		LocalStorageDir: sourceBrowser.LocalStorageLevelDB(cfg.Browser.Profile),
		IndexedDBDir:    sourceBrowser.IndexedDBDir(cfg.Browser.Profile),
		Push:            push,
		OnEvent: func(ev watcher.Event) {
			if sourceVerbose {
				fmt.Fprintf(os.Stderr, "agentcookie source --watch: %s\n", ev.String())
			}
		},
	})
	if err != nil {
		return fmt.Errorf("init watcher: %w", err)
	}
	targetNames := make([]string, 0, len(pushTargets))
	for _, target := range pushTargets {
		targetNames = append(targetNames, target.Name)
	}
	fmt.Fprintf(os.Stderr, "agentcookie source --watch: adapter=%s watching %s, targets=%v\n", sourceBrowser.Name, cfg.Chrome.DBPath, targetNames)

	// v0.13: also watch ~/.agentcookie/secrets/ so a write to a per-CLI
	// secrets.env triggers the same push pipeline as a Chrome cookie
	// change. The secrets watcher tolerates a missing root (waits for the
	// friend to create it) and fires the same push callback as the
	// cookies watcher so the payload includes whichever surface changed.
	watchHome, _ := os.UserHomeDir()
	secretsWatcher := secretsbus.NewWatcher(watchHome, 0, func(ctx context.Context) {
		_, _ = push(ctx)
	})
	go func() {
		if err := secretsWatcher.Run(cmd.Context()); err != nil {
			fmt.Fprintf(os.Stderr, "agentcookie source --watch: secrets-bus watcher exited: %v\n", err)
		}
	}()

	// v0.14: also watch the v2 discovery paths (~/.agentcookie/manifests/
	// + PP library) so dropping a new agentcookie.toml or regenerating a
	// PP CLI triggers a push without restart.
	discoveryWatcher := secretsbus.NewDiscoveryWatcher(
		secretsbus.DiscoveryConfig{HomeDir: watchHome},
		0,
		func(ctx context.Context, delta secretsbus.RegistryDelta, _ *secretsbus.Registry) {
			if sourceVerbose && (len(delta.Added)+len(delta.Removed) > 0) {
				fmt.Fprintf(os.Stderr, "agentcookie source --watch: discovery: added=%v removed=%v\n", delta.Added, delta.Removed)
			}
			_, _ = push(ctx)
		},
	)
	go func() {
		if err := discoveryWatcher.Run(cmd.Context()); err != nil {
			fmt.Fprintf(os.Stderr, "agentcookie source --watch: discovery watcher exited: %v\n", err)
		}
	}()

	return w.Run(cmd.Context())
}

func pushWithFreshBlocklist(
	ctx context.Context,
	cfg *config.SourceConfig,
	key []byte,
	targets []sourcePushTarget,
	dryRun bool,
	verbose bool,
	skipDBSC bool,
	srcState *state.SourceState,
	stateWriter *state.Writer,
) (int, error) {
	blocklist, err := loadFreshBlocklist()
	var dbsc dbscSummary
	if err != nil {
		recordSourcePushResult(srcState, stateWriter, 0, dbsc, err)
		return 0, err
	}
	n, dbsc, err := pushOnce(ctx, cfg, blocklist, key, targets, dryRun, verbose, skipDBSC)
	recordSourcePushResult(srcState, stateWriter, n, dbsc, err)
	return n, err
}

func recordSourcePushResult(
	srcState *state.SourceState,
	stateWriter *state.Writer,
	n int,
	dbsc dbscSummary,
	err error,
) {
	if srcState == nil {
		return
	}
	if err != nil {
		srcState.TotalFailures++
		srcState.LastError = err.Error()
		srcState.LastErrorAt = time.Now().UTC()
	} else {
		srcState.TotalPushes++
		srcState.LastPushCount = n
		srcState.LastPush = time.Now().UTC()
	}
	srcState.LastDBSCWarned = dbsc.warned
	srcState.LastDBSCSkipped = dbsc.skipped
	srcState.LastDBSCSample = dbsc.sample
	if stateWriter != nil {
		_ = stateWriter.Save(srcState)
	}
}

// pushOnce performs one read+filter+push cycle. Returns the number of cookies
// successfully posted (0 on dry-run or error).
//
// v0.3 reads ALL cookies from Chrome in one pass (pattern '%') then applies
// the cookie policy matcher to drop disallowed hosts. Missing or empty
// blocklist-mode config preserves legacy sync-all behavior.
func pushOnce(
	ctx context.Context,
	cfg *config.SourceConfig,
	blocklist *config.Blocklist,
	key []byte,
	targets []sourcePushTarget,
	dryRun bool,
	verbose bool,
	skipDBSC bool,
) (int, dbscSummary, error) {
	var dbsc dbscSummary

	// Shared read pipeline (decrypt -> cookie policy -> DBSC). See
	// readFilteredCookies in cookie_pipeline.go; `source` and `cmux-sync`
	// both use it so they filter identically.
	all, st, err := readFilteredCookies(cfg.Chrome.DBPath, blocklist, key, skipDBSC, time.Now().UTC())
	if err != nil {
		return 0, dbsc, err
	}
	totalRead := st.totalRead
	totalDropped := st.totalDropped
	droppedHosts := st.droppedHosts
	dbsc = st.dbsc
	if verbose {
		fmt.Fprintf(os.Stderr, "agentcookie source: read %d cookies, filtered %d on %d hosts, passing %d\n",
			totalRead, totalDropped, len(droppedHosts), len(all))
	}

	// Only print the DBSC detail block under --verbose: in --watch mode this
	// fires on every cookie change and would flood the LaunchAgent log for
	// any user with a persistent Google cookie. The durable signal lives in
	// `agentcookie doctor` (source-state.json) and the JSON result map; the
	// per-push human summary below carries a concise count.
	if verbose {
		if n := dbsc.warned + dbsc.skipped; n > 0 {
			verb := "shipping with a warning"
			if skipDBSC {
				verb = "skipping"
			}
			fmt.Fprintf(os.Stderr, "agentcookie source: %d cookie(s) look device-bound (DBSC); %s. These likely will not work on the sink. See README: DBSC.\n", n, verb)
			for _, r := range dbsc.sample {
				fmt.Fprintf(os.Stderr, "  - %s\n", r)
			}
		}
	}

	cookiesOnly := os.Getenv("AGENTCOOKIE_COOKIES_ONLY") == "1"

	// v0.14: combined v1 bus + v2 discovery. A cookies-only source must not
	// even read the secrets bus; it is a capability boundary, not just a
	// smaller wire payload.
	home, _ := os.UserHomeDir()
	var secretsPayload *secretsbus.Payload
	var secretsErrs []error
	if cookiesOnly {
		fmt.Fprintln(os.Stderr, "agentcookie source: AGENTCOOKIE_COOKIES_ONLY=1; skipping secrets bus")
	} else {
		secretsPayload, secretsErrs = secretsbus.LoadPayloadWithDiscovery(home)
	}
	for _, e := range secretsErrs {
		fmt.Fprintf(os.Stderr, "agentcookie source: secrets-bus: %v\n", e)
	}
	secretsCLICount := 0
	if secretsPayload != nil {
		secretsCLICount = len(secretsPayload.CLIs)
	}
	if verbose && secretsCLICount > 0 {
		fmt.Fprintf(os.Stderr, "agentcookie source: secrets-bus: shipping %d cli(s)\n", secretsCLICount)
	}

	result := map[string]any{
		"cookies_read":         totalRead,
		"cookies_blocked":      totalDropped,
		"cookies_filtered":     totalDropped,
		"cookie_policy":        blocklist.CookiePolicySummary(),
		"cookies_passing":      len(all),
		"cookies_dbsc_warned":  dbsc.warned,
		"cookies_dbsc_skipped": dbsc.skipped,
		"secrets_clis":         secretsCLICount,
		"dry_run":              dryRun,
		"target_count":         len(targets),
		"posted":               false,
	}

	if dryRun || (len(all) == 0 && secretsCLICount == 0) {
		_ = emit(result, fmt.Sprintf("agentcookie source: %d cookies after cookie policy (%s), %d secrets clis (dry-run=%v)%s\n", len(all), blocklist.CookiePolicySummary(), secretsCLICount, dryRun, dbscNote(dbsc)))
		return 0, dbsc, nil
	}

	// The source may also be a consumer: Dia can remain the authority while
	// this machine's ordinary Google Chrome receives the same live identity.
	// This path attaches to Chrome's user-approved loopback endpoint and never
	// edits Chrome SQLite.
	var localChromeErr error
	if cfg.RealChrome.Enabled && len(all) > 0 {
		localResult, injectErr := injectConfiguredRealChrome(ctx, cfg.RealChrome, all)
		result["real_chrome"] = map[string]any{
			"enabled":          true,
			"mode":             localResult.Mode,
			"cookies":          localResult.Cookies,
			"port":             localResult.Port,
			"approval_clicked": localResult.ApprovalClicked,
			"restarted":        localResult.Restarted,
		}
		if injectErr != nil {
			localChromeErr = fmt.Errorf("ordinary Chrome injection: %w", injectErr)
			result["real_chrome_error"] = injectErr.Error()
			fmt.Fprintf(os.Stderr, "agentcookie source: ordinary Chrome injection failed; remote targets will still be attempted: %v\n", injectErr)
		} else if !common.JSON {
			fmt.Fprintf(os.Stderr, "agentcookie source: injected %d cookies into ordinary Chrome (mode=%s, restarted=%v)\n", localResult.Cookies, localResult.Mode, localResult.Restarted)
		}
	}

	// v0.7: pack Local Storage and IndexedDB alongside cookies from the
	// configured source browser/profile. The envelope carries the bytes, the
	// sink unpacks into its real Chrome profile. Errors fetching either are
	// non-fatal so the source still pushes whatever it could read.
	// Resolve the same adapter the watcher uses. cfg.Browser.Name was already
	// validated in LoadSource, so a failure here means the config changed
	// underneath us; fail loud rather than silently packing Chrome's profile
	// (which would mismatch the cookies/localStorage/IndexedDB the watcher and
	// the rest of this push are reading from the configured browser).
	sourceBrowser, err := chrome.LookupBrowser(cfg.Browser.Name)
	if err != nil {
		return 0, dbsc, err
	}
	var lsTarball []byte
	var idbTarball []byte
	var idbSkipped []string
	// IndexedDB is opt-in (typical dirs are 400MB+). Local Storage is packed
	// by default, but a busy Dia/Chrome profile can still be tens of MB and
	// blow the Tailscale POST. AGENTCOOKIE_COOKIES_ONLY=1 skips both.
	if cookiesOnly {
		fmt.Fprintln(os.Stderr, "agentcookie source: AGENTCOOKIE_COOKIES_ONLY=1; skipping localStorage and indexedDB")
	} else if lt, _, err := chromedirsync.Pack(sourceBrowser.LocalStorageLevelDB(cfg.Browser.Profile), 0); err == nil {
		lsTarball = lt
	} else if !errors.Is(err, chromedirsync.ErrSourceMissing) {
		fmt.Fprintf(os.Stderr, "agentcookie source: localStorage pack failed (%v); continuing without it\n", err)
	}
	// Set AGENTCOOKIE_SYNC_INDEXEDDB=1 to opt in.
	if !cookiesOnly && os.Getenv("AGENTCOOKIE_SYNC_INDEXEDDB") == "1" {
		if it, sk, err := chromedirsync.Pack(sourceBrowser.IndexedDBDir(cfg.Browser.Profile), 5*1024*1024); err == nil {
			idbTarball = it
			idbSkipped = sk
		} else if !errors.Is(err, chromedirsync.ErrSourceMissing) {
			fmt.Fprintf(os.Stderr, "agentcookie source: indexedDB pack failed (%v); continuing without it\n", err)
		}
	}

	targetResults := make(map[string]any, len(targets))
	var targetErrs []error
	if localChromeErr != nil {
		targetErrs = append(targetErrs, localChromeErr)
	}
	succeeded := 0
	for _, target := range targets {
		targetCookies := all
		targetDropped := 0
		if target.Policy != nil {
			matcher := protocol.NewBlocklistMatcher(target.Policy)
			var dropped map[string]int
			targetCookies, dropped = matcher.Filter(all)
			for _, count := range dropped {
				targetDropped += count
			}
		}
		envelope := protocol.SyncEnvelope{
			ProtocolVersion:     protocol.Version,
			SourceHostname:      pairing.LocalHostname(),
			Sequence:            time.Now().UnixNano(),
			Cookies:             targetCookies,
			LocalStorageTarball: lsTarball,
			IndexedDBTarball:    idbTarball,
			IndexedDBSkipped:    idbSkipped,
		}
		if secretsPayload != nil && len(secretsPayload.CLIs) > 0 {
			envelope.Secrets = secretsPayload.CLIs
		}
		payload, err := json.Marshal(envelope)
		if err != nil {
			targetErrs = append(targetErrs, fmt.Errorf("target %s: marshal envelope: %w", target.Name, err))
			continue
		}
		sealed, err := transport.SealWithSecret(payload, target.Secret)
		if err != nil {
			targetErrs = append(targetErrs, fmt.Errorf("target %s: seal payload: %w", target.Name, err))
			continue
		}

		// Preserve upstream v1.1's stale-IP protection independently for every
		// target. Ambiguous peers fail closed; soft failures fall back to DNS.
		targetURL := target.URL
		if resolved, resolveErr := resolveSinkURL(ctx, targetURL); resolveErr != nil {
			if errors.Is(resolveErr, tsclient.ErrAmbiguousPeer) {
				targetErrs = append(targetErrs, fmt.Errorf("target %s: resolve sink URL: %w", target.Name, resolveErr))
				continue
			}
			if verbose {
				fmt.Fprintf(os.Stderr, "agentcookie source: target=%s URL resolution failed (%v); using original %s\n", target.Name, resolveErr, targetURL)
			}
		} else if resolved != targetURL {
			if verbose {
				fmt.Fprintf(os.Stderr, "agentcookie source: target=%s resolved sink URL %s -> %s\n", target.Name, targetURL, resolved)
			}
			targetURL = resolved
		}

		// Each target gets an independent timeout and result. A slow or down
		// sink cannot prevent the remaining configured targets from being tried.
		postCtx, cancel := context.WithTimeout(ctx, httpserver.Defaults(httpserver.SyncClient).ClientTimeout)
		req, err := http.NewRequestWithContext(postCtx, "POST", targetURL, bytes.NewReader(sealed))
		if err != nil {
			cancel()
			targetErrs = append(targetErrs, fmt.Errorf("target %s: new request: %w", target.Name, err))
			continue
		}
		req.Header.Set("Content-Type", "application/octet-stream")
		resp, err := httpserver.Client(httpserver.SyncClient).Do(req)
		if err != nil {
			cancel()
			targetErrs = append(targetErrs, fmt.Errorf("target %s: POST %s: %w", target.Name, targetURL, err))
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		cancel()
		targetResults[target.Name] = map[string]any{
			"url":             targetURL,
			"status":          resp.StatusCode,
			"response":        string(body),
			"cookies":         len(targetCookies),
			"cookies_dropped": targetDropped,
		}
		if resp.StatusCode != http.StatusOK {
			targetErrs = append(targetErrs, fmt.Errorf("target %s returned %d: %s", target.Name, resp.StatusCode, string(body)))
			continue
		}
		succeeded++
		if !common.JSON {
			fmt.Fprintf(os.Stderr, "agentcookie source: target=%s posted %d cookies (target policy dropped %d), sink replied: %s%s\n", target.Name, len(targetCookies), targetDropped, string(body), dbscNote(dbsc))
		}
	}

	result["targets"] = targetResults
	result["targets_succeeded"] = succeeded
	result["posted"] = succeeded == len(targets)
	if common.JSON {
		_ = emit(result, "")
	}
	if len(targetErrs) > 0 {
		return 0, dbsc, errors.Join(targetErrs...)
	}
	return len(all), dbsc, nil
}

// dbscNote returns a concise " (N DBSC-suspect: warned/skipped)" suffix for the
// per-push human summary, or "" when nothing was flagged. Keeps the daemon's
// single summary line informative without the verbose per-cookie block.
func dbscNote(d dbscSummary) string {
	if d.warned == 0 && d.skipped == 0 {
		return ""
	}
	return fmt.Sprintf(" (%d DBSC-suspect: %d warned, %d skipped)", d.warned+d.skipped, d.warned, d.skipped)
}

// dbscSampleReasons returns up to three reason strings (warns first, then
// skips) for surfacing in logs and SourceState without flooding output.
func dbscSampleReasons(res chrome.DBSCResult) []string {
	const max = 3
	out := make([]string, 0, max)
	for _, r := range res.Warned {
		if len(out) == max {
			return out
		}
		out = append(out, r)
	}
	for _, r := range res.Skipped {
		if len(out) == max {
			return out
		}
		out = append(out, r)
	}
	return out
}

// emit writes machine output or human output depending on --json. The human
// string is the fallback text to print when --json is not set.
func emit(machine map[string]any, human string) error {
	if common.JSON {
		return json.NewEncoder(os.Stdout).Encode(machine)
	}
	_, err := fmt.Fprint(os.Stderr, human)
	return err
}
