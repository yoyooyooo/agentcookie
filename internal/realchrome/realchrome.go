// Package realchrome delivers cookies into the user's ordinary Google Chrome
// profile through either a user-approved live endpoint or an unattended,
// host-bound write while Chrome is stopped.
package realchrome

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/mvanhorn/agentcookie/internal/chrome"
	"github.com/mvanhorn/agentcookie/internal/livecdp"
)

const activePortFile = "DevToolsActivePort"

var (
	ErrRemoteDebuggingDisabled = errors.New("realchrome: Chrome remote debugging is not enabled")
	ErrStaleEndpoint           = errors.New("realchrome: Chrome remote-debugging endpoint is stale")
	ErrApprovalUnavailable     = errors.New("realchrome: Chrome approval dialog could not be automated")

	offlineChromeRunning       = chromeRunning
	offlineStopChrome          = stopChrome
	offlineLaunchChrome        = launchChrome
	offlineWaitChromeRunning   = waitForChromeRunning
	offlineSafeStoragePassword = chrome.SafeStoragePassword
	offlineWriteCookies        = chrome.WriteCookiesForChrome
)

// Endpoint is Chrome's browser-level DevTools websocket endpoint.
type Endpoint struct {
	Port   int
	WSPath string
}

func (e Endpoint) WebSocketURL() string {
	return fmt.Sprintf("ws://127.0.0.1:%d%s", e.Port, e.WSPath)
}

// Options controls one delivery into the ordinary Chrome profile.
type Options struct {
	Mode        string
	UserDataDir string
	Profile     string
	AutoApprove bool
	Timeout     time.Duration
}

// Result is intentionally value-free so it is safe for status and logs.
type Result struct {
	Mode            string `json:"mode"`
	Cookies         int    `json:"cookies"`
	Port            int    `json:"port,omitempty"`
	ApprovalClicked bool   `json:"approval_clicked,omitempty"`
	Restarted       bool   `json:"restarted,omitempty"`
}

// Status reports the ordinary Chrome profile's debugging posture.
type Status struct {
	UserDataDir       string `json:"user_data_dir"`
	ChromeRunning     bool   `json:"chrome_running"`
	PreferenceSet     bool   `json:"preference_set"`
	EndpointActive    bool   `json:"endpoint_active"`
	EndpointReachable bool   `json:"endpoint_reachable"`
	Port              int    `json:"port,omitempty"`
}

// DefaultUserDataDir returns the platform's normal Google Chrome data root.
func DefaultUserDataDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	if runtime.GOOS == "darwin" {
		return filepath.Join(home, "Library", "Application Support", "Google", "Chrome")
	}
	return filepath.Join(home, ".config", "google-chrome")
}

// ResolveUserDataDir expands an empty path to the ordinary Chrome data root.
func ResolveUserDataDir(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return DefaultUserDataDir()
	}
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			if path == "~" {
				return home
			}
			return filepath.Join(home, strings.TrimPrefix(path, "~/"))
		}
	}
	return path
}

// Discover parses Chrome's two-line DevToolsActivePort file.
func Discover(userDataDir string) (Endpoint, error) {
	userDataDir = ResolveUserDataDir(userDataDir)
	data, err := os.ReadFile(filepath.Join(userDataDir, activePortFile))
	if err != nil {
		if os.IsNotExist(err) {
			return Endpoint{}, ErrRemoteDebuggingDisabled
		}
		return Endpoint{}, fmt.Errorf("realchrome: read DevToolsActivePort: %w", err)
	}
	return parseActivePort(data)
}

func parseActivePort(data []byte) (Endpoint, error) {
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) < 2 {
		return Endpoint{}, fmt.Errorf("realchrome: DevToolsActivePort needs two lines")
	}
	port, err := strconv.Atoi(strings.TrimSpace(lines[0]))
	if err != nil || port < 1 || port > 65535 {
		return Endpoint{}, fmt.Errorf("realchrome: invalid DevToolsActivePort port %q", strings.TrimSpace(lines[0]))
	}
	wsPath := strings.TrimSpace(lines[1])
	if !strings.HasPrefix(wsPath, "/") {
		return Endpoint{}, fmt.Errorf("realchrome: invalid DevToolsActivePort websocket path %q", wsPath)
	}
	return Endpoint{Port: port, WSPath: wsPath}, nil
}

