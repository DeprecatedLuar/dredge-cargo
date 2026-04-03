package storage

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
	"github.com/DeprecatedLuar/dredge/internal/crypto"
)

const (
	// Manifest file
	manifestFileName = "links.json"

	// Permissions
	manifestPermissions = 0600 // rw-------
	spawnedPermissions  = 0600 // rw-------
)

// LinkEntry represents a single link in the manifest
type LinkEntry struct {
	Path string `json:"path"` // Target path where symlink points (e.g., /home/user/.ssh/config)
	Hash string `json:"hash"` // SHA256 hash of spawned file content
}

// LinkManifest maps item IDs to link entries
type LinkManifest map[string]LinkEntry

// getManifestPath returns the path to links.json
func getManifestPath() (string, error) {
	dredgeDir, err := GetDredgeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dredgeDir, manifestFileName), nil
}

// LoadManifest reads and parses links.json, returns empty map if file doesn't exist
func LoadManifest() (LinkManifest, error) {
	manifestPath, err := getManifestPath()
	if err != nil {
		return nil, err
	}

	// If manifest doesn't exist, return empty map (not an error)
	if _, err := os.Stat(manifestPath); os.IsNotExist(err) {
		return make(LinkManifest), nil
	}

	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read manifest: %w", err)
	}

	var manifest LinkManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("failed to parse manifest: %w", err)
	}

	return manifest, nil
}

// SaveManifest writes the manifest to links.json
func SaveManifest(manifest LinkManifest) error {
	manifestPath, err := getManifestPath()
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal manifest: %w", err)
	}

	if err := os.WriteFile(manifestPath, data, manifestPermissions); err != nil {
		return fmt.Errorf("failed to write manifest: %w", err)
	}

	return nil
}

// GetSpawnedPath returns the path to the spawned file for an item
func GetSpawnedPath(id string) (string, error) {
	dredgeDir, err := GetDredgeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dredgeDir, spawnedDirName, id), nil
}

// CreateSpawnedFile writes plain text content to .spawned/<id>
func CreateSpawnedFile(id, content string) error {
	spawnedPath, err := GetSpawnedPath(id)
	if err != nil {
		return err
	}

	// Ensure .spawned/ directory exists with strict permissions
	spawnedDir := filepath.Dir(spawnedPath)
	if err := os.MkdirAll(spawnedDir, dirPermissions); err != nil {
		return fmt.Errorf("failed to create .spawned directory: %w", err)
	}
	// Enforce permissions even if directory already existed
	_ = os.Chmod(spawnedDir, dirPermissions)

	// Write plain text content
	if err := os.WriteFile(spawnedPath, []byte(content), spawnedPermissions); err != nil {
		return fmt.Errorf("failed to write spawned file: %w", err)
	}

	return nil
}

// RemoveSpawnedFile deletes the spawned file for an item
func RemoveSpawnedFile(id string) error {
	spawnedPath, err := GetSpawnedPath(id)
	if err != nil {
		return err
	}

	if err := os.Remove(spawnedPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove spawned file: %w", err)
	}

	return nil
}

// hashFile computes SHA256 hash of a file
func hashFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(data)
	return fmt.Sprintf("sha256:%x", hash), nil
}

// hashSpawnedFile computes SHA256 hash of a spawned file
func hashSpawnedFile(id string) (string, error) {
	spawnedPath, err := GetSpawnedPath(id)
	if err != nil {
		return "", err
	}
	return hashFile(spawnedPath)
}

// syncItemIfNeeded checks if spawned file changed and syncs to encrypted item
func syncItemIfNeeded(id string, key []byte) error {
	manifest, err := LoadManifest()
	if err != nil {
		return err
	}

	entry, exists := manifest[id]
	if !exists {
		return nil
	}

	currentHash, _ := hashSpawnedFile(id)
	if currentHash == entry.Hash {
		return nil
	}

	// Hash mismatch → sync spawned content back to encrypted item
	spawnedPath, _ := GetSpawnedPath(id)
	spawnedContent, err := os.ReadFile(spawnedPath)
	if err != nil {
		return err
	}

	// Raw read to avoid recursion (ReadItem calls syncItemIfNeeded)
	itemPath, _ := GetItemPath(id)
	encryptedData, _ := os.ReadFile(itemPath)
	decryptedData, err := crypto.Decrypt(encryptedData, key)
	if err != nil {
		return err
	}

	var item Item
	if err := toml.Unmarshal(decryptedData, &item); err != nil {
		return err
	}

	item.Content.Text = string(spawnedContent)
	return UpdateItem(id, &item, key)
}

