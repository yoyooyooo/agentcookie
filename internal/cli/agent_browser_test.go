package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mvanhorn/agentcookie/internal/chrome"
)

func TestExpandAgentBrowserDomainPatterns(t *testing.T) {
	got := expandAgentBrowserDomainPatterns([]string{"example.com", ".example.com", "%github.com", ""})
	want := []string{"example.com", "%.example.com", "%github.com"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("patterns = %v, want %v", got, want)
	}
}

func TestResolveAgentBrowserCookieSource(t *testing.T) {
	dir := t.TempDir()
	withConfigDir(t, dir)

	if _, err := resolveAgentBrowserCookieSource("auto"); err == nil {
		t.Fatal("auto without role config should fail")
	}
	writeCLIFile(t, filepath.Join(dir, "source.yaml"), "browser:\n  name: dia\n")
	if got, err := resolveAgentBrowserCookieSource("auto"); err != nil || got != agentBrowserCookieSourceSource {
		t.Fatalf("source auto = %q, %v", got, err)
	}
	writeCLIFile(t, filepath.Join(dir, "sink.yaml"), "listen:\n  addr: 127.0.0.1:9999\n")
	if _, err := resolveAgentBrowserCookieSource("auto"); err == nil || !strings.Contains(err.Error(), "both source.yaml and sink.yaml") {
		t.Fatalf("ambiguous auto error = %v", err)
	}
	if got, err := resolveAgentBrowserCookieSource("sink"); err != nil || got != agentBrowserCookieSourceSink {
		t.Fatalf("explicit sink = %q, %v", got, err)
	}
}

func TestLoadAgentBrowserCookiesFromSinkPreservesMetadata(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	configDir := t.TempDir()
	withConfigDir(t, configDir)
	writeCLIFile(t, filepath.Join(configDir, "blocklist.yaml"), `
version: 1
policy: blocklist
domains: []
`)

	path := filepath.Join(home, ".agentcookie", "cookies-plain.db")
	cookies := []chrome.Cookie{
		{
			HostKey: ".example.com", Name: "session", Value: "value", Path: "/app",
			ExpiresUTC: 13380163200000000, IsSecure: 1, IsHTTPOnly: 1,
			LastAccessUTC: 13370000000000000, HasExpires: 1, IsPersistent: 1,
			Priority: 2, SameSite: 2, SourceScheme: 2, SourcePort: 443,
		},
		{HostKey: ".other.com", Name: "other", Value: "value", Path: "/"},
	}
	if _, err := chrome.WriteCookiesSidecar(path, cookies, nil); err != nil {
		t.Fatalf("WriteCookiesSidecar: %v", err)
	}

	got, source, err := loadAgentBrowserCookies(agentBrowserInjectOptions{
		From:    agentBrowserCookieSourceSink,
		Domains: []string{"example.com"},
	})
	if err != nil {
		t.Fatalf("loadAgentBrowserCookies: %v", err)
	}
	if source != "sidecar" || len(got) != 1 {
		t.Fatalf("source=%q cookies=%d", source, len(got))
	}
	cookie := got[0]
	if cookie.HostKey != ".example.com" || cookie.Path != "/app" || cookie.IsSecure != 1 ||
		cookie.IsHTTPOnly != 1 || cookie.HasExpires != 1 || cookie.IsPersistent != 1 ||
		cookie.SameSite != 2 || cookie.SourcePort != 443 {
		t.Fatalf("metadata was not preserved: %+v", cookie)
	}
}

