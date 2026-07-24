package git

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReadHistoryUsesExactPathsAndDistinctChanges(t *testing.T) {
	vault := newHistoryRepo(t)
	writeHistoryFile(t, vault, "items/abc", "metadata-one")
	writeHistoryFile(t, vault, "storage/abc", "payload-one")
	writeHistoryFile(t, vault, "items/xyz", "xyz-one")
	writeHistoryFile(t, vault, "items/nested/ignored", "not-an-id")
	commitHistory(t, vault, "2026-01-01T10:00:00Z", "\x1b[31madd/upd/del nonsense\x1b[0m")

	writeHistoryFile(t, vault, "items/abc", "metadata-two")
	writeHistoryFile(t, vault, "items/xyz", "xyz-two")
	commitHistory(t, vault, "2026-01-02T10:00:00Z", "claims to delete every item")

	writeHistoryFile(t, vault, "storage/abc", "payload-two")
	commitHistory(t, vault, "2026-01-03T10:00:00Z", "manual payload edit")

	removeHistoryFile(t, vault, "items/abc")
	removeHistoryFile(t, vault, "storage/abc")
	commitHistory(t, vault, "2026-01-04T10:00:00Z", "this message says add abc")

	writeHistoryFile(t, vault, "unrelated", "later")
	commitHistory(t, vault, "2026-01-05T10:00:00Z", "malformed [] <>")

	got, err := ReadHistory(vault)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != "abc" || got[1].ID != "xyz" {
		t.Fatalf("unexpected IDs: %+v", got)
	}

	abc := got[0]
	if abc.Live {
		t.Fatal("deleted item reported live")
	}
	if abc.DeletedAt == nil || !abc.DeletedAt.Equal(mustHistoryTime(t, "2026-01-04T10:00:00Z")) {
		t.Fatalf("unexpected deletion time: %v", abc.DeletedAt)
	}
	if len(abc.ItemVersions) != 2 || len(abc.StorageVersions) != 2 {
		t.Fatalf("unexpected abc versions: items=%+v storage=%+v", abc.ItemVersions, abc.StorageVersions)
	}
	if abc.ItemVersions[0].Size != int64(len("metadata-two")) ||
		abc.StorageVersions[0].Size != int64(len("payload-two")) {
		t.Fatalf("sizes were not derived from blobs: %+v", abc)
	}
	if abc.ItemVersions[0].Timestamp.Before(abc.ItemVersions[1].Timestamp) {
		t.Fatal("item versions are not newest-first")
	}

	xyz := got[1]
	if !xyz.Live || xyz.DeletedAt != nil || len(xyz.ItemVersions) != 2 || len(xyz.StorageVersions) != 0 {
		t.Fatalf("unexpected xyz history: %+v", xyz)
	}
}

func TestReadHistoryPreservesDeletionTimeAndHandlesRecreation(t *testing.T) {
	vault := newHistoryRepo(t)
	writeHistoryFile(t, vault, "items/abc", "one")
	commitHistory(t, vault, "2026-02-01T10:00:00Z", "create")
	removeHistoryFile(t, vault, "items/abc")
	commitHistory(t, vault, "2026-02-02T10:00:00Z", "delete")
	writeHistoryFile(t, vault, "unrelated", "later")
	commitHistory(t, vault, "2026-02-03T10:00:00Z", "unrelated")

	got, err := ReadHistory(vault)
	if err != nil {
		t.Fatal(err)
	}
	if got[0].DeletedAt == nil ||
		!got[0].DeletedAt.Equal(mustHistoryTime(t, "2026-02-02T10:00:00Z")) {
		t.Fatalf("later commit reset deletion time: %v", got[0].DeletedAt)
	}

	writeHistoryFile(t, vault, "items/abc", "two")
	commitHistory(t, vault, "2026-02-04T10:00:00Z", "recreate")
	got, err = ReadHistory(vault)
	if err != nil {
		t.Fatal(err)
	}
	if !got[0].Live || got[0].DeletedAt != nil || len(got[0].ItemVersions) != 2 {
		t.Fatalf("recreation not modeled correctly: %+v", got[0])
	}
}

