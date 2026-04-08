package storage

import (
	"os"
	"path/filepath"
	"testing"
)

// Test helpers

func setupLinkTest(t *testing.T) (string, func()) {
	t.Helper()

	// Create temp directory for test
	tmpDir, err := os.MkdirTemp("", "dredge-links-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	// Override XDG_DATA_HOME
	oldDataHome := os.Getenv("XDG_DATA_HOME")
	os.Setenv("XDG_DATA_HOME", tmpDir)

	// Ensure dredge directories exist
	if err := EnsureDirectories(); err != nil {
		t.Fatalf("failed to ensure directories: %v", err)
	}

	cleanup := func() {
		os.Setenv("XDG_DATA_HOME", oldDataHome)
		os.RemoveAll(tmpDir)
	}

	return tmpDir, cleanup
}

// Manifest tests

func TestLoadManifest_EmptyWhenNotExists(t *testing.T) {
	_, cleanup := setupLinkTest(t)
	defer cleanup()

	manifest, err := LoadManifest()
	if err != nil {
		t.Fatalf("LoadManifest() failed: %v", err)
	}

	if manifest == nil {
		t.Fatal("LoadManifest() returned nil manifest")
	}

	if len(manifest) != 0 {
		t.Errorf("LoadManifest() returned non-empty manifest: got %d entries, want 0", len(manifest))
	}
}

func TestSaveAndLoadManifest(t *testing.T) {
	_, cleanup := setupLinkTest(t)
	defer cleanup()

	// Create manifest with test data
	original := LinkManifest{
		"abc": LinkEntry{
			Path: "/test/path1",
			Hash: "sha256:testhash1",
		},
		"def": LinkEntry{
			Path: "/test/path2",
			Hash: "sha256:testhash2",
		},
	}

	// Save manifest
	if err := SaveManifest(original); err != nil {
		t.Fatalf("SaveManifest() failed: %v", err)
	}

	// Load manifest
	loaded, err := LoadManifest()
	if err != nil {
		t.Fatalf("LoadManifest() failed: %v", err)
	}

	// Verify entries
	if len(loaded) != len(original) {
		t.Errorf("LoadManifest() returned wrong number of entries: got %d, want %d", len(loaded), len(original))
	}

	for id, origEntry := range original {
		loadedEntry, exists := loaded[id]
		if !exists {
			t.Errorf("LoadManifest() missing entry for ID %s", id)
			continue
		}

		if loadedEntry.Path != origEntry.Path {
			t.Errorf("LoadManifest() wrong path for ID %s: got %s, want %s", id, loadedEntry.Path, origEntry.Path)
		}

		if loadedEntry.Hash != origEntry.Hash {
			t.Errorf("LoadManifest() wrong hash for ID %s: got %s, want %s", id, loadedEntry.Hash, origEntry.Hash)
		}
	}
}

// Spawned file tests

func TestGetSpawnedPath(t *testing.T) {
	_, cleanup := setupLinkTest(t)
	defer cleanup()

	id := "testID"
	path, err := GetSpawnedPath(id)
	if err != nil {
		t.Fatalf("GetSpawnedPath() failed: %v", err)
	}

	if path == "" {
		t.Fatal("GetSpawnedPath() returned empty path")
	}

	if !filepath.IsAbs(path) {
		t.Errorf("GetSpawnedPath() returned relative path: %s", path)
	}

	if filepath.Base(path) != id {
		t.Errorf("GetSpawnedPath() returned wrong filename: got %s, want %s", filepath.Base(path), id)
	}
}

func TestCreateAndRemoveSpawnedFile(t *testing.T) {
	_, cleanup := setupLinkTest(t)
	defer cleanup()

	id := "testID"
	content := "test content\nmultiline\n"

	// Create spawned file
	if err := CreateSpawnedFile(id, content); err != nil {
		t.Fatalf("CreateSpawnedFile() failed: %v", err)
	}

	// Verify file exists
	path, err := GetSpawnedPath(id)
	if err != nil {
		t.Fatalf("GetSpawnedPath() failed: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read spawned file: %v", err)
	}

	if string(data) != content {
		t.Errorf("CreateSpawnedFile() wrote wrong content: got %q, want %q", string(data), content)
	}

	// Remove spawned file
	if err := RemoveSpawnedFile(id); err != nil {
		t.Fatalf("RemoveSpawnedFile() failed: %v", err)
	}

	// Verify file is gone
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("RemoveSpawnedFile() did not remove file: %v", err)
	}
}

func TestRemoveSpawnedFile_NotExistsIsOK(t *testing.T) {
	_, cleanup := setupLinkTest(t)
	defer cleanup()

	// Removing non-existent file should not error
	if err := RemoveSpawnedFile("nonexistent"); err != nil {
		t.Errorf("RemoveSpawnedFile() failed on non-existent file: %v", err)
	}
}

// Hash tests

