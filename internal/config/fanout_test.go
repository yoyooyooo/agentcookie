package config

import (
	"strings"
	"testing"
)

func TestLoadSourceFanoutTargets(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "source.yaml", `
targets:
  mini:
    url: http://mini:9999/sync
    peer: mini
    policy: allowlist
    domains:
      - pattern: "tailscale.com"
      - pattern: "%.tailscale.com"
  grok-bot:
    url: http://grok-bot:9999/sync
    peer: grok-bot
  parked:
    url: http://parked:9999/sync
    peer: parked
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
	if len(targets) != 2 || targets[0].Name != "grok-bot" || targets[1].Name != "mini" {
		t.Fatalf("enabled targets = %+v", targets)
	}

	selected, err := cfg.SelectSourceTargets([]string{"mini"})
	if err != nil {
		t.Fatalf("SelectSourceTargets mini: %v", err)
	}
	if len(selected) != 1 || selected[0].Peer != "mini" || selected[0].Policy == nil || selected[0].Policy.PolicyMode() != CookiePolicyAllowlist {
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
  mini:
    url: http://mini:9999/sync
    peer: mini
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
		{name: "missing URL", yaml: "targets:\n  mini:\n    peer: mini\n", want: "targets.mini.url is required"},
		{name: "missing peer", yaml: "targets:\n  mini:\n    url: http://mini:9999/sync\n", want: "targets.mini.peer is required"},
		{name: "invalid policy", yaml: "targets:\n  mini:\n    url: http://mini:9999/sync\n    peer: mini\n    policy: unknown\n", want: "targets.mini.policy"},
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
