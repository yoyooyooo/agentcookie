package realchrome

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
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
	if err := setRemoteDebuggingPreference(dir); err != nil {
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