func TestReadHistoryUnionsStorageOnlyIDs(t *testing.T) {
	vault := newHistoryRepo(t)
	writeHistoryFile(t, vault, "storage/orphan", "opaque")
	commitHistory(t, vault, "2026-03-01T10:00:00Z", "storage only")

	got, err := ReadHistory(vault)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "orphan" || got[0].Live ||
		len(got[0].ItemVersions) != 0 || len(got[0].StorageVersions) != 1 {
		t.Fatalf("storage-only ID missing: %+v", got)
	}
}

func TestRestoreDeletedItemRestoresTextBlobByteForByte(t *testing.T) {
	vault := newHistoryRepo(t)
	want := []byte("opaque\x00encrypted\xffitem")
	writeHistoryBytes(t, vault, "items/abc", want)
	commitHistory(t, vault, "2026-04-01T10:00:00Z", "create")
	removeHistoryFile(t, vault, "items/abc")
	commitHistory(t, vault, "2026-04-02T10:00:00Z", "delete")

	if err := RestoreDeletedItem(vault, "abc"); err != nil {
		t.Fatal(err)
	}
	assertHistoryFile(t, filepath.Join(vault, "items", "abc"), want)
	status, err := runGitCommand(vault, "status", "--short", "--", "items/abc")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(status) != "?? items/abc" {
		t.Fatalf("restoration was not an ordinary uncommitted addition: %q", status)
	}
}

func TestRestoreDeletedItemRestoresNewestBinaryBlobsByteForByte(t *testing.T) {
	vault := newHistoryRepo(t)
	oldItem := []byte("old-item")
	newItem := []byte("new\x00item")
	oldStorage := []byte{0, 1, 2}
	newStorage := []byte{255, 0, 127, 42}
	writeHistoryBytes(t, vault, "items/abc", oldItem)
	writeHistoryBytes(t, vault, "storage/abc", oldStorage)
	commitHistory(t, vault, "2026-05-01T10:00:00Z", "create")
	writeHistoryBytes(t, vault, "items/abc", newItem)
	writeHistoryBytes(t, vault, "storage/abc", newStorage)
	commitHistory(t, vault, "2026-05-02T10:00:00Z", "update")
	removeHistoryFile(t, vault, "items/abc")
	removeHistoryFile(t, vault, "storage/abc")
	commitHistory(t, vault, "2026-05-03T10:00:00Z", "delete")

	if err := RestoreDeletedItem(vault, "abc"); err != nil {
		t.Fatal(err)
	}
	assertHistoryFile(t, filepath.Join(vault, "items", "abc"), newItem)
	assertHistoryFile(t, filepath.Join(vault, "storage", "abc"), newStorage)
}

