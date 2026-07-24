package git

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	historymodel "github.com/DeprecatedLuar/dredge-cargo/internal/history"
)

func TestRewriteHistoryAppliesRetentionAndPreservesCurrentTree(t *testing.T) {
	vault := newHistoryRepo(t)
	writeRewriteConfig(t, vault, 2, 9, 1, 7, "30d")
	writeHistoryFile(t, vault, ".dredge-key", "current-key")
	writeHistoryFile(t, vault, ".gitignore", "private/\n")
	writeHistoryFile(t, vault, "unrelated", "preserve-current-tree")

	writeHistoryFile(t, vault, "items/cnt", "c1")
	writeHistoryFile(t, vault, "items/byt", "11111")
	writeHistoryFile(t, vault, "items/big", "old")
	writeHistoryFile(t, vault, "items/rep", "same")
	writeHistoryFile(t, vault, "items/mix", "m1")
	writeHistoryFile(t, vault, "storage/mix", "s1")
	writeHistoryFile(t, vault, "items/del", "d1")
	writeHistoryFile(t, vault, "items/old", "expired")
	writeHistoryFile(t, vault, "storage/orphan", "oversized-current-storage")
	commitHistory(t, vault, "2026-01-01T10:00:00Z", "initial")

	writeHistoryFile(t, vault, "items/cnt", "c2")
	writeHistoryFile(t, vault, "items/byt", "22222")
	writeHistoryFile(t, vault, "items/big", "this-current-blob-is-oversized")
	writeHistoryFile(t, vault, "items/rep", "other")
	writeHistoryFile(t, vault, "items/mix", "m2")
	writeHistoryFile(t, vault, "storage/mix", "s2")
	writeHistoryFile(t, vault, "items/del", "d2")
	removeHistoryFile(t, vault, "items/old")
	commitHistory(t, vault, "2026-01-02T10:00:00Z", "second")

	writeHistoryFile(t, vault, "items/cnt", "c3")
	writeHistoryFile(t, vault, "items/byt", "33333")
	writeHistoryFile(t, vault, "items/rep", "same")
	writeHistoryFile(t, vault, "items/mix", "m3")
	writeHistoryFile(t, vault, "storage/mix", "s3")
	commitHistory(t, vault, "2026-01-03T10:00:00Z", "current")

	removeHistoryFile(t, vault, "items/del")
	commitHistory(t, vault, "2026-01-10T10:00:00Z", "recent deletion")

	rewriteCommitDate(t, vault, "HEAD", "2026-01-10T10:00:00Z")
	oldHead := mustGitOutput(t, vault, "rev-parse", "HEAD")
	oldTree := mustGitOutput(t, vault, "rev-parse", "HEAD^{tree}")

	result, err := RewriteHistory(vault, mustHistoryTime(t, "2026-02-05T10:00:00Z"))
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || result.OldHead != oldHead || result.NewHead == oldHead {
		t.Fatalf("unexpected rewrite result: %+v", result)
	}
	if result.BackupRef == "" || mustGitOutput(t, vault, "rev-parse", result.BackupRef) != oldHead {
		t.Fatalf("backup ref does not preserve old HEAD: %+v", result)
	}
	if got := mustGitOutput(t, vault, "rev-parse", "HEAD^{tree}"); got != oldTree {
		t.Fatalf("final tree changed: got %s want %s", got, oldTree)
	}

	items, err := ReadHistory(vault)
	if err != nil {
		t.Fatal(err)
	}
	byID := historyItemsByID(items)
	assertVersionCount(t, byID, "cnt", 2, 0)
	assertVersionCount(t, byID, "byt", 1, 0)
	assertVersionCount(t, byID, "big", 1, 0)
	assertVersionCount(t, byID, "rep", 2, 0)
	assertVersionCount(t, byID, "mix", 2, 1)
	assertVersionCount(t, byID, "del", 2, 0)
	assertVersionCount(t, byID, "orphan", 0, 1)
	if byID["del"].Live || byID["del"].DeletedAt == nil ||
		!byID["del"].DeletedAt.Equal(mustHistoryTime(t, "2026-01-10T10:00:00Z")) {
		t.Fatalf("recent deletion metadata changed: %+v", byID["del"])
	}
	if _, exists := byID["old"]; exists {
		t.Fatalf("expired deleted ID remains reachable: %+v", byID["old"])
	}

	second, err := RewriteHistory(vault, mustHistoryTime(t, "2026-02-05T10:00:00Z"))
	if err != nil {
		t.Fatal(err)
	}
	if second.Changed || second.NewHead != result.NewHead {
		t.Fatalf("repeat rewrite was not idempotent: first=%+v second=%+v", result, second)
	}
}

