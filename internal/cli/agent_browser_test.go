package cli

import (
	"context"
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

func TestLoopbackCDPWebSocket(t *testing.T) {
	tests := []struct {
		raw     string
		want    string
		wantErr bool
	}{
		{raw: "ws://127.0.0.1:62606/devtools/browser/id", want: "ws://127.0.0.1:62606/devtools/browser/id"},
		{raw: "ws://[::1]:9222/devtools/browser/id", want: "ws://[::1]:9222/devtools/browser/id"},
		{raw: "wss://localhost:9443/devtools/browser/id", want: "wss://localhost:9443/devtools/browser/id"},
		{raw: "ws://100.101.1.2:9222/devtools/browser/id", wantErr: true},
		{raw: "http://127.0.0.1:9222/json/version", wantErr: true},
		{raw: "ws://127.0.0.1/devtools/browser/id", wantErr: true},
		{raw: "ws://127.0.0.1:9222/devtools/page/id", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			got, err := loopbackCDPWebSocket(tt.raw)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %q", got)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("endpoint = %q, %v; want %q", got, err, tt.want)
			}
		})
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
			case reflect.DeepEqual(args, []string{"--session", "task-a", "get", "cdp-url", "--json"}):
				return []byte(`{"success":true,"data":{"cdpUrl":"ws://127.0.0.1:62606/devtools/browser/id"}}`), nil
			default:
				return nil, fmt.Errorf("unexpected args %v", args)
			}
		},
		func(_ context.Context, wsURL string, cookies []chrome.Cookie) (int, error) {
			if wsURL != "ws://127.0.0.1:62606/devtools/browser/id" || len(cookies) != 1 {
				t.Fatalf("inject wsURL=%q cookies=%d", wsURL, len(cookies))
			}
			return 1, nil
		},
	)
	defer restore()

	result, err := injectNamedAgentBrowserSession(context.Background(), agentBrowserInjectOptions{
		Session: "task-a", Binary: "/fake/agent-browser", Start: true,
	}, []chrome.Cookie{{HostKey: ".example.com", Name: "session", Value: "value"}})
	if err != nil {
		t.Fatalf("injectNamedAgentBrowserSession: %v", err)
	}
	if !result.Started || result.Contexts != 1 || result.Session != "task-a" {
		t.Fatalf("result = %+v", result)
	}
	if len(calls) != 3 || calls[1] != "--session task-a open" {
		t.Fatalf("calls = %v", calls)
	}
}

func TestInjectNamedAgentBrowserSessionClosesNewSessionOnFailure(t *testing.T) {
	var calls []string
	restore := stubAgentBrowserRuntime(t,
		func(_ context.Context, _ string, args ...string) ([]byte, error) {
			calls = append(calls, strings.Join(args, " "))
			switch args[len(args)-1] {
			case "--json":
				if len(args) >= 2 && args[len(args)-2] == "info" {
					return []byte(`{"success":true,"data":{"active":false}}`), nil
				}
				return []byte(`{"success":true,"data":{"cdpUrl":"ws://127.0.0.1:62606/devtools/browser/id"}}`), nil
			default:
				return nil, nil
			}
		},
		func(context.Context, string, []chrome.Cookie) (int, error) {
			return 0, errors.New("inject failed")
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
	injector func(context.Context, string, []chrome.Cookie) (int, error),
) func() {
	t.Helper()
	oldRunner := runAgentBrowser
	oldInjector := injectAgentBrowserCDP
	runAgentBrowser = runner
	injectAgentBrowserCDP = injector
	return func() {
		runAgentBrowser = oldRunner
		injectAgentBrowserCDP = oldInjector
	}
}
