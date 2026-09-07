package store

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/richardcase/satchel/internal/state"
)

// isolate points defaultHome's XDG_DATA_HOME lookup at base and clears both
// generations of the home override, so MigrateHome's legacy/new paths land
// exactly where the test built its fixture.
func isolate(t *testing.T, base string) {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", base)
	t.Setenv("SATCHEL_HOME", "")
	t.Setenv("SKILLSCTL_HOME", "")
}

// legacyLayout builds a pre-rename store at base/skillsctl containing one
// receipt whose revision holds a file and whose single link is a symlink
// pointing at that revision, then returns the store root and the symlink
// path. It mirrors what an install under the old name would have left
// behind.
func legacyLayout(t *testing.T, base string) (legacyRoot, linkPath string) {
	t.Helper()
	legacyRoot = filepath.Join(base, "skillsctl")
	rev := filepath.Join(legacyRoot, "rev", "github.com", "o", "r", sha1s)
	if err := os.MkdirAll(rev, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rev, "SKILL.md"), []byte("---\nname: demo\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	linkPath = filepath.Join(base, "agents", "claude", "skills", "demo")
	if err := os.MkdirAll(filepath.Dir(linkPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(rev, linkPath); err != nil {
		t.Fatal(err)
	}

	db := state.DB{Version: 1, Receipts: map[string]*state.Receipt{
		"demo": {
			Name:     "demo",
			Channel:  "git",
			Slug:     "github.com/o/r",
			Resolved: sha1s,
			RevPath:  rev,
			Links:    []state.Link{{Target: "claude", Path: linkPath}},
		},
	}}
	blob, err := json.Marshal(db)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacyRoot, "state.json"), blob, 0o644); err != nil {
		t.Fatal(err)
	}
	return legacyRoot, linkPath
}

func TestMigrateHomeMovesLegacyStoreAndRelinks(t *testing.T) {
	base := t.TempDir()
	isolate(t, base)
	_, linkPath := legacyLayout(t, base)
	newRoot := filepath.Join(base, "satchel")

	migrated, err := MigrateHome(context.Background(), newRoot)
	if err != nil {
		t.Fatalf("MigrateHome: %v", err)
	}
	if !migrated {
		t.Fatal("MigrateHome reported migrated == false, want true")
	}
	if _, err := os.Stat(filepath.Join(base, "skillsctl")); !os.IsNotExist(err) {
		t.Errorf("legacy store still present (stat err = %v)", err)
	}

	wantRev := filepath.Join(newRoot, "rev", "github.com", "o", "r", sha1s)
	got, err := os.Readlink(linkPath)
	if err != nil {
		t.Fatalf("Readlink: %v", err)
	}
	if got != wantRev {
		t.Errorf("symlink target = %q, want %q", got, wantRev)
	}

	blob, err := os.ReadFile(filepath.Join(newRoot, "state.json"))
	if err != nil {
		t.Fatalf("read migrated state: %v", err)
	}
	var db state.DB
	if err := json.Unmarshal(blob, &db); err != nil {
		t.Fatal(err)
	}
	if got := db.Receipts["demo"].RevPath; got != wantRev {
		t.Errorf("receipt RevPath = %q, want %q", got, wantRev)
	}
}

func TestMigrateHomeNoopWhenLegacyAbsent(t *testing.T) {
	base := t.TempDir()
	isolate(t, base)
	newRoot := filepath.Join(base, "satchel")

	migrated, err := MigrateHome(context.Background(), newRoot)
	if err != nil {
		t.Fatalf("MigrateHome: %v", err)
	}
	if migrated {
		t.Error("MigrateHome reported migrated == true with no legacy store present")
	}
	if _, err := os.Stat(newRoot); !os.IsNotExist(err) {
		t.Errorf("MigrateHome created %s out of nothing", newRoot)
	}
}

func TestMigrateHomeNoopWhenNewAlreadyExists(t *testing.T) {
	base := t.TempDir()
	isolate(t, base)
	legacyRoot, _ := legacyLayout(t, base)
	newRoot := filepath.Join(base, "satchel")
	if err := os.MkdirAll(newRoot, 0o755); err != nil {
		t.Fatal(err)
	}

	migrated, err := MigrateHome(context.Background(), newRoot)
	if err != nil {
		t.Fatalf("MigrateHome: %v", err)
	}
	if migrated {
		t.Error("MigrateHome reported migrated == true when the new path already existed")
	}
	if _, err := os.Stat(legacyRoot); err != nil {
		t.Errorf("legacy store was touched despite a conflicting new path: %v", err)
	}
}

func TestMigrateHomeNoopWhenEnvOverrideSet(t *testing.T) {
	for _, envVar := range []string{"SATCHEL_HOME", "SKILLSCTL_HOME"} {
		t.Run(envVar, func(t *testing.T) {
			base := t.TempDir()
			isolate(t, base)
			legacyRoot, _ := legacyLayout(t, base)
			newRoot := filepath.Join(base, "satchel")
			t.Setenv(envVar, "/wherever/the/user/put/it")

			migrated, err := MigrateHome(context.Background(), newRoot)
			if err != nil {
				t.Fatalf("MigrateHome: %v", err)
			}
			if migrated {
				t.Error("MigrateHome reported migrated == true despite an env override")
			}
			if _, err := os.Stat(legacyRoot); err != nil {
				t.Errorf("legacy store was touched despite an env override: %v", err)
			}
		})
	}
}
