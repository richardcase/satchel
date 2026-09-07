package cli

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// captureStderr redirects the process's real os.Stderr for the duration of fn,
// which is what legacyEnvHint and the migration notices in newEnv write to
// directly rather than through cobra's SetErr.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	t.Cleanup(func() { os.Stderr = orig })

	fn()

	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	os.Stderr = orig
	blob, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(blob)
}

func TestNewEnvWarnsOnceForEachStaleSkillsctlEnvVar(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_DATA_HOME", base)
	t.Setenv("XDG_CONFIG_HOME", base)
	t.Setenv("SATCHEL_HOME", "")
	t.Setenv("SATCHEL_CONFIG", "")
	t.Setenv("SATCHEL_REGISTRY_URL", "")
	t.Setenv("SKILLSCTL_HOME", filepath.Join(base, "wherever"))
	t.Setenv("SKILLSCTL_CONFIG", filepath.Join(base, "wherever", "config.toml"))
	t.Setenv("SKILLSCTL_REGISTRY_URL", "https://example.invalid/registry.json")

	out := captureStderr(t, func() {
		if _, err := newEnv(); err != nil {
			t.Fatalf("newEnv: %v", err)
		}
	})

	for _, want := range []string{
		"SKILLSCTL_HOME is set but no longer used; rename it to SATCHEL_HOME",
		"SKILLSCTL_CONFIG is set but no longer used; rename it to SATCHEL_CONFIG",
		"SKILLSCTL_REGISTRY_URL is set but no longer used; rename it to SATCHEL_REGISTRY_URL",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("stderr = %q, want it to contain %q", out, want)
		}
	}
}

func TestNewEnvSilentWhenNoStaleEnvVarsAreSet(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_DATA_HOME", base)
	t.Setenv("XDG_CONFIG_HOME", base)
	t.Setenv("SATCHEL_HOME", "")
	t.Setenv("SATCHEL_CONFIG", "")
	t.Setenv("SATCHEL_REGISTRY_URL", "")
	t.Setenv("SKILLSCTL_HOME", "")
	t.Setenv("SKILLSCTL_CONFIG", "")
	t.Setenv("SKILLSCTL_REGISTRY_URL", "")

	out := captureStderr(t, func() {
		if _, err := newEnv(); err != nil {
			t.Fatalf("newEnv: %v", err)
		}
	})
	if out != "" {
		t.Errorf("stderr = %q, want no output with nothing stale set", out)
	}
}
