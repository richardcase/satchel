package target

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMigrateConfigMovesLegacyFile(t *testing.T) {
	cfgHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgHome)
	t.Setenv("SATCHEL_CONFIG", "")
	t.Setenv("SKILLSCTL_CONFIG", "")

	legacyPath := filepath.Join(cfgHome, "skillsctl", "config.toml")
	if err := os.MkdirAll(filepath.Dir(legacyPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacyPath, []byte("[[target]]\nname=\"claude\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	newPath := filepath.Join(cfgHome, "satchel", "config.toml")

	migrated, err := MigrateConfig(newPath)
	if err != nil {
		t.Fatalf("MigrateConfig: %v", err)
	}
	if !migrated {
		t.Fatal("MigrateConfig reported migrated == false, want true")
	}
	if _, err := os.Stat(legacyPath); !os.IsNotExist(err) {
		t.Errorf("legacy config file still present (stat err = %v)", err)
	}
	if _, err := os.Stat(newPath); err != nil {
		t.Errorf("new config file missing: %v", err)
	}
}

func TestMigrateConfigNoopWhenAbsent(t *testing.T) {
	cfgHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgHome)
	t.Setenv("SATCHEL_CONFIG", "")
	t.Setenv("SKILLSCTL_CONFIG", "")
	newPath := filepath.Join(cfgHome, "satchel", "config.toml")

	migrated, err := MigrateConfig(newPath)
	if err != nil {
		t.Fatalf("MigrateConfig: %v", err)
	}
	if migrated {
		t.Error("MigrateConfig reported migrated == true with no legacy file present")
	}
}

func TestMigrateConfigNoopWhenNewExists(t *testing.T) {
	cfgHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgHome)
	t.Setenv("SATCHEL_CONFIG", "")
	t.Setenv("SKILLSCTL_CONFIG", "")

	legacyPath := filepath.Join(cfgHome, "skillsctl", "config.toml")
	if err := os.MkdirAll(filepath.Dir(legacyPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacyPath, []byte("legacy"), 0o644); err != nil {
		t.Fatal(err)
	}
	newPath := filepath.Join(cfgHome, "satchel", "config.toml")
	if err := os.MkdirAll(filepath.Dir(newPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newPath, []byte("current"), 0o644); err != nil {
		t.Fatal(err)
	}

	migrated, err := MigrateConfig(newPath)
	if err != nil {
		t.Fatalf("MigrateConfig: %v", err)
	}
	if migrated {
		t.Error("MigrateConfig reported migrated == true when the new path already existed")
	}
	if _, err := os.Stat(legacyPath); err != nil {
		t.Errorf("legacy config file was touched despite a conflicting new path: %v", err)
	}
}

func TestMigrateConfigNoopWhenEnvOverrideSet(t *testing.T) {
	for _, envVar := range []string{"SATCHEL_CONFIG", "SKILLSCTL_CONFIG"} {
		t.Run(envVar, func(t *testing.T) {
			cfgHome := t.TempDir()
			t.Setenv("XDG_CONFIG_HOME", cfgHome)
			t.Setenv("SATCHEL_CONFIG", "")
			t.Setenv("SKILLSCTL_CONFIG", "")

			legacyPath := filepath.Join(cfgHome, "skillsctl", "config.toml")
			if err := os.MkdirAll(filepath.Dir(legacyPath), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(legacyPath, []byte("legacy"), 0o644); err != nil {
				t.Fatal(err)
			}
			t.Setenv(envVar, "/explicit/config.toml")

			newPath := filepath.Join(cfgHome, "satchel", "config.toml")
			migrated, err := MigrateConfig(newPath)
			if err != nil {
				t.Fatalf("MigrateConfig: %v", err)
			}
			if migrated {
				t.Error("MigrateConfig reported migrated == true despite an env override")
			}
			if _, err := os.Stat(legacyPath); err != nil {
				t.Errorf("legacy config file was touched despite an env override: %v", err)
			}
		})
	}
}
