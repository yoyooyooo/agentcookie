package secretsbus

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

// TestLoadPayloadWithDiscovery_CarriesFilesOnlyManifest covers the fix for a
// [[files]]-only manifest (no [secrets.file], so ReadInPlacePath==""): its
// carried files must still ship, where previously the empty read-in-place path
// short-circuited the loop before reaching the carry.
func TestLoadPayloadWithDiscovery_CarriesFilesOnlyManifest(t *testing.T) {
	home := t.TempDir()
	// A source file to carry.
	cfgDir := filepath.Join(home, ".config", "demo-cli")
	if err := os.MkdirAll(cfgDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "config.toml"), []byte("k = \"v\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A [[files]]-only manifest at the priority-1 discovery path.
	mdir := filepath.Join(home, ".agentcookie", "manifests")
	if err := os.MkdirAll(mdir, 0o700); err != nil {
		t.Fatal(err)
	}
	manifest := `
schema_version = 2
name = "demo-cli"
display_name = "Demo"

[[files]]
source = "~/.config/demo-cli/config.toml"
key = "DEMO_CONFIG"
target = "demo-cli/config.toml"
env = "DEMO_CONFIG_PATH"
`
	if err := os.WriteFile(filepath.Join(mdir, "demo-cli.toml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}

	p, _ := LoadPayloadWithDiscovery(home)
	kv := p.CLIs["demo-cli"]
	if kv["DEMO_CONFIG"] == "" {
		t.Fatalf("[[files]]-only manifest did not carry the file; payload keys: %v", keysOf(kv))
	}
	if kv[CarryFileKey("DEMO_CONFIG")] != "demo-cli/config.toml" {
		t.Errorf("missing target companion: %v", keysOf(kv))
	}
	if kv[CarryFileEnvKey("DEMO_CONFIG")] != "DEMO_CONFIG_PATH" {
		t.Errorf("missing env companion: %v", keysOf(kv))
	}
}

// writePPProject creates a discoverable PP CLI project under home. When
// cfgBody is empty the config.toml is deliberately not created, modelling a
// CLI that has been installed but never authenticated.
func writePPProject(t *testing.T, home, slug, cfgBody string) {
	t.Helper()
	lib := filepath.Join(home, "printing-press", "library", slug)
	if err := os.MkdirAll(lib, 0o700); err != nil {
		t.Fatal(err)
	}
	meta := `{"cli_name": "` + slug + `", "auth_env_var_specs": [{"name": "ACCESS_TOKEN", "sensitive": true}]}`
	if err := os.WriteFile(filepath.Join(lib, ".printing-press.json"), []byte(meta), 0o600); err != nil {
		t.Fatal(err)
	}
	if cfgBody == "" {
		return
	}
	cfgDir := filepath.Join(home, ".config", slug)
	if err := os.MkdirAll(cfgDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "config.toml"), []byte(cfgBody), 0o600); err != nil {
		t.Fatal(err)
	}
}

// An installed-but-never-authenticated CLI is a normal state, not an error.
func TestLoadPayloadWithDiscovery_UnconfiguredDerivedProjectIsQuiet(t *testing.T) {
	home := t.TempDir()
	writePPProject(t, home, "unconfigured-pp-cli", "")

	p, errs := LoadPayloadWithDiscovery(home)
	if len(errs) != 0 {
		t.Errorf("unconfigured CLI must not report errors, got: %v", errs)
	}
	if kv := p.CLIs["unconfigured-pp-cli"]; len(kv) != 0 {
		t.Errorf("unconfigured CLI must contribute nothing, got: %v", keysOf(kv))
	}
}

// A configured CLI still carries, and its bytes survive verbatim.
func TestLoadPayloadWithDiscovery_ConfiguredDerivedProjectCarries(t *testing.T) {
	home := t.TempDir()
	body := "# comment\n[section]\nkey = \"value\"\n"
	writePPProject(t, home, "configured-pp-cli", body)

	p, errs := LoadPayloadWithDiscovery(home)
	if len(errs) != 0 {
		t.Errorf("unexpected errors: %v", errs)
	}
	kv := p.CLIs["configured-pp-cli"]
	enc := kv["CONFIGURED_PP_CLI_CONFIG_TOML"]
	if enc == "" {
		t.Fatalf("configured CLI did not carry its config; keys: %v", keysOf(kv))
	}
	decoded, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		t.Fatalf("payload is not valid base64: %v", err)
	}
	if string(decoded) != body {
		t.Errorf("carried bytes differ from source.\n got: %q\nwant: %q", decoded, body)
	}
	if kv[CarryFileKey("CONFIGURED_PP_CLI_CONFIG_TOML")] != "configured-pp-cli/config.toml" {
		t.Errorf("missing or wrong target companion: %v", keysOf(kv))
	}
}

// Only absence is quiet. A derived source that exists but cannot be carried is
// still a real failure and must be reported.
func TestLoadPayloadWithDiscovery_DerivedSourceThatIsNotAFileStillErrors(t *testing.T) {
	home := t.TempDir()
	writePPProject(t, home, "brokencfg-pp-cli", "")
	// Put a directory where the config.toml should be: present, but uncarryable.
	if err := os.MkdirAll(filepath.Join(home, ".config", "brokencfg-pp-cli", "config.toml"), 0o700); err != nil {
		t.Fatal(err)
	}

	_, errs := LoadPayloadWithDiscovery(home)
	if len(errs) == 0 {
		t.Fatal("a present-but-uncarryable derived source must still error")
	}
}

// Absence is quiet only for derived manifests. A hand-written manifest names a
// path its author chose, so a missing source there is a real misconfiguration.
func TestLoadPayloadWithDiscovery_ExplicitManifestMissingSourceStillErrors(t *testing.T) {
	home := t.TempDir()
	mdir := filepath.Join(home, ".agentcookie", "manifests")
	if err := os.MkdirAll(mdir, 0o700); err != nil {
		t.Fatal(err)
	}
	manifest := `
schema_version = 2
name = "explicit-cli"
display_name = "Explicit"

[[files]]
source = "~/.config/explicit-cli/config.toml"
key = "EXPLICIT_CONFIG"
target = "explicit-cli/config.toml"
`
	if err := os.WriteFile(filepath.Join(mdir, "explicit-cli.toml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}

	_, errs := LoadPayloadWithDiscovery(home)
	if len(errs) == 0 {
		t.Fatal("explicit manifest with a missing declared source must still error")
	}
}

func keysOf(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
