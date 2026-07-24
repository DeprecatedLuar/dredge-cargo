package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureGitIgnoreCreatesDefaultAndIsIdempotent(t *testing.T) {
	vault := t.TempDir()
	if err := EnsureGitIgnore(vault); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(vault, ".gitignore")
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(requiredGitIgnore) {
		t.Fatalf("created .gitignore differs from embedded default:\n%s", got)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	modified := info.ModTime()
	if err := EnsureGitIgnore(vault); err != nil {
		t.Fatal(err)
	}
	info, err = os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().Equal(modified) {
		t.Fatal("idempotent repair rewrote .gitignore")
	}
}

func TestEnsureGitIgnoreRepairsExactRulesAndPreservesUserContent(t *testing.T) {
	vault := t.TempDir()
	path := filepath.Join(vault, ".gitignore")
	original := "# user rules\nbuild/\nspawned/\nfoo-links.json\n.dredge/*\n"
	if err := os.WriteFile(path, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}
	if err := EnsureGitIgnore(vault); err != nil {
		t.Fatal(err)
	}
	gotBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(gotBytes)
	if !strings.HasPrefix(got, original) {
		t.Fatalf("user content changed:\n%s", got)
	}
	for _, rule := range []string{".spawned/", "links.json", ".dredge/*", "!.dredge/config.toml"} {
		count := 0
		for _, line := range strings.Split(got, "\n") {
			if strings.TrimSpace(line) == rule {
				count++
			}
		}
		if count != 1 {
			t.Errorf("rule %q appears %d times", rule, count)
		}
	}
	if !strings.Contains(got, "\nspawned/\n") {
		t.Fatal("obsolete but user-owned spawned/ rule was removed")
	}
}

func TestEnsureGitIgnoreKeepsConfigExceptionAfterDirectoryRule(t *testing.T) {
	vault := t.TempDir()
	path := filepath.Join(vault, ".gitignore")
	original := "!.dredge/config.toml\n"
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	if err := EnsureGitIgnore(vault); err != nil {
		t.Fatal(err)
	}
	gotBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(gotBytes)
	ignoreIndex := strings.LastIndex(got, ".dredge/*")
	exceptionIndex := strings.LastIndex(got, "!.dredge/config.toml")
	if ignoreIndex < 0 || exceptionIndex < ignoreIndex {
		t.Fatalf("config exception does not follow directory rule:\n%s", got)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("repair changed permissions to %o", info.Mode().Perm())
	}
}

func TestAddTrackedFilesStagesOnlyConfigUnderDredgeDirectory(t *testing.T) {
	vault := t.TempDir()
	for _, dir := range []string{"items", "storage", ".dredge"} {
		if err := os.MkdirAll(filepath.Join(vault, dir), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := runGitCommand(vault, "init"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vault, ".dredge", "config.toml"), []byte("format = 1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vault, ".dredge", "private"), []byte("do not stage"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := EnsureGitIgnore(vault); err != nil {
		t.Fatal(err)
	}
	if err := addTrackedFiles(vault); err != nil {
		t.Fatal(err)
	}
	output, err := runGitCommand(vault, "diff", "--cached", "--name-only")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output, ".dredge/config.toml") {
		t.Fatalf("config not staged:\n%s", output)
	}
	if strings.Contains(output, ".dredge/private") {
		t.Fatalf("private .dredge file was staged:\n%s", output)
	}
	vaultFiles, err := getChangedVaultFiles(vault)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(vaultFiles, ",") != ".dredge/config.toml,.gitignore" {
		t.Fatalf("unexpected changed vault files: %v", vaultFiles)
	}
}