// Inspect returns a value-free status snapshot.
func Inspect(ctx context.Context, userDataDir string) (Status, error) {
	userDataDir = ResolveUserDataDir(userDataDir)
	running, err := chromeRunning(ctx)
	if err != nil {
		return Status{}, err
	}
	pref, err := remoteDebuggingPreference(userDataDir)
	if err != nil {
		return Status{}, err
	}
	status := Status{UserDataDir: userDataDir, ChromeRunning: running, PreferenceSet: pref}
	ep, err := Discover(userDataDir)
	if errors.Is(err, ErrRemoteDebuggingDisabled) {
		return status, nil
	}
	if err != nil {
		return Status{}, err
	}
	status.EndpointActive = true
	status.Port = ep.Port
	probeCtx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()
	status.EndpointReachable = probeEndpoint(probeCtx, ep) == nil
	return status, nil
}

// Enable turns on Chrome's persisted remote-debugging preference. Chrome is
// gracefully restarted when necessary so the endpoint becomes active now.
func Enable(ctx context.Context, userDataDir string, restart bool) (Status, error) {
	if runtime.GOOS != "darwin" {
		return Status{}, fmt.Errorf("realchrome: automatic enable is supported on macOS only")
	}
	userDataDir = ResolveUserDataDir(userDataDir)
	if ep, err := Discover(userDataDir); err == nil {
		if probeEndpoint(ctx, ep) == nil {
			return Inspect(ctx, userDataDir)
		}
	}

	running, err := chromeRunning(ctx)
	if err != nil {
		return Status{}, err
	}
	if running && !restart {
		return Status{}, fmt.Errorf("realchrome: Chrome is running; allow restart or quit Chrome before enabling remote debugging")
	}
	if running {
		if err := stopChrome(ctx, 20*time.Second); err != nil {
			return Status{}, err
		}
	}
	if err := setRemoteDebuggingPreference(userDataDir, true); err != nil {
		return Status{}, err
	}
	if !restart {
		return Inspect(ctx, userDataDir)
	}
	if err := launchChrome(ctx); err != nil {
		return Status{}, err
	}
	if _, err := waitForEndpoint(ctx, userDataDir, 30*time.Second); err != nil {
		return Status{}, err
	}
	return Inspect(ctx, userDataDir)
}

// Disable turns off Chrome's persisted remote-debugging preference and
// restores Chrome only when it was running before the change.
func Disable(ctx context.Context, userDataDir string, restart bool) (Status, error) {
	if runtime.GOOS != "darwin" {
		return Status{}, fmt.Errorf("realchrome: automatic disable is supported on macOS only")
	}
	userDataDir = ResolveUserDataDir(userDataDir)
	running, err := chromeRunning(ctx)
	if err != nil {
		return Status{}, err
	}
	if running && !restart {
		return Status{}, fmt.Errorf("realchrome: Chrome is running; allow restart or quit Chrome before disabling remote debugging")
	}
	if running {
		if err := stopChrome(ctx, 20*time.Second); err != nil {
			return Status{}, err
		}
	}
	if err := setRemoteDebuggingPreference(userDataDir, false); err != nil {
		return Status{}, err
	}
	if err := os.Remove(filepath.Join(userDataDir, activePortFile)); err != nil && !os.IsNotExist(err) {
		return Status{}, fmt.Errorf("realchrome: remove stale DevToolsActivePort: %w", err)
	}
	if running {
		if err := launchChrome(ctx); err != nil {
			return Status{}, err
		}
		if err := waitForChromeRunning(ctx, 15*time.Second); err != nil {
			return Status{}, err
		}
	}
	return Inspect(ctx, userDataDir)
}

// Inject writes cookies into the ordinary Chrome profile using the selected
// delivery mode. Live mode uses Chrome's DevTools endpoint. Offline mode
// writes Chrome's host-bound Cookie rows while Chrome is stopped and restores
// the browser's prior running state.
func Inject(parent context.Context, opts Options, cookies []chrome.Cookie) (Result, error) {
	if len(cookies) == 0 {
		return Result{}, nil
	}
	switch opts.Mode {
	case "", "live":
		return injectLive(parent, opts, cookies)
	case "offline":
		return injectOffline(parent, opts, cookies)
	default:
		return Result{}, fmt.Errorf("realchrome: unsupported mode %q", opts.Mode)
	}
}

