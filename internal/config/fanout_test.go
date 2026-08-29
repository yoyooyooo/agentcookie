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

func TestLoadSinkLiveCDPOnlyRequiresLiveCDP(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "sink.yaml", `
listen:
  addr: 100.80.229.80:9999
peer:
  hostname: pro
live_cdp_only: true
`)
	_, err := LoadSink(dir)
	if err == nil || !strings.Contains(err.Error(), "requires live_cdp.enabled") {
		t.Fatalf("live_cdp_only error = %v", err)
	}

	writeFile(t, dir, "sink.yaml", `
listen:
  addr: 100.80.229.80:9999
peer:
  hostname: pro
live_cdp_only: true
live_cdp:
  enabled: true
  endpoint: http://127.0.0.1:19222
`)
	cfg, err := LoadSink(dir)
	if err != nil {
		t.Fatalf("LoadSink live CDP only: %v", err)
	}
	if !cfg.LiveCDPOnly || !cfg.LiveCDP.Enabled {
		t.Fatalf("live CDP config = %+v", cfg)
	}
}