func TestHashFile(t *testing.T) {
	tmpDir, cleanup := setupLinkTest(t)
	defer cleanup()

	// Create test file
	testFile := filepath.Join(tmpDir, "testfile")
	content := "test content"
	if err := os.WriteFile(testFile, []byte(content), 0600); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	// Hash file
	hash1, err := hashFile(testFile)
	if err != nil {
		t.Fatalf("hashFile() failed: %v", err)
	}

	if hash1 == "" {
		t.Fatal("hashFile() returned empty hash")
	}

	if len(hash1) < 20 {
		t.Errorf("hashFile() returned suspiciously short hash: %s", hash1)
	}

	// Hash same content again - should be identical
	hash2, err := hashFile(testFile)
	if err != nil {
		t.Fatalf("hashFile() second call failed: %v", err)
	}

	if hash1 != hash2 {
		t.Errorf("hashFile() not deterministic: got %s, then %s", hash1, hash2)
	}

	// Modify file - hash should change
	if err := os.WriteFile(testFile, []byte(content+"modified"), 0600); err != nil {
		t.Fatalf("failed to modify test file: %v", err)
	}

	hash3, err := hashFile(testFile)
	if err != nil {
		t.Fatalf("hashFile() third call failed: %v", err)
	}

	if hash1 == hash3 {
		t.Errorf("hashFile() did not change after content modification")
	}
}

// IsLinked tests

func TestIsLinked(t *testing.T) {
	_, cleanup := setupLinkTest(t)
	defer cleanup()

	id := "testID"

	// Initially not linked
	if IsLinked(id) {
		t.Error("IsLinked() returned true for non-linked item")
	}

	// Add to manifest
	manifest := LinkManifest{
		id: LinkEntry{
			Path: "/test/path",
			Hash: "sha256:testhash",
		},
	}
	if err := SaveManifest(manifest); err != nil {
		t.Fatalf("SaveManifest() failed: %v", err)
	}

	// Now should be linked
	if !IsLinked(id) {
		t.Error("IsLinked() returned false for linked item")
	}
}

func TestGetLinkedPath(t *testing.T) {
	_, cleanup := setupLinkTest(t)
	defer cleanup()

	id := "testID"
	expectedPath := "/test/path"

	// Not linked initially
	path, exists := GetLinkedPath(id)
	if exists {
		t.Error("GetLinkedPath() returned exists=true for non-linked item")
	}
	if path != "" {
		t.Errorf("GetLinkedPath() returned non-empty path for non-linked item: %s", path)
	}

	// Add to manifest
	manifest := LinkManifest{
		id: LinkEntry{
			Path: expectedPath,
			Hash: "sha256:testhash",
		},
	}
	if err := SaveManifest(manifest); err != nil {
		t.Fatalf("SaveManifest() failed: %v", err)
	}

	// Now should return path
	path, exists = GetLinkedPath(id)
	if !exists {
		t.Error("GetLinkedPath() returned exists=false for linked item")
	}
	if path != expectedPath {
		t.Errorf("GetLinkedPath() returned wrong path: got %s, want %s", path, expectedPath)
	}
}

func TestUpdateManifestHash(t *testing.T) {
	_, cleanup := setupLinkTest(t)
	defer cleanup()

	id := "testID"
	content := "test content"

	// Create spawned file
	if err := CreateSpawnedFile(id, content); err != nil {
		t.Fatalf("CreateSpawnedFile() failed: %v", err)
	}

	// Create manifest entry with wrong hash
	manifest := LinkManifest{
		id: LinkEntry{
			Path: "/test/path",
			Hash: "sha256:wronghash",
		},
	}
	if err := SaveManifest(manifest); err != nil {
		t.Fatalf("SaveManifest() failed: %v", err)
	}

	// Update hash
	if err := UpdateManifestHash(id); err != nil {
		t.Fatalf("UpdateManifestHash() failed: %v", err)
	}

	// Verify hash was updated
	updated, err := LoadManifest()
	if err != nil {
		t.Fatalf("LoadManifest() failed: %v", err)
	}

	entry, exists := updated[id]
	if !exists {
		t.Fatal("UpdateManifestHash() removed entry from manifest")
	}

	if entry.Hash == "sha256:wronghash" {
		t.Error("UpdateManifestHash() did not update hash")
	}

	// Verify hash is correct by computing it ourselves
	path, _ := GetSpawnedPath(id)
	expectedHash, err := hashFile(path)
	if err != nil {
		t.Fatalf("hashFile() failed: %v", err)
	}

	if entry.Hash != expectedHash {
		t.Errorf("UpdateManifestHash() computed wrong hash: got %s, want %s", entry.Hash, expectedHash)
	}
}

func TestUpdateManifestHash_NotLinked(t *testing.T) {
	_, cleanup := setupLinkTest(t)
	defer cleanup()

	// Updating hash for non-linked item should not error
	if err := UpdateManifestHash("nonexistent"); err != nil {
		t.Errorf("UpdateManifestHash() failed for non-linked item: %v", err)
	}
}