func injectLive(parent context.Context, opts Options, cookies []chrome.Cookie) (Result, error) {
	userDataDir := ResolveUserDataDir(opts.UserDataDir)
	ep, err := Discover(userDataDir)
	if err != nil {
		return Result{}, fmt.Errorf("%w; run `agentcookie chrome enable`", err)
	}
	probeCtx, cancelProbe := context.WithTimeout(parent, 1500*time.Millisecond)
	err = probeEndpoint(probeCtx, ep)
	cancelProbe()
	if err != nil {
		return Result{}, err
	}

	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 40 * time.Second
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	approval := make(chan approvalResult, 1)
	if opts.AutoApprove && runtime.GOOS == "darwin" {
		go func() { approval <- waitForApproval(ctx, timeout) }()
	}

	wsURL := ep.WebSocketURL()
	if _, err := url.Parse(wsURL); err != nil {
		return Result{}, fmt.Errorf("realchrome: invalid websocket URL: %w", err)
	}
	allocCtx, cancelAlloc := chromedp.NewRemoteAllocator(ctx, wsURL, chromedp.NoModifyURL)
	defer cancelAlloc()
	tabCtx, cancelTab := chromedp.NewContext(allocCtx)
	defer cancelTab()

	if err := livecdp.Inject(tabCtx, cookies); err != nil {
		select {
		case ar := <-approval:
			if ar.err != nil && !errors.Is(ar.err, errApprovalTimeout) {
				return Result{}, fmt.Errorf("realchrome: inject: %w; approval helper: %v", err, ar.err)
			}
		default:
		}
		return Result{}, fmt.Errorf("realchrome: inject into ordinary Chrome: %w", err)
	}

	result := Result{Mode: "live", Cookies: len(cookies), Port: ep.Port}
	select {
	case ar := <-approval:
		result.ApprovalClicked = ar.clicked
	default:
	}
	return result, nil
}

func injectOffline(parent context.Context, opts Options, cookies []chrome.Cookie) (result Result, retErr error) {
	if runtime.GOOS != "darwin" {
		return Result{}, fmt.Errorf("realchrome: offline mode is supported on macOS only")
	}
	userDataDir := ResolveUserDataDir(opts.UserDataDir)
	profile := strings.TrimSpace(opts.Profile)
	if profile == "" {
		profile = "Default"
	}
	if profile == "." || profile == ".." || strings.ContainsAny(profile, `/\\`) {
		return Result{}, fmt.Errorf("realchrome: invalid profile directory %q", profile)
	}
	cookiesPath := filepath.Join(userDataDir, profile, "Cookies")
	if _, err := os.Stat(cookiesPath); err != nil {
		return Result{}, fmt.Errorf("realchrome: ordinary Chrome Cookies database: %w", err)
	}

	wasRunning, err := offlineChromeRunning(parent)
	if err != nil {
		return Result{}, err
	}
	stopped := false
	relaunched := false
	if wasRunning {
		if err := offlineStopChrome(parent, 20*time.Second); err != nil {
			return Result{}, err
		}
		stopped = true
	}
	defer func() {
		if !stopped || relaunched {
			return
		}
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(parent), 15*time.Second)
		defer cancel()
		if err := offlineLaunchChrome(cleanupCtx); err != nil && retErr == nil {
			retErr = err
		}
	}()

	password, err := offlineSafeStoragePassword()
	if err != nil {
		return Result{}, fmt.Errorf("realchrome: read Chrome Safe Storage: %w", err)
	}
	key, err := chrome.DeriveAESKey(password)
	if err != nil {
		return Result{}, err
	}
	written, err := offlineWriteCookies(cookiesPath, cookies, key)
	if err != nil {
		return Result{}, fmt.Errorf("realchrome: write ordinary Chrome Cookie store: %w", err)
	}
	if wasRunning {
		if err := offlineLaunchChrome(parent); err != nil {
			return Result{}, err
		}
		relaunched = true
		if err := offlineWaitChromeRunning(parent, 15*time.Second); err != nil {
			return Result{}, err
		}
	}
	return Result{Mode: "offline", Cookies: written, Restarted: wasRunning}, nil
}

func probeEndpoint(ctx context.Context, ep Endpoint) error {
	d := net.Dialer{}
	conn, err := d.DialContext(ctx, "tcp", fmt.Sprintf("127.0.0.1:%d", ep.Port))
	if err != nil {
		return fmt.Errorf("%w: %v", ErrStaleEndpoint, err)
	}
	_ = conn.Close()
	return nil
}

func waitForEndpoint(ctx context.Context, userDataDir string, timeout time.Duration) (Endpoint, error) {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		ep, err := Discover(userDataDir)
		if err == nil {
			if err = probeEndpoint(ctx, ep); err == nil {
				return ep, nil
			}
		}
		lastErr = err
		select {
		case <-ctx.Done():
			return Endpoint{}, ctx.Err()
		case <-time.After(400 * time.Millisecond):
		}
	}
	return Endpoint{}, fmt.Errorf("realchrome: endpoint did not become active within %s: %v", timeout, lastErr)
}

func chromeRunning(ctx context.Context) (bool, error) {
	cmd := exec.CommandContext(ctx, "/usr/bin/pgrep", "-x", "Google Chrome")
	err := cmd.Run()
	if err == nil {
		return true, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return false, nil
	}
	return false, fmt.Errorf("realchrome: probe Google Chrome process: %w", err)
}

