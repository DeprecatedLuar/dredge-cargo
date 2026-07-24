package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPushAndPullUseCommonReconciliation(t *testing.T) {
	remote, first, second := newReconcileClones(t)
	_ = remote

	writeReconcileItem(t, first, "one", "first")
	if err := Push(first); err != nil {
		t.Fatal(err)
	}
	if err := Pull(second); err != nil {
		t.Fatal(err)
	}
	assertReconcileItem(t, second, "one", "first")

	writeReconcileItem(t, second, "two", "second")
	if err := Sync(second); err != nil {
		t.Fatal(err)
	}
	if err := Pull(first); err != nil {
		t.Fatal(err)
	}
	assertReconcileItem(t, first, "two", "second")
}

func TestPullFollowsRewrittenRemoteWithDirtyAndLocalOnlyWork(t *testing.T) {
	remote, writer, offline := newReconcileClones(t)
	writeReconcileItem(t, writer, "base", "base")
	if err := Push(writer); err != nil {
		t.Fatal(err)
	}
	if err := Pull(offline); err != nil {
		t.Fatal(err)
	}
	clean := filepath.Join(filepath.Dir(remote), "clean")
	gitReconcile(t, filepath.Dir(remote), "clone", remote, clean)
	configureReconcileRepo(t, clean)

	writeReconcileItem(t, offline, "local", "local commit")
	commitReconcileChanges(t, offline, "local only")
	writeReconcileItem(t, offline, "dirty", "dirty work")

	writeReconcileItem(t, writer, "remote", "rewritten")
	commitReconcileChanges(t, writer, "remote rewrite")
	rewriteReconcileRoot(t, writer)
	gitReconcile(t, writer, "push", "--force", "origin", currentReconcileBranch(t, writer))

	if err := Pull(clean); err != nil {
		t.Fatal(err)
	}
	assertReconcileItem(t, clean, "remote", "rewritten")
	if err := Pull(offline); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		"base": "base", "local": "local commit", "dirty": "dirty work", "remote": "rewritten",
	} {
		assertReconcileItem(t, offline, name, content)
	}
	status := gitReconcile(t, offline, "status", "--porcelain")
	if strings.TrimSpace(status) != "" {
		t.Fatalf("reconciled checkout is dirty:\n%s", status)
	}
}

func TestRewrittenRemoteConflictKeepsBothStatesRecoverable(t *testing.T) {
	_, writer, offline := newReconcileClones(t)
	writeReconcileItem(t, writer, "same", "base")
	if err := Push(writer); err != nil {
		t.Fatal(err)
	}
	if err := Pull(offline); err != nil {
		t.Fatal(err)
	}

	writeReconcileItem(t, offline, "same", "offline")
	commitReconcileChanges(t, offline, "offline edit")
	localHead := strings.TrimSpace(gitReconcile(t, offline, "rev-parse", "HEAD"))

	writeReconcileItem(t, writer, "same", "remote")
	commitReconcileChanges(t, writer, "remote edit")
	rewriteReconcileRoot(t, writer)
	gitReconcile(t, writer, "push", "--force", "origin", currentReconcileBranch(t, writer))

	err := Pull(offline)
	if err == nil || !strings.Contains(err.Error(), "both states are recoverable") {
		t.Fatalf("expected recoverable conflict, got %v", err)
	}
	backup := strings.TrimSpace(gitReconcile(t, offline, "rev-parse", reconcileRefNamespace+"/local/"+localHead))
	if backup != localHead {
		t.Fatalf("local recovery ref = %s, want %s", backup, localHead)
	}
	if head := strings.TrimSpace(gitReconcile(t, offline, "rev-parse", "HEAD")); head != localHead {
		t.Fatalf("conflict moved HEAD to %s, want %s", head, localHead)
	}
}

func TestPullRecoversInterruptedRebaseBeforeReconciliation(t *testing.T) {
	_, writer, offline := newReconcileClones(t)
	writeReconcileItem(t, writer, "same", "writer")
	if err := Push(writer); err != nil {
		t.Fatal(err)
	}
	writeReconcileItem(t, offline, "same", "offline")
	commitReconcileChanges(t, offline, "offline edit")
	localHead := strings.TrimSpace(gitReconcile(t, offline, "rev-parse", "HEAD"))
	gitReconcile(t, offline, "fetch", "origin")
	if _, err := runGitCommand(offline, "rebase", "origin/"+currentReconcileBranch(t, offline)); err == nil {
		t.Fatal("fixture did not leave an interrupted conflicting rebase")
	}
	if !isRebaseInProgress(offline) {
		t.Fatal("fixture has no interrupted rebase")
	}

	err := Pull(offline)
	if err == nil || strings.Contains(err.Error(), "failed to recover interrupted reconciliation") {
		t.Fatalf("interrupted rebase was not recovered before retry: %v", err)
	}
	backup := strings.TrimSpace(gitReconcile(t, offline, "rev-parse", reconcileRefNamespace+"/local/"+localHead))
	if backup != localHead {
		t.Fatalf("recovered local ref = %s, want %s", backup, localHead)
	}
}

func newReconcileClones(t *testing.T) (remote, first, second string) {
	t.Helper()
	root := t.TempDir()
	remote = filepath.Join(root, "remote.git")
	first = filepath.Join(root, "first")
	second = filepath.Join(root, "second")
	gitReconcile(t, root, "init", "--bare", remote)
	gitReconcile(t, root, "clone", remote, first)
	configureReconcileRepo(t, first)
	if err := os.MkdirAll(filepath.Join(first, "items"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := EnsureGitIgnore(first); err != nil {
		t.Fatal(err)
	}
	writeRewriteConfig(t, first, 100, 1_000_000, 100, 1_000_000, "30d")
	writeReconcileItem(t, first, "seed", "seed")
	commitReconcileChanges(t, first, "seed")
	gitReconcile(t, first, "push", "-u", "origin", currentReconcileBranch(t, first))
	gitReconcile(t, root, "clone", remote, second)
	configureReconcileRepo(t, second)
	return remote, first, second
}

func configureReconcileRepo(t *testing.T, dir string) {
	t.Helper()
	gitReconcile(t, dir, "config", "user.name", "Dredge Test")
	gitReconcile(t, dir, "config", "user.email", "dredge@example.invalid")
}

func writeReconcileItem(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "items", name), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func assertReconcileItem(t *testing.T, dir, name, want string) {
	t.Helper()
	got, err := os.ReadFile(filepath.Join(dir, "items", name))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("items/%s = %q, want %q", name, got, want)
	}
}

func commitReconcileChanges(t *testing.T, dir, message string) {
	t.Helper()
	gitReconcile(t, dir, "add", ".")
	gitReconcile(t, dir, "commit", "-m", message)
}

func currentReconcileBranch(t *testing.T, dir string) string {
	t.Helper()
	return strings.TrimSpace(gitReconcile(t, dir, "branch", "--show-current"))
}

func rewriteReconcileRoot(t *testing.T, dir string) {
	t.Helper()
	tree := strings.TrimSpace(gitReconcile(t, dir, "rev-parse", "HEAD^{tree}"))
	newHead := strings.TrimSpace(gitReconcile(t, dir, "commit-tree", tree, "-m", "compacted history"))
	gitReconcile(t, dir, "reset", "--hard", newHead)
}

func gitReconcile(t *testing.T, dir string, args ...string) string {
	t.Helper()
	output, err := runGitCommand(dir, args...)
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, output)
	}
	return output
}