func TestRewriteHistoryInjectedFailuresPreserveRepositoryState(t *testing.T) {
	for _, step := range []string{"planned", "built", "verified", "backed-up", "moved"} {
		t.Run(step, func(t *testing.T) {
			vault := newRewriteFailureRepo(t)
			oldHead := mustGitOutput(t, vault, "rev-parse", "HEAD")
			oldTree := mustGitOutput(t, vault, "rev-parse", "HEAD^{tree}")
			statusBefore := mustGitOutputRaw(t, vault, "status", "--porcelain=v1")

			rewriteFailureHook = func(got string) error {
				if got == step {
					return errors.New("test failure")
				}
				return nil
			}
			defer func() { rewriteFailureHook = nil }()
			result, err := RewriteHistory(vault, mustHistoryTime(t, "2026-02-01T10:00:00Z"))
			if err == nil || !strings.Contains(err.Error(), "injected rewrite failure") {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := mustGitOutput(t, vault, "rev-parse", "HEAD"); got != oldHead {
				t.Fatalf("HEAD was not restored: got %s want %s", got, oldHead)
			}
			if got := mustGitOutput(t, vault, "rev-parse", "HEAD^{tree}"); got != oldTree {
				t.Fatalf("HEAD tree changed: got %s want %s", got, oldTree)
			}
			if got := mustGitOutputRaw(t, vault, "status", "--porcelain=v1"); got != statusBefore {
				t.Fatalf("index or working tree changed:\ngot  %q\nwant %q", got, statusBefore)
			}
			if step == "backed-up" || step == "moved" {
				if result.BackupRef == "" ||
					mustGitOutput(t, vault, "rev-parse", result.BackupRef) != oldHead {
					t.Fatalf("recoverable backup missing after %s: %+v", step, result)
				}
			}
		})
	}
}

func TestRewriteHistoryRejectsInvalidConfigurationBeforeCreatingBackup(t *testing.T) {
	vault := newHistoryRepo(t)
	writeHistoryFile(t, vault, ".dredge/config.toml", "format = 1\n")
	writeHistoryFile(t, vault, "items/abc", "one")
	commitHistory(t, vault, "2026-01-01T10:00:00Z", "initial")
	oldHead := mustGitOutput(t, vault, "rev-parse", "HEAD")

	if _, err := RewriteHistory(vault, time.Now()); err == nil ||
		!strings.Contains(err.Error(), "invalid vault configuration") {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := mustGitOutput(t, vault, "rev-parse", "HEAD"); got != oldHead {
		t.Fatalf("invalid configuration moved HEAD: got %s want %s", got, oldHead)
	}
	output, _ := gitOutput(vault, nil, "for-each-ref", "--format=%(refname)", rewriteBackupNamespace)
	if strings.TrimSpace(output) != "" {
		t.Fatalf("invalid configuration created backup refs: %q", output)
	}
}

func newRewriteFailureRepo(t *testing.T) string {
	t.Helper()
	vault := newHistoryRepo(t)
	writeRewriteConfig(t, vault, 1, 100, 1, 100, "30d")
	writeHistoryFile(t, vault, "items/abc", "one")
	writeHistoryFile(t, vault, "notes", "tracked")
	commitHistory(t, vault, "2026-01-01T10:00:00Z", "initial")
	writeHistoryFile(t, vault, "items/abc", "two")
	commitHistory(t, vault, "2026-01-02T10:00:00Z", "second")
	writeHistoryFile(t, vault, "notes", "staged")
	if output, err := runGitCommand(vault, "add", "notes"); err != nil {
		t.Fatalf("git add failed: %v\n%s", err, output)
	}
	writeHistoryFile(t, vault, "notes", "working")
	writeHistoryFile(t, vault, "untracked", "leave alone")
	return vault
}

func writeRewriteConfig(t *testing.T, vault string, itemVersions int, itemBytes int64, storageVersions int, storageBytes int64, retainFor string) {
	t.Helper()
	content := fmt.Sprintf(`format = 1

[history.items]
max_versions = %d
max_bytes_per_item = "%d"

[history.storage]
max_versions = %d
max_bytes_per_item = "%d"

[history.deleted]
retain_for = %q
`, itemVersions, itemBytes, storageVersions, storageBytes, retainFor)
	writeHistoryFile(t, vault, ".dredge/config.toml", content)
}

func historyItemsByID(items []historymodel.Item) map[string]historymodel.Item {
	result := make(map[string]historymodel.Item, len(items))
	for _, item := range items {
		result[item.ID] = item
	}
	return result
}

func assertVersionCount(t *testing.T, items map[string]historymodel.Item, id string, itemCount, storageCount int) {
	t.Helper()
	item, ok := items[id]
	if !ok {
		t.Fatalf("history missing %s", id)
	}
	if len(item.ItemVersions) != itemCount || len(item.StorageVersions) != storageCount {
		t.Fatalf("%s version counts: items=%d storage=%d, want items=%d storage=%d",
			id, len(item.ItemVersions), len(item.StorageVersions), itemCount, storageCount)
	}
}

func mustGitOutput(t *testing.T, vault string, args ...string) string {
	t.Helper()
	return strings.TrimSpace(mustGitOutputRaw(t, vault, args...))
}

func mustGitOutputRaw(t *testing.T, vault string, args ...string) string {
	t.Helper()
	output, err := runGitCommand(vault, args...)
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, output)
	}
	return output
}

// rewriteCommitDate documents that deletion age is Git-derived. The fixture
// already commits with explicit dates; amend is unnecessary and would change
// topology, so this helper verifies the expected date instead.
func rewriteCommitDate(t *testing.T, vault, ref, want string) {
	t.Helper()
	got := mustGitOutput(t, vault, "show", "-s", "--format=%cI", ref)
	expected := mustHistoryTime(t, want).Format(time.RFC3339)
	if got != expected {
		t.Fatalf("%s date = %s, want %s", ref, got, expected)
	}
}

func TestRewriteHistoryUsesTemporaryIndexOutsideRepository(t *testing.T) {
	vault := newRewriteFailureRepo(t)
	gitIndex := filepath.Join(vault, ".git", "index")
	oldTree := mustGitOutput(t, vault, "rev-parse", "HEAD^{tree}")
	before, err := os.ReadFile(gitIndex)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RewriteHistory(vault, mustHistoryTime(t, "2026-02-01T10:00:00Z")); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(gitIndex)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("rewrite modified the repository index")
	}
	if got := mustGitOutput(t, vault, "rev-parse", "HEAD^{tree}"); got != oldTree {
		t.Fatalf("rewrite changed the current tree: got %s want %s", got, oldTree)
	}
}
