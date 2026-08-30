package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/mvanhorn/agentcookie/internal/chrome"
	"github.com/mvanhorn/agentcookie/internal/config"
	"github.com/mvanhorn/agentcookie/internal/livecdp"
	"github.com/mvanhorn/agentcookie/internal/protocol"
	"github.com/mvanhorn/agentcookie/internal/sinkpush"
	"github.com/mvanhorn/agentcookie/pkg/sidecar"
)

const (
	agentBrowserCookieSourceAuto   = "auto"
	agentBrowserCookieSourceSource = "source"
	agentBrowserCookieSourceSink   = "sink"
)

var (
	agentBrowserInjectSession  string
	agentBrowserInjectDomains  []string
	agentBrowserInjectFrom     string
	agentBrowserInjectBinary   string
	agentBrowserInjectStart    bool
	agentBrowserInjectSkipDBSC bool
)

var agentBrowserCmd = &cobra.Command{
	Use:   "agent-browser",
	Short: "Inject synced browser identity into isolated agent-browser sessions",
}

var agentBrowserInjectCmd = &cobra.Command{
	Use:   "inject",
	Short: "Inject cookies into one named agent-browser session",
	Long: `Inject cookies into exactly one agent-browser --session instance.

The command reads the live source browser when source.yaml is present, or the
latest official sink sidecar on a sink machine. If the named session is not
running, it starts the browser on about:blank, discovers that session's dynamic
loopback CDP endpoint, and injects before any application navigation.

  agentcookie agent-browser inject --session task --domain github.com
  agent-browser --session task open https://github.com
  agent-browser --session task close`,
	RunE: runAgentBrowserInject,
}

func init() {
	agentBrowserInjectCmd.Flags().StringVar(&agentBrowserInjectSession, "session", "", "agent-browser session name (required)")
	agentBrowserInjectCmd.Flags().StringSliceVar(&agentBrowserInjectDomains, "domain", nil, "limit to a cookie host suffix or SQLite-LIKE pattern (repeatable)")
	agentBrowserInjectCmd.Flags().StringVar(&agentBrowserInjectFrom, "from", agentBrowserCookieSourceAuto, "cookie source: auto, source, or sink")
	agentBrowserInjectCmd.Flags().StringVar(&agentBrowserInjectBinary, "agent-browser-path", "", "agent-browser executable path (default: PATH lookup)")
	agentBrowserInjectCmd.Flags().BoolVar(&agentBrowserInjectStart, "start", true, "start an inactive session on about:blank before injection")
	agentBrowserInjectCmd.Flags().BoolVar(&agentBrowserInjectSkipDBSC, "skip-dbsc-suspect", false, "drop cookies that look device-bound before injection")
	agentBrowserCmd.AddCommand(agentBrowserInjectCmd)
}

type agentBrowserInjectOptions struct {
	Session  string
	Domains  []string
	From     string
	Binary   string
	Start    bool
	SkipDBSC bool
}

type agentBrowserInjectResult struct {
	Session  string `json:"session"`
	Source   string `json:"source"`
	Cookies  int    `json:"cookies"`
	Contexts int    `json:"contexts"`
	Started  bool   `json:"started"`
}

func runAgentBrowserInject(cmd *cobra.Command, _ []string) error {
	opts := agentBrowserInjectOptions{
		Session:  strings.TrimSpace(agentBrowserInjectSession),
		Domains:  append([]string(nil), agentBrowserInjectDomains...),
		From:     strings.ToLower(strings.TrimSpace(agentBrowserInjectFrom)),
		Binary:   agentBrowserInjectBinary,
		Start:    agentBrowserInjectStart,
		SkipDBSC: agentBrowserInjectSkipDBSC,
	}
	if opts.Session == "" {
		return fmt.Errorf("agent-browser inject: --session is required")
	}

	cookies, source, err := loadAgentBrowserCookies(opts)
	if err != nil {
		return err
	}
	if len(cookies) == 0 {
		return fmt.Errorf("agent-browser inject: no cookies matched source %s and domains %v", source, opts.Domains)
	}

	result, err := injectNamedAgentBrowserSession(cmd.Context(), opts, cookies)
	if err != nil {
		return err
	}
	result.Source = source
	result.Cookies = len(cookies)
	return emit(map[string]any{
		"session":  result.Session,
		"source":   result.Source,
		"cookies":  result.Cookies,
		"contexts": result.Contexts,
		"started":  result.Started,
	}, fmt.Sprintf("agentcookie agent-browser: injected %d cookies into session %q (%d context(s), source=%s)\n", result.Cookies, result.Session, result.Contexts, result.Source))
}

