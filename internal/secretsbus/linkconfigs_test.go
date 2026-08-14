package secretsbus

import (
	"os"
	"path/filepath"
	"testing"
)

// materialized writes a carried config where MaterializeFiles would put it.
func materialized(t *testing.T, home, slug string) string {
	t.Helper()
	dir := filepath.Join(agentcookieRoot(home), slug)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(p, []byte("carried = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func planFor(t *testing.T, home, slug string) LinkPlanEntry {
	t.Helper()
	plan, err := PlanConfigLinks(home)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	for _, e := range plan {
		if e.Slug == slug {
			return e
		}
	}
	t.Fatalf("no plan entry for %q; plan: %#v", slug, plan)
	return LinkPlanEntry{}
}

func TestPlanConfigLinks_AbsentDestinationIsLinkable(t *testing.T) {
	home := t.TempDir()
	materialized(t, home, "demo-pp-cli")

	e := planFor(t, home, "demo-pp-cli")
	if e.Action != LinkActionLink {
		t.Errorf("absent destination should be linkable, got %q (%s)", e.Action, e.Reason)
	}
}

// The user's own config is never replaced.
func TestPlanConfigLinks_ExistingRegularFileRefused(t *testing.T) {
	home := t.TempDir()
	materialized(t, home, "demo-pp-cli")
	dst := filepath.Join(home, ".config", "demo-pp-cli")
	if err := os.MkdirAll(dst, 0o700); err != nil {
		t.Fatal(err)
	}
	real := filepath.Join(dst, "config.toml")
	if err := os.WriteFile(real, []byte("mine = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	e := planFor(t, home, "demo-pp-cli")
	if e.Action != LinkActionRefuse {
		t.Fatalf("existing regular file must be refused, got %q", e.Action)
	}

	// And applying the plan must leave it byte-identical.
	if _, errs := ApplyConfigLinks(home, []LinkPlanEntry{e}); len(errs) == 0 {
		t.Error("applying a refused entry should report an error")
	}
	got, err := os.ReadFile(real)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "mine = true\n" {
		t.Errorf("existing config was modified: %q", got)
	}
}

// A symlink we previously created is ours to re-point.
func TestPlanConfigLinks_OwnedSymlinkIsRelinkable(t *testing.T) {
	home := t.TempDir()
	src := materialized(t, home, "demo-pp-cli")
	dst := filepath.Join(home, ".config", "demo-pp-cli")
	if err := os.MkdirAll(dst, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(src, filepath.Join(dst, "config.toml")); err != nil {
		t.Fatal(err)
	}

	e := planFor(t, home, "demo-pp-cli")
	if e.Action != LinkActionAlreadyLinked {
		t.Errorf("a symlink into ~/.agentcookie/ is ours, got %q (%s)", e.Action, e.Reason)
	}
}

// A symlink pointing somewhere else is not ours; never write through it.
func TestPlanConfigLinks_ForeignSymlinkRefused(t *testing.T) {
	home := t.TempDir()
	materialized(t, home, "demo-pp-cli")
	outside := filepath.Join(t.TempDir(), "elsewhere.toml")
	if err := os.WriteFile(outside, []byte("elsewhere = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(home, ".config", "demo-pp-cli")
	if err := os.MkdirAll(dst, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dst, "config.toml")); err != nil {
		t.Fatal(err)
	}

	e := planFor(t, home, "demo-pp-cli")
	if e.Action != LinkActionRefuse {
		t.Fatalf("foreign symlink must be refused, got %q", e.Action)
	}
	ApplyConfigLinks(home, []LinkPlanEntry{e})
	// The symlink target must not have been written through.
	got, err := os.ReadFile(outside)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "elsewhere = true\n" {
		t.Errorf("wrote through a foreign symlink: %q", got)
	}
}

func TestApplyConfigLinks_CreatesWorkingSymlink(t *testing.T) {
	home := t.TempDir()
	src := materialized(t, home, "demo-pp-cli")

	plan, err := PlanConfigLinks(home)
	if err != nil {
		t.Fatal(err)
	}
	applied, errs := ApplyConfigLinks(home, plan)
	if len(errs) != 0 {
		t.Fatalf("apply: %v", errs)
	}
	if applied != 1 {
		t.Errorf("applied = %d, want 1", applied)
	}
	dst := filepath.Join(home, ".config", "demo-pp-cli", "config.toml")
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("destination not readable: %v", err)
	}
	if string(got) != "carried = true\n" {
		t.Errorf("destination content: %q", got)
	}
	resolved, err := filepath.EvalSymlinks(dst)
	if err != nil {
		t.Fatal(err)
	}
	wantResolved, err := filepath.EvalSymlinks(src)
	if err != nil {
		t.Fatal(err)
	}
	if resolved != wantResolved {
		t.Errorf("resolved to %q, want %q", resolved, wantResolved)
	}
}

// Planning must never mutate the filesystem -- that is what makes dry-run safe.
func TestPlanConfigLinks_IsReadOnly(t *testing.T) {
	home := t.TempDir()
	materialized(t, home, "demo-pp-cli")

	if _, err := PlanConfigLinks(home); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(home, ".config", "demo-pp-cli", "config.toml")); !os.IsNotExist(err) {
		t.Errorf("planning created the destination; it must be read-only (err=%v)", err)
	}
}

// Only carried config.toml files are link candidates.
func TestPlanConfigLinks_IgnoresNonConfigCarriedFiles(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(agentcookieRoot(home), "demo-pp-cli")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cookies.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}

	plan, err := PlanConfigLinks(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan) != 0 {
		t.Errorf("non-config carried files must not be link candidates: %#v", plan)
	}
}

// A directory whose name is not a valid CLI slug is never turned into a path.
func TestPlanConfigLinks_RejectsInvalidSlug(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(agentcookieRoot(home), "Not A Slug")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte("x = 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	plan, err := PlanConfigLinks(home)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range plan {
		if e.Slug == "Not A Slug" {
			t.Errorf("invalid slug must not produce a plan entry: %#v", e)
		}
	}
}

// The bus's own directories are not CLI config candidates.
func TestPlanConfigLinks_SkipsReservedBusDirectories(t *testing.T) {
	home := t.TempDir()
	for _, reserved := range []string{"secrets", "manifests", "file-optin"} {
		dir := filepath.Join(agentcookieRoot(home), reserved)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte("x = 1\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	plan, err := PlanConfigLinks(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan) != 0 {
		t.Errorf("reserved bus directories must be skipped: %#v", plan)
	}
}