func TestRestoreDeletedItemFailsSafely(t *testing.T) {
	t.Run("live ID", func(t *testing.T) {
		vault := newHistoryRepo(t)
		writeHistoryFile(t, vault, "items/abc", "live")
		commitHistory(t, vault, "2026-06-01T10:00:00Z", "create")
		err := RestoreDeletedItem(vault, "abc")
		if err == nil || !strings.Contains(err.Error(), "live in HEAD") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("missing history", func(t *testing.T) {
		vault := newHistoryRepo(t)
		writeHistoryFile(t, vault, "unrelated", "content")
		commitHistory(t, vault, "2026-06-01T10:00:00Z", "create")
		err := RestoreDeletedItem(vault, "abc")
		if err == nil || !strings.Contains(err.Error(), "no restorable Git history") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("item collision", func(t *testing.T) {
		vault := deletedHistoryRepo(t, true)
		writeHistoryFile(t, vault, "items/abc", "working collision")
		err := RestoreDeletedItem(vault, "abc")
		if err == nil || !strings.Contains(err.Error(), "already exists in the working tree") {
			t.Fatalf("unexpected error: %v", err)
		}
		assertHistoryFile(t, filepath.Join(vault, "items", "abc"), []byte("working collision"))
	})

	t.Run("storage collision", func(t *testing.T) {
		vault := deletedHistoryRepo(t, true)
		writeHistoryFile(t, vault, "storage/abc", "working collision")
		err := RestoreDeletedItem(vault, "abc")
		if err == nil || !strings.Contains(err.Error(), "storage blob") {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, statErr := os.Stat(filepath.Join(vault, "items", "abc")); !os.IsNotExist(statErr) {
			t.Fatalf("item was partially restored: %v", statErr)
		}
	})

	t.Run("storage-only history", func(t *testing.T) {
		vault := newHistoryRepo(t)
		writeHistoryFile(t, vault, "storage/abc", "orphan")
		commitHistory(t, vault, "2026-06-01T10:00:00Z", "orphan")
		removeHistoryFile(t, vault, "storage/abc")
		commitHistory(t, vault, "2026-06-02T10:00:00Z", "delete")
		err := RestoreDeletedItem(vault, "abc")
		if err == nil || !strings.Contains(err.Error(), "no restorable Git history") {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, statErr := os.Stat(filepath.Join(vault, "storage", "abc")); !os.IsNotExist(statErr) {
			t.Fatalf("incomplete recovery wrote storage: %v", statErr)
		}
	})

	t.Run("invalid ID", func(t *testing.T) {
		vault := newHistoryRepo(t)
		err := RestoreDeletedItem(vault, "../abc")
		if err == nil || !strings.Contains(err.Error(), "invalid item ID") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func newHistoryRepo(t *testing.T) string {
	t.Helper()
	vault := t.TempDir()
	for _, args := range [][]string{
		{"init"},
		{"config", "user.name", "Dredge Test"},
		{"config", "user.email", "dredge-test@example.invalid"},
	} {
		if output, err := runGitCommand(vault, args...); err != nil {
			t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, output)
		}
	}
	return vault
}

func writeHistoryFile(t *testing.T, vault, name, content string) {
	t.Helper()
	writeHistoryBytes(t, vault, name, []byte(content))
}

func writeHistoryBytes(t *testing.T, vault, name string, content []byte) {
	t.Helper()
	fullPath := filepath.Join(vault, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(fullPath), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fullPath, content, 0600); err != nil {
		t.Fatal(err)
	}
}

func deletedHistoryRepo(t *testing.T, withStorage bool) string {
	t.Helper()
	vault := newHistoryRepo(t)
	writeHistoryFile(t, vault, "items/abc", "item")
	if withStorage {
		writeHistoryFile(t, vault, "storage/abc", "storage")
	}
	commitHistory(t, vault, "2026-07-01T10:00:00Z", "create")
	removeHistoryFile(t, vault, "items/abc")
	if withStorage {
		removeHistoryFile(t, vault, "storage/abc")
	}
	commitHistory(t, vault, "2026-07-02T10:00:00Z", "delete")
	return vault
}

func assertHistoryFile(t *testing.T, filePath string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s differs: got %v want %v", filePath, got, want)
	}
	info, err := os.Stat(filePath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("%s permissions = %o, want 600", filePath, info.Mode().Perm())
	}
}

func removeHistoryFile(t *testing.T, vault, name string) {
	t.Helper()
	if err := os.Remove(filepath.Join(vault, filepath.FromSlash(name))); err != nil {
		t.Fatal(err)
	}
}

func commitHistory(t *testing.T, vault, timestamp, message string) {
	t.Helper()
	if output, err := runGitCommand(vault, "add", "-A"); err != nil {
		t.Fatalf("git add failed: %v\n%s", err, output)
	}
	cmdTimestamp := mustHistoryTime(t, timestamp).Format(time.RFC3339)
	t.Setenv("GIT_AUTHOR_DATE", cmdTimestamp)
	t.Setenv("GIT_COMMITTER_DATE", cmdTimestamp)
	if output, err := runGitCommand(vault, "commit", "-m", message); err != nil {
		t.Fatalf("git commit failed: %v\n%s", err, output)
	}
}

func mustHistoryTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}