func loadAgentBrowserCookies(opts agentBrowserInjectOptions) ([]chrome.Cookie, string, error) {
	from, err := resolveAgentBrowserCookieSource(opts.From)
	if err != nil {
		return nil, "", err
	}
	patterns := expandAgentBrowserDomainPatterns(opts.Domains)

	switch from {
	case agentBrowserCookieSourceSource:
		cfg, err := config.LoadSourceLocal(common.ConfigDir)
		if err != nil {
			return nil, "", fmt.Errorf("agent-browser inject: load source config: %w", err)
		}
		blocklist, err := loadFreshBlocklist()
		if err != nil {
			return nil, "", err
		}
		browser, err := chrome.LookupBrowser(cfg.Browser.Name)
		if err != nil {
			return nil, "", err
		}
		password, err := chrome.SafeStoragePasswordFor(browser)
		if err != nil {
			return nil, "", err
		}
		key, err := chrome.DeriveAESKey(password)
		if err != nil {
			return nil, "", err
		}
		cookies, _, err := readFilteredCookies(cfg.Chrome.DBPath, blocklist, key, opts.SkipDBSC, time.Now().UTC())
		if err != nil {
			return nil, "", err
		}
		return sinkpush.FilterByHostPatterns(cookies, patterns), "browser:" + browser.Name, nil

	case agentBrowserCookieSourceSink:
		path, err := sidecar.DefaultPath()
		if err != nil {
			return nil, "", fmt.Errorf("agent-browser inject: resolve sidecar: %w", err)
		}
		stored, err := sidecar.ReadSidecar(path)
		if err != nil {
			return nil, "", fmt.Errorf("agent-browser inject: read sidecar: %w", err)
		}
		cookies := make([]chrome.Cookie, 0, len(stored))
		for _, c := range stored {
			cookies = append(cookies, chrome.Cookie{
				HostKey:       c.HostKey,
				Name:          c.Name,
				Value:         c.Value,
				Path:          c.Path,
				ExpiresUTC:    c.ExpiresUTC,
				IsSecure:      boolInt(c.IsSecure),
				IsHTTPOnly:    boolInt(c.IsHTTPOnly),
				LastAccessUTC: c.LastAccessUTC,
				HasExpires:    boolInt(c.HasExpires),
				IsPersistent:  boolInt(c.IsPersistent),
				Priority:      c.Priority,
				SameSite:      c.SameSite,
				SourceScheme:  c.SourceScheme,
				SourcePort:    c.SourcePort,
			})
		}
		blocklist, err := loadFreshBlocklist()
		if err != nil {
			return nil, "", err
		}
		cookies, _ = protocol.NewBlocklistMatcherForSink(blocklist).Filter(cookies)
		cookies = chrome.ClassifyCookies(cookies, time.Now().UTC(), opts.SkipDBSC).Shipped
		return sinkpush.FilterByHostPatterns(cookies, patterns), "sidecar", nil
	default:
		return nil, "", fmt.Errorf("agent-browser inject: unsupported cookie source %q", from)
	}
}

func resolveAgentBrowserCookieSource(from string) (string, error) {
	switch from {
	case agentBrowserCookieSourceSource, agentBrowserCookieSourceSink:
		return from, nil
	case "", agentBrowserCookieSourceAuto:
		sourceExists := fileExists(filepath.Join(common.ConfigDir, "source.yaml"))
		sinkExists := fileExists(filepath.Join(common.ConfigDir, "sink.yaml"))
		if sourceExists && sinkExists {
			return "", fmt.Errorf("agent-browser inject: both source.yaml and sink.yaml exist; pass --from source or --from sink")
		}
		if sourceExists {
			return agentBrowserCookieSourceSource, nil
		}
		if sinkExists {
			return agentBrowserCookieSourceSink, nil
		}
		return "", fmt.Errorf("agent-browser inject: no source.yaml or sink.yaml in %s", common.ConfigDir)
	default:
		return "", fmt.Errorf("agent-browser inject: --from must be auto, source, or sink (got %q)", from)
	}
}

