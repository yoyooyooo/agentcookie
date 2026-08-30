package realchrome

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/mvanhorn/agentcookie/internal/chrome"
)

func TestParseActivePort(t *testing.T) {
	ep, err := parseActivePort([]byte("53747\n/devtools/browser/abc\n"))
	if err != nil {
		t.Fatalf("parseActivePort: %v", err)
	}
	if ep.Port != 53747 || ep.WebSocketURL() != "ws://127.0.0.1:53747/devtools/browser/abc" {
		t.Fatalf("unexpected endpoint: %+v", ep)
	}
}

func TestDiscoverMissing(t *testing.T) {
	_, err := Discover(t.TempDir())
	if !errors.Is(err, ErrRemoteDebuggingDisabled) {
		t.Fatalf("Discover error = %v", err)
	}
}

func TestSetRemoteDebuggingPreferencePreservesRawNumbers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Local State")
	original := `{"large":18446744073709551615,"devtools":{"other":7},"untouched":{"x":"y"}}`
	if err := os.WriteFile(path, []byte(original), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := setRemoteDebuggingPreference(dir, true); err != nil {
		t.Fatalf("set preference: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if string(got["large"]) != "18446744073709551615" {
		t.Fatalf("large number changed: %s", got["large"])
	}
	enabled, err := remoteDebuggingPreference(dir)
	if err != nil || !enabled {
		t.Fatalf("preference enabled=%v err=%v", enabled, err)
	}
	if err := setRemoteDebuggingPreference(dir, false); err != nil {
		t.Fatalf("clear preference: %v", err)
	}
	enabled, err = remoteDebuggingPreference(dir)
	if err != nil || enabled {
		t.Fatalf("preference enabled after clear=%v err=%v", enabled, err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("mode = %o", info.Mode().Perm())
	}
}

func TestInspectDisabledEndpoint(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Local State"), []byte(`{"devtools":{"remote_debugging":{"user-enabled":true}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	status, err := Inspect(context.Background(), dir)
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if !status.PreferenceSet || status.EndpointActive {
		t.Fatalf("status = %+v", status)
	}
}

func TestOfflineInjectRestoresRunningChrome(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("offline ordinary Chrome delivery is macOS-only")
	}
	dir := t.TempDir()
	cookiesPath := filepath.Join(dir, "Default", "Cookies")
	if err := os.MkdirAll(filepath.Dir(cookiesPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cookiesPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	oldRunning := offlineChromeRunning
	oldStop := offlineStopChrome
	oldLaunch := offlineLaunchChrome
	oldWait := offlineWaitChromeRunning
	oldPassword := offlineSafeStoragePassword
	oldWrite := offlineWriteCookies
	t.Cleanup(func() {
		offlineChromeRunning = oldRunning
		offlineStopChrome = oldStop
		offlineLaunchChrome = oldLaunch
		offlineWaitChromeRunning = oldWait
		offlineSafeStoragePassword = oldPassword
		offlineWriteCookies = oldWrite
	})

	stopped, launched, waited := false, false, false
	offlineChromeRunning = func(context.Context) (bool, error) { return true, nil }
	offlineStopChrome = func(context.Context, time.Duration) error { stopped = true; return nil }
	offlineLaunchChrome = func(context.Context) error { launched = true; return nil }
	offlineWaitChromeRunning = func(context.Context, time.Duration) error { waited = true; return nil }
	offlineSafeStoragePassword = func() (string, error) { return "password", nil }
	offlineWriteCookies = func(path string, cookies []chrome.Cookie, key []byte) (int, error) {
		if path != cookiesPath || len(cookies) != 1 || len(key) != 16 {
			t.Fatalf("write path=%q cookies=%d key=%d", path, len(cookies), len(key))
		}
		return len(cookies), nil
	}

	result, err := Inject(context.Background(), Options{Mode: "offline", UserDataDir: dir, Profile: "Default"}, []chrome.Cookie{{HostKey: ".example.com", Name: "session"}})
	if err != nil {
		t.Fatalf("Inject: %v", err)
	}
	if !stopped || !launched || !waited || !result.Restarted || result.Mode != "offline" || result.Cookies != 1 {
		t.Fatalf("result=%+v stopped=%v launched=%v waited=%v", result, stopped, launched, waited)
	}
}

func TestInjectRejectsUnknownMode(t *testing.T) {
	_, err := Inject(context.Background(), Options{Mode: "magic"}, []chrome.Cookie{{HostKey: ".example.com", Name: "session"}})
	if err == nil {
		t.Fatal("expected error")
	}
}
