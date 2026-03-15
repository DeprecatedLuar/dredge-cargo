package storage

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/DeprecatedLuar/dredge/internal/crypto"
)

// testKey is a deterministic 32-byte key used across storage tests.
var testKey = crypto.DeriveKey("test-password-123", []byte("16-byte-salt-val"))

// setupTestEnv creates a temporary test directory and clears session cache
func setupTestEnv(t *testing.T) (cleanup func()) {
	_ = crypto.ClearSession()

	tmpDir, err := os.MkdirTemp("", "dredge-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	// Set XDG_DATA_HOME to temp directory
	oldXDG := os.Getenv("XDG_DATA_HOME")
	os.Setenv("XDG_DATA_HOME", tmpDir)

	return func() {
		os.Setenv("XDG_DATA_HOME", oldXDG)
		os.RemoveAll(tmpDir)
		_ = crypto.ClearSession()
	}
}

func TestGetDredgeDir(t *testing.T) {
	cleanup := setupTestEnv(t)
	defer cleanup()

	dir, err := GetDredgeDir()
	if err != nil {
		t.Fatalf("GetDredgeDir() failed: %v", err)
	}

	tmpDir := os.Getenv("XDG_DATA_HOME")
	expected := filepath.Join(tmpDir, "dredge")
	if dir != expected {
		t.Errorf("GetDredgeDir() = %q, want %q", dir, expected)
	}
}

func TestEnsureDirectories(t *testing.T) {
	cleanup := setupTestEnv(t)
	defer cleanup()

	if err := EnsureDirectories(); err != nil {
		t.Fatalf("EnsureDirectories() failed: %v", err)
	}

	// Check dredge directory
	dredgeDir, _ := GetDredgeDir()
	if _, err := os.Stat(dredgeDir); os.IsNotExist(err) {
		t.Error("dredge directory not created")
	}

	// Check items directory
	itemsDir, _ := GetItemsDir()
	if _, err := os.Stat(itemsDir); os.IsNotExist(err) {
		t.Error("items directory not created")
	}

	// Check spawned directory
	spawnedDir, _ := GetSpawnedDir()
	if _, err := os.Stat(spawnedDir); os.IsNotExist(err) {
		t.Error("spawned directory not created")
	}

	// Check .gitignore
	gitignorePath := filepath.Join(dredgeDir, ".gitignore")
	if _, err := os.Stat(gitignorePath); os.IsNotExist(err) {
		t.Error(".gitignore not created")
	}
}

func TestCreateAndReadItem(t *testing.T) {
	cleanup := setupTestEnv(t)
	defer cleanup()

	item := NewTextItem("Test Item", "secret content", []string{"test", "example"})

	// Create item
	if err := CreateItem("test-id", item, testKey); err != nil {
		t.Fatalf("CreateItem() failed: %v", err)
	}

	// Read item
	readItem, err := ReadItem("test-id", testKey)
	if err != nil {
		t.Fatalf("ReadItem() failed: %v", err)
	}

	// Verify
	if readItem.Title != item.Title {
		t.Errorf("Title = %q, want %q", readItem.Title, item.Title)
	}
	if readItem.Content.Text != item.Content.Text {
		t.Errorf("Content = %q, want %q", readItem.Content.Text, item.Content.Text)
	}
	if len(readItem.Tags) != len(item.Tags) {
		t.Errorf("Tags length = %d, want %d", len(readItem.Tags), len(item.Tags))
	}
}

func TestCreateItemDuplicate(t *testing.T) {
	cleanup := setupTestEnv(t)
	defer cleanup()

	item := NewTextItem("Test", "content", nil)

	// Create first time - should succeed
	if err := CreateItem("dup-test", item, testKey); err != nil {
		t.Fatalf("First CreateItem() failed: %v", err)
	}

	// Create again - should fail
	err := CreateItem("dup-test", item, testKey)
	if err == nil {
		t.Error("CreateItem() should fail for duplicate ID")
	}
}

func TestUpdateItem(t *testing.T) {
	cleanup := setupTestEnv(t)
	defer cleanup()

	// Create initial item
	item := NewTextItem("Original", "original content", []string{"tag1"})
	if err := CreateItem("update-test", item, testKey); err != nil {
		t.Fatalf("CreateItem() failed: %v", err)
	}

	// Update item
	item.Title = "Updated"
	item.Content.Text = "updated content"
	item.Tags = []string{"tag1", "tag2"}

	if err := UpdateItem("update-test", item, testKey); err != nil {
		t.Fatalf("UpdateItem() failed: %v", err)
	}

	// Read and verify
	readItem, err := ReadItem("update-test", testKey)
	if err != nil {
		t.Fatalf("ReadItem() failed: %v", err)
	}

	if readItem.Title != "Updated" {
		t.Errorf("Title = %q, want 'Updated'", readItem.Title)
	}
	if readItem.Content.Text != "updated content" {
		t.Errorf("Content = %q, want 'updated content'", readItem.Content.Text)
	}
	if len(readItem.Tags) != 2 {
		t.Errorf("Tags length = %d, want 2", len(readItem.Tags))
	}
}

func TestDeleteItem(t *testing.T) {
	cleanup := setupTestEnv(t)
	defer cleanup()

	// Create item
	item := NewTextItem("Delete Test", "content", nil)
	if err := CreateItem("delete-test", item, testKey); err != nil {
		t.Fatalf("CreateItem() failed: %v", err)
	}

	// Delete item
	if err := DeleteItem("delete-test"); err != nil {
		t.Fatalf("DeleteItem() failed: %v", err)
	}

	// Try to read - should fail
	_, err := ReadItem("delete-test", testKey)
	if err == nil {
		t.Error("ReadItem(, testKey) should fail for deleted item")
	}
}

func TestListItemIDs(t *testing.T) {
	cleanup := setupTestEnv(t)
	defer cleanup()

	// Create multiple items
	ids := []string{"item1", "item2", "item3"}
	for _, id := range ids {
		item := NewTextItem("Title", "content", nil)
		if err := CreateItem(id, item, testKey); err != nil {
			t.Fatalf("CreateItem(%q) failed: %v", id, err)
		}
	}

	// List items
	listedIDs, err := ListItemIDs()
	if err != nil {
		t.Fatalf("ListItemIDs() failed: %v", err)
	}

	if len(listedIDs) != len(ids) {
		t.Errorf("ListItemIDs() returned %d items, want %d", len(listedIDs), len(ids))
	}

	// Check all IDs are present
	idMap := make(map[string]bool)
	for _, id := range listedIDs {
		idMap[id] = true
	}

	for _, id := range ids {
		if !idMap[id] {
			t.Errorf("ID %q not found in listed IDs", id)
		}
	}
}

func TestItemExists(t *testing.T) {
	cleanup := setupTestEnv(t)
	defer cleanup()

	// Check non-existent item
	exists, err := ItemExists("nonexistent")
	if err != nil {
		t.Fatalf("ItemExists() failed: %v", err)
	}
	if exists {
		t.Error("ItemExists() should return false for non-existent item")
	}

	// Create item
	item := NewTextItem("Test", "content", nil)
	if err := CreateItem("exists-test", item, testKey); err != nil {
		t.Fatalf("CreateItem() failed: %v", err)
	}

	// Check existing item
	exists, err = ItemExists("exists-test")
	if err != nil {
		t.Fatalf("ItemExists() failed: %v", err)
	}
	if !exists {
		t.Error("ItemExists() should return true for existing item")
	}
}

func TestNewBinaryItem(t *testing.T) {
	item := NewBinaryItem("Service Key", "key.json", 2048, 0644, []string{"api", "gcp"})

	if item.Type != TypeBinary {
		t.Errorf("Type = %q, want %q", item.Type, TypeBinary)
	}
	if item.Filename != "key.json" {
		t.Errorf("Filename = %q, want 'key.json'", item.Filename)
	}
	if item.Size == nil || *item.Size != 2048 {
		if item.Size == nil {
			t.Error("Size is nil, want 2048")
		} else {
			t.Errorf("Size = %d, want 2048", *item.Size)
		}
	}
	// Content lives in storage/; item struct has no content
	if item.Content.Text != "" {
		t.Errorf("Content = %q, want empty (binary content stored in storage/)", item.Content.Text)
	}
}
