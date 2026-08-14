package secretsbus

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Config linking bridges a gap between where the bus is allowed to write and
// where a PP CLI actually reads.
//
// The bus materializes carried files strictly under ~/.agentcookie/ --
// validateMaterializeTarget enforces that, and the sink re-applies it, so a
// manifest can never name an arbitrary write path. But a PP CLI reads its
// config from ~/.config/<slug>/config.toml, and only binaries built after
// roughly 2026-07 honor XDG_CONFIG_HOME or <API>_CONFIG_DIR, so an env pointer
// reaches almost none of the installed fleet.
//
// Rather than widen the bus's write authority, linking is a separate, explicit,
// opt-in step: it plans in read-only mode, and only acts when the caller asks.
// It is the one place in the system that writes outside ~/.agentcookie/, so it
// refuses anything it does not positively recognize as safe.

// LinkAction is what a plan entry proposes to do about one destination.
type LinkAction string

const (
	// LinkActionLink means the destination is absent and can be created.
	LinkActionLink LinkAction = "link"
	// LinkActionAlreadyLinked means the destination is already a symlink into
	// ~/.agentcookie/, so it is ours and already correct.
	LinkActionAlreadyLinked LinkAction = "already-linked"
	// LinkActionRefuse means the destination is occupied by something we did
	// not create. Never overwritten.
	LinkActionRefuse LinkAction = "refuse"
)

// reservedBusDirs are ~/.agentcookie/ subdirectories owned by the bus itself
// rather than by a carried CLI config.
var reservedBusDirs = map[string]bool{
	"secrets":     true,
	"manifests":   true,
	"file-optin":  true,
	"cookies":     true,
	"state":       true,
	"logs":        true,
	"tmp":         true,
	"credentials": true,
}

// LinkPlanEntry is one proposed link, fully resolved and classified.
type LinkPlanEntry struct {
	// Slug is the CLI directory name (also the ~/.config/ directory name).
	Slug string
	// Materialized is the absolute path of the carried config under
	// ~/.agentcookie/.
	Materialized string
	// Destination is the absolute path the CLI reads.
	Destination string
	// Action is what would happen when applied.
	Action LinkAction
	// Reason explains a refusal, or is empty when there is nothing to explain.
	Reason string
}

// PlanConfigLinks scans materialized carried configs under ~/.agentcookie/ and
// classifies what linking each into ~/.config/<slug>/config.toml would do.
//
// It is strictly read-only: it creates no directories and no links, which is
// what makes it safe to run as the default dry run.
func PlanConfigLinks(homeDir string) ([]LinkPlanEntry, error) {
	root := agentcookieRoot(homeDir)
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // nothing materialized yet
		}
		return nil, fmt.Errorf("read %s: %w", root, err)
	}

	var plan []LinkPlanEntry
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		slug := e.Name()
		if reservedBusDirs[slug] {
			continue
		}
		// Re-validate the slug before it is used to compose a destination
		// path, so a malformed directory name cannot produce a surprising
		// write location.
		if !validCLIName(slug) {
			continue
		}
		src := filepath.Join(root, slug, "config.toml")
		info, err := os.Lstat(src)
		if err != nil || !info.Mode().IsRegular() {
			continue // only a real materialized config is a candidate
		}

		dst := filepath.Join(homeDir, ".config", slug, "config.toml")
		action, reason := classifyDestination(dst, root)
		plan = append(plan, LinkPlanEntry{
			Slug:         slug,
			Materialized: src,
			Destination:  dst,
			Action:       action,
			Reason:       reason,
		})
	}
	return plan, nil
}

// classifyDestination decides what may be done with dst without touching it.
// Uses Lstat throughout so a symlink is inspected, never followed.
func classifyDestination(dst, busRoot string) (LinkAction, string) {
	info, err := os.Lstat(dst)
	if err != nil {
		if os.IsNotExist(err) {
			return LinkActionLink, ""
		}
		return LinkActionRefuse, fmt.Sprintf("cannot inspect destination: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return LinkActionRefuse, "destination is an existing file; not replacing it"
	}
	target, err := os.Readlink(dst)
	if err != nil {
		return LinkActionRefuse, fmt.Sprintf("cannot read existing symlink: %v", err)
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(dst), target)
	}
	if !underRoot(target, busRoot) {
		return LinkActionRefuse, "destination is a symlink to " + target + ", which we did not create"
	}
	return LinkActionAlreadyLinked, ""
}

// underRoot reports whether p is root or lies beneath it, after cleaning.
func underRoot(p, root string) bool {
	cleanP := filepath.Clean(p)
	cleanRoot := filepath.Clean(root)
	if cleanP == cleanRoot {
		return true
	}
	return strings.HasPrefix(cleanP, cleanRoot+string(filepath.Separator))
}

// ApplyConfigLinks acts on a plan, creating only the links its entries marked
// linkable. Entries marked refuse are reported as errors and never written;
// entries already linked are skipped. Returns the number of links created.
func ApplyConfigLinks(homeDir string, plan []LinkPlanEntry) (int, []error) {
	root := agentcookieRoot(homeDir)
	var errs []error
	applied := 0
	for _, e := range plan {
		switch e.Action {
		case LinkActionAlreadyLinked:
			continue
		case LinkActionRefuse:
			errs = append(errs, fmt.Errorf("%s: %s (%s)", e.Slug, e.Reason, e.Destination))
			continue
		}
		// Re-classify immediately before writing: the plan may have been made
		// against a filesystem that has since changed, and this is the one
		// write that leaves the bus root.
		if action, reason := classifyDestination(e.Destination, root); action != LinkActionLink {
			if action == LinkActionAlreadyLinked {
				continue
			}
			errs = append(errs, fmt.Errorf("%s: %s (%s)", e.Slug, reason, e.Destination))
			continue
		}
		if err := os.MkdirAll(filepath.Dir(e.Destination), 0o700); err != nil {
			errs = append(errs, fmt.Errorf("%s: create config dir: %w", e.Slug, err))
			continue
		}
		if err := os.Symlink(e.Materialized, e.Destination); err != nil {
			errs = append(errs, fmt.Errorf("%s: link: %w", e.Slug, err))
			continue
		}
		applied++
	}
	return applied, errs
}