func stopChrome(ctx context.Context, timeout time.Duration) error {
	attemptTimeout := 5 * time.Second
	if timeout > 0 && timeout < attemptTimeout {
		attemptTimeout = timeout
	}
	quitCtx, cancelQuit := context.WithTimeout(ctx, attemptTimeout)
	quitErr := quitChrome(quitCtx, attemptTimeout)
	cancelQuit()
	if quitErr == nil {
		return nil
	}
	out, err := exec.CommandContext(ctx, "/usr/bin/pgrep", "-x", "Google Chrome").Output()
	if err != nil {
		return fmt.Errorf("realchrome: locate Chrome for graceful SIGTERM fallback: %w", err)
	}
	for _, line := range strings.Fields(string(out)) {
		pid, err := strconv.Atoi(line)
		if err != nil {
			continue
		}
		if err := syscall.Kill(pid, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
			return fmt.Errorf("realchrome: terminate Chrome pid %d: %w", pid, err)
		}
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		running, err := chromeRunning(ctx)
		if err != nil {
			return err
		}
		if !running {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
	return fmt.Errorf("realchrome: Chrome did not exit after SIGTERM within %s", timeout)
}

func quitChrome(ctx context.Context, timeout time.Duration) error {
	cmd := exec.CommandContext(ctx, "/usr/bin/osascript", "-e", `tell application "Google Chrome" to quit`)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("realchrome: quit Chrome: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		running, err := chromeRunning(ctx)
		if err != nil {
			return err
		}
		if !running {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
	return fmt.Errorf("realchrome: Chrome did not quit within %s", timeout)
}

func waitForChromeRunning(ctx context.Context, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		running, err := chromeRunning(ctx)
		if err != nil {
			return err
		}
		if running {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
	return fmt.Errorf("realchrome: Chrome did not relaunch within %s", timeout)
}

func launchChrome(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "/usr/bin/open", "-a", "Google Chrome")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("realchrome: relaunch Chrome: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func remoteDebuggingPreference(userDataDir string) (bool, error) {
	state, _, _, err := readPreferenceMaps(userDataDir)
	if err != nil {
		return false, err
	}
	devRaw, ok := state["devtools"]
	if !ok {
		return false, nil
	}
	var dev map[string]json.RawMessage
	if err := json.Unmarshal(devRaw, &dev); err != nil {
		return false, fmt.Errorf("realchrome: parse Local State devtools: %w", err)
	}
	rdRaw, ok := dev["remote_debugging"]
	if !ok {
		return false, nil
	}
	var rd map[string]json.RawMessage
	if err := json.Unmarshal(rdRaw, &rd); err != nil {
		return false, fmt.Errorf("realchrome: parse Local State remote_debugging: %w", err)
	}
	var enabled bool
	if raw, ok := rd["user-enabled"]; ok {
		_ = json.Unmarshal(raw, &enabled)
	}
	return enabled, nil
}

func setRemoteDebuggingPreference(userDataDir string, enabled bool) error {
	state, path, mode, err := readPreferenceMaps(userDataDir)
	if err != nil {
		return err
	}
	dev := map[string]json.RawMessage{}
	if raw, ok := state["devtools"]; ok {
		if err := json.Unmarshal(raw, &dev); err != nil {
			return fmt.Errorf("realchrome: parse Local State devtools: %w", err)
		}
	}
	rd := map[string]json.RawMessage{}
	if raw, ok := dev["remote_debugging"]; ok {
		if err := json.Unmarshal(raw, &rd); err != nil {
			return fmt.Errorf("realchrome: parse Local State remote_debugging: %w", err)
		}
	}
	rd["user-enabled"] = json.RawMessage(strconv.FormatBool(enabled))
	rdJSON, err := json.Marshal(rd)
	if err != nil {
		return err
	}
	dev["remote_debugging"] = rdJSON
	devJSON, err := json.Marshal(dev)
	if err != nil {
		return err
	}
	state["devtools"] = devJSON
	out, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("realchrome: marshal Local State: %w", err)
	}
	return atomicWrite(path, out, mode)
}

func readPreferenceMaps(userDataDir string) (map[string]json.RawMessage, string, os.FileMode, error) {
	path := filepath.Join(ResolveUserDataDir(userDataDir), "Local State")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, path, 0, fmt.Errorf("realchrome: read Local State: %w", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, path, 0, err
	}
	var state map[string]json.RawMessage
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, path, 0, fmt.Errorf("realchrome: parse Local State: %w", err)
	}
	return state, path, info.Mode().Perm(), nil
}

func atomicWrite(path string, data []byte, mode os.FileMode) (retErr error) {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".agentcookie-local-state-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		_ = tmp.Close()
		if retErr != nil {
			_ = os.Remove(tmpName)
		}
	}()
	if err := tmp.Chmod(mode); err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	return nil
}
