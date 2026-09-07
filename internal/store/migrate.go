package store

import (
	"context"
	"fmt"
	"os"

	"github.com/richardcase/satchel/internal/state"
	"github.com/richardcase/satchel/internal/target"
)

// MigrateHome moves the store from its pre-satchel default location to
// newRoot, and relinks every git/OCI-owned receipt onto the moved paths. It
// is a no-op unless the new default path is absent, the old default path is
// present, and neither SATCHEL_HOME nor SKILLSCTL_HOME is set — an explicit
// override means the store lives somewhere the user chose, and moving it is
// their call, not this one's.
//
// A local-channel receipt's RevPath points outside the store (see
// (*Store).Contains), so it is left untouched: there is nothing under the
// old root to move it onto.
func MigrateHome(ctx context.Context, newRoot string) (migrated bool, err error) {
	if os.Getenv("SATCHEL_HOME") != "" || os.Getenv("SKILLSCTL_HOME") != "" {
		return false, nil
	}

	legacyRoot, err := defaultHome("skillsctl")
	if err != nil {
		return false, err
	}

	if fi, statErr := os.Stat(newRoot); statErr == nil && fi.IsDir() {
		return false, nil
	}
	if fi, statErr := os.Stat(legacyRoot); statErr != nil || !fi.IsDir() {
		return false, nil
	}

	if err := os.Rename(legacyRoot, newRoot); err != nil {
		return false, fmt.Errorf("move %s to %s: %w", legacyRoot, newRoot, err)
	}

	legacy := New(legacyRoot)
	fresh := New(newRoot)

	h, err := state.Open(ctx, fresh.StatePath(), nil)
	if err != nil {
		return false, fmt.Errorf("open moved state: %w", err)
	}
	defer func() { _ = h.Close() }()

	for _, r := range h.DB.List() {
		if !legacy.Contains(r.RevPath) {
			continue
		}
		revPath, err := Join(fresh.RevPath(r.Slug, r.Resolved), r.Subpath)
		if err != nil {
			return false, fmt.Errorf("relink %s onto the moved store: %w", r.Name, err)
		}
		for _, l := range r.Links {
			// Called directly rather than staged as a plan.Relink op: this is a
			// one-time startup migration with no dry-run mode, not a
			// user-initiated mutation the plan/apply convention governs. A
			// failure partway through leaves some links repointed and the state
			// commit below not yet written; doctor/gc reconcile the rest, the
			// same characteristic the git/OCI update relink loops already have.
			if _, err := target.Relink(l.Path, revPath); err != nil {
				return false, fmt.Errorf("relink %s for %s: %w", l.Path, r.Name, err)
			}
		}
		r.RevPath = revPath
	}

	if err := h.Commit(); err != nil {
		return false, fmt.Errorf("commit migrated state: %w", err)
	}
	return true, nil
}
