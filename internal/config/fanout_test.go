package config

import (
	"strings"
	"testing"
)

func TestLoadSourceFanoutTargets(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "source.yaml", `
targets:
  restricted-sink:
    url: http://restricted-sink:9999/sync
    peer: restricted-sink
    policy: allowlist
    domains:
      - pattern: "example.com"
      - pattern: "%.example.com"
  full-sink:
    url: http://full-sink:9999/sync
    peer: full-sink
  parked-sink:
    url: http://parked-sink:9999/sync
    peer: parked-sink
    disabled: true
browser:
  name: dia
  profile: Default
`)

	cfg, err := LoadSource(dir)
	if err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	targets, err := cfg.SelectSourceTargets(nil)
	if err != nil {
		t.Fatalf("SelectSourceTargets: %v", err)
	}
	if len(targets) != 2 || targets[0].Name != "full-sink" || targets[1].Name != "restricted-sink" {
		t.Fatalf("enabled targets = %+v", targets)
	}

	selected, err := cfg.SelectSourceTargets([]string{"restricted-sink"})
	if err != nil {
		t.Fatalf("SelectSourceTargets restricted-sink: %v", err)
	}
	if len(selected) != 1 || selected[0].Peer != "restricted-sink" || selected[0].Policy == nil || selected[0].Policy.PolicyMode() != CookiePolicyAllowlist {
		t.Fatalf("selected target = %+v", selected)
	}
	if _, err := cfg.SelectSourceTargets([]string{"missing"}); err == nil {
		t.Fatal("unknown target should fail")
	}
}

func TestLoadSourceRejectsMixedLegacyAndFanout(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "source.yaml", `
sink:
  url: http://legacy:9999/sync
peer:
  hostname: legacy
targets:
  fanout-sink:
    url: http://fanout-sink:9999/sync
    peer: fanout-sink
`)
	_, err := LoadSource(dir)
	if err == nil || !strings.Contains(err.Error(), "cannot be configured together") {
		t.Fatalf("mixed config error = %v", err)
	}
}

func TestLoadSourceRejectsInvalidFanoutTarget(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		want string
	}{
		{name: "missing URL", yaml: "targets:\n  target:\n    peer: target\n", want: "targets.target.url is required"},
		{name: "missing peer", yaml: "targets:\n  target:\n    url: http://target:9999/sync\n", want: "targets.target.peer is required"},
		{name: "invalid policy", yaml: "targets:\n  target:\n    url: http://target:9999/sync\n    peer: target\n    policy: unknown\n", want: "targets.target.policy"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, dir, "source.yaml", tt.yaml)
			_, err := LoadSource(dir)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("LoadSource error = %v, want %q", err, tt.want)
			}
		})
	}
}