// UpdateManifestHash recomputes and updates the hash for a linked item
func UpdateManifestHash(id string) error {
	manifest, err := LoadManifest()
	if err != nil {
		return err
	}

	entry, exists := manifest[id]
	if !exists {
		return nil
	}

	entry.Hash, _ = hashSpawnedFile(id)
	manifest[id] = entry
	return SaveManifest(manifest)
}

// IsLinked checks if an item has an active link
func IsLinked(id string) bool {
	_, exists := GetLinkedPath(id)
	return exists
}

// GetLinkedPath returns the target path from manifest
func GetLinkedPath(id string) (string, bool) {
	manifest, _ := LoadManifest()
	if entry, exists := manifest[id]; exists {
		return entry.Path, true
	}
	return "", false
}

// Link creates a symlink from targetPath to .spawned/<id>
func Link(id, targetPath string, force bool) error {
	manifest, err := LoadManifest()
	if err != nil {
		return err
	}

	if entry, exists := manifest[id]; exists {
		return fmt.Errorf("item %s already linked to %s", id, entry.Path)
	}

	key, err := crypto.GetKeyWithVerification()
	if err != nil {
		return err
	}

	item, err := ReadItem(id, key)
	if err != nil {
		return fmt.Errorf("failed to load item: %w", err)
	}

	if item.Type != TypeText {
		return fmt.Errorf("cannot link binary items")
	}

	// Handle existing file at target
	if _, err := os.Lstat(targetPath); err == nil {
		if !force {
			return fmt.Errorf("file exists at %s (use --force)", targetPath)
		}
		os.Remove(targetPath)
	}

	// Create spawned file and symlink
	if err := CreateSpawnedFile(id, item.Content.Text); err != nil {
		return err
	}

	spawnedPath, _ := GetSpawnedPath(id)
	if err := os.Symlink(spawnedPath, targetPath); err != nil {
		RemoveSpawnedFile(id)
		return fmt.Errorf("failed to create symlink: %w", err)
	}

	hash, _ := hashSpawnedFile(id)
	manifest[id] = LinkEntry{Path: targetPath, Hash: hash}
	if err := SaveManifest(manifest); err != nil {
		os.Remove(targetPath)
		RemoveSpawnedFile(id)
		return err
	}

	return nil
}

// Unlink removes the symlink, spawned file, and manifest entry
func Unlink(id string) error {
	manifest, err := LoadManifest()
	if err != nil {
		return err
	}

	entry, exists := manifest[id]
	if !exists {
		return fmt.Errorf("item %s is not linked", id)
	}

	// Sync spawned changes before unlinking (if item still exists)
	if itemExists, _ := ItemExists(id); itemExists {
		if key, err := crypto.GetKeyWithVerification(); err == nil {
			ReadItem(id, key) // Triggers sync, ignore errors
		}
	}

	// Remove symlink and spawned file (silent if missing)
	os.Remove(entry.Path)
	if spawnedPath, err := GetSpawnedPath(id); err == nil {
		os.Remove(spawnedPath)
	}

	// Remove from manifest
	delete(manifest, id)
	return SaveManifest(manifest)
}

// GetOrphanedLinkIDs returns IDs of manifest entries where the encrypted item no longer exists
func GetOrphanedLinkIDs() []string {
	manifest, err := LoadManifest()
	if err != nil {
		return nil
	}

	var orphaned []string
	for id := range manifest {
		exists, _ := ItemExists(id)
		if !exists {
			orphaned = append(orphaned, id)
		}
	}
	return orphaned
}

// RepairBrokenSymlinks recreates symlinks that are in the manifest but missing on disk
func RepairBrokenSymlinks() {
	manifest, err := LoadManifest()
	if err != nil {
		return
	}

	for id, entry := range manifest {
		if _, err := os.Lstat(entry.Path); !os.IsNotExist(err) {
			continue // symlink exists (or other error), skip
		}
		spawnedPath, err := GetSpawnedPath(id)
		if err != nil {
			continue
		}
		if _, err := os.Stat(spawnedPath); err != nil {
			continue // spawned file also missing, skip (orphan cleanup handles this)
		}
		os.Symlink(spawnedPath, entry.Path)
	}
}

// GetOrphanedSpawnedFiles returns IDs of spawned files not tracked in manifest
func GetOrphanedSpawnedFiles() []string {
	manifest, err := LoadManifest()
	if err != nil {
		return nil
	}

	dredgeDir, err := GetDredgeDir()
	if err != nil {
		return nil
	}

	spawnedDir := filepath.Join(dredgeDir, spawnedDirName)
	entries, err := os.ReadDir(spawnedDir)
	if err != nil {
		return nil
	}

	var orphaned []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		id := entry.Name()
		if _, exists := manifest[id]; !exists {
			orphaned = append(orphaned, id)
		}
	}
	return orphaned
}