func TestAgentBrowserCookieCommandsPreserveMetadata(t *testing.T) {
	expires := int64(chromeToUnixEpochSeconds+1_700_000_000) * 1_000_000
	got := agentBrowserCookieCommands([]chrome.Cookie{
		{HostKey: "app.example.com", Name: "__Host-session", Value: "host-value", Path: "/wrong", ExpiresUTC: expires, IsHTTPOnly: 1, SameSite: 0},
		{HostKey: ".example.com", Name: "domain", Value: "domain-value", Path: "/app", SameSite: 0},
		{HostKey: "api.example.net", Name: "host", Value: "host-only", SameSite: -1},
	})
	want := [][]string{
		{"cookies", "set", "__Host-session", "host-value", "--url", "https://app.example.com", "--path", "/", "--secure", "--httpOnly", "--sameSite", "None", "--expires", "1700000000"},
		{"cookies", "set", "domain", "domain-value", "--domain", ".example.com", "--path", "/app", "--sameSite", "Lax"},
		{"cookies", "set", "host", "host-only", "--url", "http://api.example.net", "--path", "/"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("commands = %#v, want %#v", got, want)
	}
}

func TestInjectNamedAgentBrowserSessionStartsAndTargetsExactSession(t *testing.T) {
	var calls []string
	restore := stubAgentBrowserRuntime(t,
		func(_ context.Context, _ string, args ...string) ([]byte, error) {
			calls = append(calls, strings.Join(args, " "))
			switch {
			case reflect.DeepEqual(args, []string{"--session", "task-a", "session", "info", "--json"}):
				return []byte(`{"success":true,"data":{"active":false}}`), nil
			case reflect.DeepEqual(args, []string{"--session", "task-a", "open"}):
				return nil, nil
			default:
				return nil, fmt.Errorf("unexpected args %v", args)
			}
		},
		func(_ context.Context, _ string, stdin []byte, args ...string) error {
			if !reflect.DeepEqual(args, []string{"--session", "task-a", "batch", "--bail"}) {
				t.Fatalf("batch args = %v", args)
			}
			var commands [][]string
			if err := json.Unmarshal(stdin, &commands); err != nil {
				t.Fatalf("decode batch: %v", err)
			}
			if len(commands) != 1 || commands[0][2] != "session" || commands[0][3] != "value" {
				t.Fatalf("batch commands = %v", commands)
			}
			return nil
		},
	)
	defer restore()

	result, err := injectNamedAgentBrowserSession(context.Background(), agentBrowserInjectOptions{
		Session: "task-a", Binary: "/fake/agent-browser", Start: true,
	}, []chrome.Cookie{{HostKey: ".example.com", Name: "session", Value: "value"}})
	if err != nil {
		t.Fatalf("injectNamedAgentBrowserSession: %v", err)
	}
	if !result.Started || result.Cookies != 1 || result.Session != "task-a" {
		t.Fatalf("result = %+v", result)
	}
	if len(calls) != 2 || calls[1] != "--session task-a open" {
		t.Fatalf("calls = %v", calls)
	}
}

func TestInjectNamedAgentBrowserSessionClosesNewSessionOnFailure(t *testing.T) {
	var calls []string
	restore := stubAgentBrowserRuntime(t,
		func(_ context.Context, _ string, args ...string) ([]byte, error) {
			calls = append(calls, strings.Join(args, " "))
			if args[len(args)-1] == "--json" {
				return []byte(`{"success":true,"data":{"active":false}}`), nil
			}
			return nil, nil
		},
		func(context.Context, string, []byte, ...string) error {
			return errors.New("inject failed")
		},
	)
	defer restore()

	_, err := injectNamedAgentBrowserSession(context.Background(), agentBrowserInjectOptions{
		Session: "task-fail", Binary: "/fake/agent-browser", Start: true,
	}, []chrome.Cookie{{HostKey: ".example.com", Name: "session", Value: "value"}})
	if err == nil || !strings.Contains(err.Error(), "inject failed") {
		t.Fatalf("error = %v", err)
	}
	if got := calls[len(calls)-1]; got != "--session task-fail close" {
		t.Fatalf("last call = %q, all calls=%v", got, calls)
	}
}

func stubAgentBrowserRuntime(
	t *testing.T,
	runner agentBrowserRunner,
	batchRunner agentBrowserBatchRunner,
) func() {
	t.Helper()
	oldRunner := runAgentBrowser
	oldBatchRunner := runAgentBrowserBatch
	runAgentBrowser = runner
	runAgentBrowserBatch = batchRunner
	return func() {
		runAgentBrowser = oldRunner
		runAgentBrowserBatch = oldBatchRunner
	}
}
