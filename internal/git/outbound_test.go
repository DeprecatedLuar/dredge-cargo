package git

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPushAutomaticallyAppliesRetention(t *testing.T) {
	_, writer, reader := newReconcileClones(t)
	writeRewriteConfig(t, writer, 2, 1_000_000, 1, 1_000_000, "30d")
	commitReconcileChanges(t, writer, "configure retention")
	if err := Push(writer); err != nil {
		t.Fatal(err)
	}
	if err := Pull(reader); err != nil {
		t.Fatal(err)
	}
	writeReconcileItem(t, reader, "offline", "preserve me")

	for version := 1; version <= 3; version++ {
		writeReconcileItem(t, writer, "kept", fmt.Sprintf("version-%d", version))
		if err := Push(writer); err != nil {
			t.Fatalf("push version %d: %v", version, err)
		}
	}

	items, err := ReadHistory(writer)
	if err != nil {
		t.Fatal(err)
	}
	got := historyItemsByID(items)["kept"]
	if len(got.ItemVersions) != 2 {
		t.Fatalf("retained %d item versions, want 2", len(got.ItemVersions))
	}
	if err := Pull(reader); err != nil {
		t.Fatal(err)
	}
	assertReconcileItem(t, reader, "kept", "version-3")
	assertReconcileItem(t, reader, "offline", "preserve me")

	writeReconcileItem(t, reader, "kept", "version-4")
	if err := Sync(reader); err != nil {
		t.Fatal(err)
	}
	if err := Pull(writer); err != nil {
		t.Fatal(err)
	}
	assertReconcileItem(t, writer, "kept", "version-4")
	assertReconcileItem(t, writer, "offline", "preserve me")
	if localTree, remoteTree := strings.TrimSpace(gitReconcile(t, writer, "rev-parse", "HEAD^{tree}")),
		strings.TrimSpace(gitReconcile(t, reader, "rev-parse", "HEAD^{tree}")); localTree != remoteTree {
		t.Fatalf("current trees differ after retention: writer=%s reader=%s", localTree, remoteTree)
	}
	refs := gitReconcile(t, writer, "for-each-ref", "--format=%(refname)", rewriteBackupNamespace)
	if strings.TrimSpace(refs) != "" {
		t.Fatalf("successful push retained rewrite backups:\n%s", refs)
	}
}

func TestRetentionLeaseRejectsConcurrentRemoteUpdate(t *testing.T) {
	_, writer, concurrent := newReconcileClones(t)
	writeRewriteConfig(t, writer, 1, 1_000_000, 1, 1_000_000, "30d")
	commitReconcileChanges(t, writer, "configure retention")
	if err := Push(writer); err != nil {
		t.Fatal(err)
	}
	if err := Pull(concurrent); err != nil {
		t.Fatal(err)
	}

	writeReconcileItem(t, writer, "kept", "one")
	if err := Push(writer); err != nil {
		t.Fatal(err)
	}
	if err := Pull(concurrent); err != nil {
		t.Fatal(err)
	}
	writeReconcileItem(t, writer, "kept", "two")

	outboundSyncHook = func(step string) error {
		if step != "before-push" {
			return nil
		}
		outboundSyncHook = nil
		writeReconcileItem(t, concurrent, "concurrent", "safe")
		commitReconcileChanges(t, concurrent, "concurrent update")
		output, err := runGitCommand(concurrent, "push", "origin", currentReconcileBranch(t, concurrent))
		if err != nil {
			return fmt.Errorf("fixture concurrent push failed: %s", output)
		}
		return nil
	}
	defer func() { outboundSyncHook = nil }()

	err := Push(writer)
	if err == nil || !strings.Contains(err.Error(), "rejected safely") {
		t.Fatalf("expected safe lease rejection, got %v", err)
	}
	remoteCopy := filepath.Join(t.TempDir(), "remote-copy")
	gitReconcile(t, filepath.Dir(remoteCopy), "clone", RemoteURLForTest(t, writer), remoteCopy)
	data, readErr := os.ReadFile(filepath.Join(remoteCopy, "items", "concurrent"))
	if readErr != nil || string(data) != "safe" {
		t.Fatalf("concurrent remote update was lost: data=%q err=%v", data, readErr)
	}
	refs := gitReconcile(t, writer, "for-each-ref", "--format=%(refname)", rewriteBackupNamespace)
	if strings.TrimSpace(refs) == "" {
		t.Fatal("failed leased push did not retain rewrite recovery ref")
	}
}

func TestSuccessfulRetentionReclaimsReflogProtectedObjects(t *testing.T) {
	_, writer, _ := newReconcileClones(t)
	writeRewriteConfig(t, writer, 1, 4_000_000, 1, 4_000_000, "30d")
	commitReconcileChanges(t, writer, "configure retention")
	if err := Push(writer); err != nil {
		t.Fatal(err)
	}

	random := rand.New(rand.NewSource(42))
	var current []byte
	for version := 0; version < 3; version++ {
		current = make([]byte, 2_000_000)
		if _, err := random.Read(current); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(writer, "items", "big"), current, 0600); err != nil {
			t.Fatal(err)
		}
		if err := Push(writer); err != nil {
			t.Fatalf("push version %d: %v", version, err)
		}
	}

	unreachable, err := runGitCommand(writer, "fsck", "--unreachable", "--no-reflogs")
	if err != nil {
		t.Fatalf("git fsck failed: %v\n%s", err, unreachable)
	}
	if strings.TrimSpace(unreachable) != "" {
		t.Fatalf("post-retention objects remain unreachable:\n%s", unreachable)
	}
	items, err := ReadHistory(writer)
	if err != nil {
		t.Fatal(err)
	}
	if versions := len(historyItemsByID(items)["big"].ItemVersions); versions != 1 {
		t.Fatalf("retained %d versions, want 1", versions)
	}
	got, err := os.ReadFile(filepath.Join(writer, "items", "big"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(current) {
		t.Fatal("current file changed during physical pruning")
	}
	count := gitReconcile(t, writer, "count-objects", "-v")
	if strings.Contains(count, "garbage: ") && !strings.Contains(count, "garbage: 0\n") {
		t.Fatalf("garbage remains after pruning:\n%s", count)
	}
}

func RemoteURLForTest(t *testing.T, dir string) string {
	t.Helper()
	url, ok := getRemoteURL(dir, "origin")
	if !ok {
		t.Fatal("test repository has no origin")
	}
	return url
}