func expandAgentBrowserDomainPatterns(domains []string) []string {
	patterns := make([]string, 0, len(domains)*2)
	seen := make(map[string]bool)
	for _, domain := range domains {
		domain = strings.ToLower(strings.TrimSpace(domain))
		if domain == "" {
			continue
		}
		if strings.Contains(domain, "%") {
			if !seen[domain] {
				patterns = append(patterns, domain)
				seen[domain] = true
			}
			continue
		}
		bare := strings.TrimPrefix(domain, ".")
		for _, pattern := range []string{bare, "%." + bare} {
			if !seen[pattern] {
				patterns = append(patterns, pattern)
				seen[pattern] = true
			}
		}
	}
	return patterns
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

type agentBrowserRunner func(ctx context.Context, binary string, args ...string) ([]byte, error)

var runAgentBrowser = execAgentBrowser
var injectAgentBrowserCDP = livecdp.AttachAndInject

func execAgentBrowser(ctx context.Context, binary string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, binary, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err == nil {
		return out, nil
	}
	detail := strings.TrimSpace(stderr.String())
	if detail != "" {
		return nil, fmt.Errorf("agent-browser %s: %s: %w", strings.Join(args, " "), detail, err)
	}
	return nil, fmt.Errorf("agent-browser %s: %w", strings.Join(args, " "), err)
}

func injectNamedAgentBrowserSession(ctx context.Context, opts agentBrowserInjectOptions, cookies []chrome.Cookie) (result agentBrowserInjectResult, err error) {
	result.Session = opts.Session
	binary := opts.Binary
	if binary == "" {
		binary, err = exec.LookPath("agent-browser")
		if err != nil {
			return result, fmt.Errorf("agent-browser inject: find agent-browser: %w", err)
		}
	}

	active, err := agentBrowserSessionActive(ctx, binary, opts.Session)
	if err != nil {
		return result, err
	}
	if !active {
		if !opts.Start {
			return result, fmt.Errorf("agent-browser inject: session %q is not active; omit --start=false to launch it on about:blank", opts.Session)
		}
		if _, err := runAgentBrowser(ctx, binary, "--session", opts.Session, "open"); err != nil {
			return result, fmt.Errorf("agent-browser inject: start session %q: %w", opts.Session, err)
		}
		result.Started = true
	}

	keepSession := false
	defer func() {
		if result.Started && !keepSession {
			_, _ = runAgentBrowser(context.Background(), binary, "--session", opts.Session, "close")
		}
	}()

	wsURL, err := agentBrowserSessionCDPURL(ctx, binary, opts.Session)
	if err != nil {
		return result, err
	}
	endpoint, err := loopbackCDPEndpoint(wsURL)
	if err != nil {
		return result, err
	}
	result.Contexts, err = injectAgentBrowserCDP(ctx, endpoint, cookies)
	if err != nil {
		return result, fmt.Errorf("agent-browser inject: inject session %q: %w", opts.Session, err)
	}
	if result.Contexts == 0 {
		return result, fmt.Errorf("agent-browser inject: session %q has no injectable browser context", opts.Session)
	}
	keepSession = true
	return result, nil
}

func agentBrowserSessionActive(ctx context.Context, binary, session string) (bool, error) {
	out, err := runAgentBrowser(ctx, binary, "--session", session, "session", "info", "--json")
	if err != nil {
		return false, fmt.Errorf("agent-browser inject: inspect session %q: %w", session, err)
	}
	var response struct {
		Success bool `json:"success"`
		Data    struct {
			Active bool `json:"active"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out, &response); err != nil {
		return false, fmt.Errorf("agent-browser inject: decode session info: %w", err)
	}
	if !response.Success {
		return false, fmt.Errorf("agent-browser inject: session info reported failure")
	}
	return response.Data.Active, nil
}

func agentBrowserSessionCDPURL(ctx context.Context, binary, session string) (string, error) {
	out, err := runAgentBrowser(ctx, binary, "--session", session, "get", "cdp-url", "--json")
	if err != nil {
		return "", fmt.Errorf("agent-browser inject: discover CDP for session %q: %w", session, err)
	}
	var response struct {
		Success bool `json:"success"`
		Data    struct {
			CDPURL string `json:"cdpUrl"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out, &response); err != nil {
		return "", fmt.Errorf("agent-browser inject: decode CDP URL: %w", err)
	}
	if !response.Success || response.Data.CDPURL == "" {
		return "", fmt.Errorf("agent-browser inject: session %q returned no CDP URL", session)
	}
	return response.Data.CDPURL, nil
}

func loopbackCDPEndpoint(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("agent-browser inject: parse CDP URL: %w", err)
	}
	if u.Scheme != "ws" && u.Scheme != "wss" {
		return "", fmt.Errorf("agent-browser inject: CDP URL must use ws or wss (got %q)", u.Scheme)
	}
	hostname := u.Hostname()
	ip := net.ParseIP(hostname)
	if hostname != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return "", fmt.Errorf("agent-browser inject: refusing non-loopback CDP host %q", hostname)
	}
	if u.Port() == "" {
		return "", fmt.Errorf("agent-browser inject: CDP URL has no port")
	}
	scheme := "http"
	if u.Scheme == "wss" {
		scheme = "https"
	}
	return scheme + "://" + u.Host, nil
}
