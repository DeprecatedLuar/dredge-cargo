package selfheal

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrepareVaultCreatesAndRepairsRequiredFiles(t *testing.T) {
	vault := t.TempDir()
	if err := os.WriteFile(filepath.Join(vault, ".gitignore"), []byte("build/\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := PrepareVault(vault); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(vault, ".dredge", "config.toml")); err != nil {
		t.Fatalf("config was not created: %v", err)
	}
	ignore, err := os.ReadFile(filepath.Join(vault, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	for _, rule := range []string{"build/", ".spawned/", "links.json", ".dredge/*", "!.dredge/config.toml"} {
		if !strings.Contains(string(ignore), rule) {
			t.Errorf(".gitignore missing %q:\n%s", rule, ignore)
		}
	}
}

func TestPrepareVaultReturnsConfigValidationError(t *testing.T) {
	vault := t.TempDir()
	configDir := filepath.Join(vault, ".dredge")
	if err := os.MkdirAll(configDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "config.toml"), []byte("format = 1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	err := PrepareVault(vault)
	if err == nil || !strings.Contains(err.Error(), "history.items.max_versions") {
		t.Fatalf("expected config validation error, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(vault, ".gitignore")); !os.IsNotExist(statErr) {
		t.Fatal("gitignore repair ran after invalid configuration")
	}
}
