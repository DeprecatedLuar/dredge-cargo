package storage

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/DeprecatedLuar/dredge/internal/crypto"
)

const (
	// Application
	appName = "dredge"

	// Environment variables
	xdgDataHomeEnv = "XDG_DATA_HOME"

	// Default paths
	defaultLocalDir = ".local"
	defaultShareDir = "share"

	// Directory names
	itemsDirName   = "items"
	spawnedDirName = ".spawned"
	storageDirName = "storage"

	// File names
	linksFileName     = "links.json"
	gitignoreFileName = ".gitignore"
	itemFileExt       = ""

	// Permissions
	dirPermissions      = 0700 // rwx------
	itemFilePermissions = 0600 // rw-------
	gitignorePermissions = 0644 // rw-r--r--

	// Gitignore content
	gitignoreContent = ".spawned/\nlinks.json\n"
)

// ItemType represents the type of content stored in an item
type ItemType string

const (
	TypeText   ItemType = "text"
	TypeBinary ItemType = "binary"
)

// Item represents a stored item (secret, config, file, etc.)
type Item struct {
	Title    string    `toml:"title"`
	Tags     []string  `toml:"tags,omitempty"`
	Type     ItemType  `toml:"type"`
	Created  time.Time `toml:"created"`
	Modified time.Time `toml:"modified"`

	Filename string  `toml:"filename,omitempty"`
	Size     *int64  `toml:"size,omitempty"`
	Mode     *uint32 `toml:"mode,omitempty"`

	Content ItemContent `toml:"content"`
}

// ItemContent represents the content section of an item
type ItemContent struct {
	Text string `toml:"text"`
}

// NewTextItem creates a new text item
func NewTextItem(title, content string, tags []string) *Item {
	now := time.Now()
	return &Item{
		Title:    title,
		Tags:     tags,
		Type:     TypeText,
		Created:  now,
		Modified: now,
		Content: ItemContent{
			Text: content,
		},
	}
}

// NewBinaryItem creates a new binary item (content stored separately in storage/)
func NewBinaryItem(title, filename string, size int64, mode uint32, tags []string) *Item {
	now := time.Now()
	return &Item{
		Title:    title,
		Tags:     tags,
		Type:     TypeBinary,
		Created:  now,
		Modified: now,
		Filename: filename,
		Size:     &size,
		Mode:     &mode,
	}
}

// UpdateModified updates the modified timestamp to now
func (i *Item) UpdateModified() {
	i.Modified = time.Now()
}

// GetDredgeDir returns the dredge data directory path
func GetDredgeDir() (string, error) {
	baseDir := os.Getenv(xdgDataHomeEnv)
	if baseDir == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to get home directory: %w", err)
		}
		baseDir = filepath.Join(homeDir, defaultLocalDir, defaultShareDir)
	}
	return filepath.Join(baseDir, appName), nil
}

// GetItemsDir returns the items directory path
func GetItemsDir() (string, error) {
	dredgeDir, err := GetDredgeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dredgeDir, itemsDirName), nil
}

// GetSpawnedDir returns the spawned directory path
func GetSpawnedDir() (string, error) {
	dredgeDir, err := GetDredgeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dredgeDir, spawnedDirName), nil
}

// GetStorageDir returns the storage directory path (for binary blobs)
func GetStorageDir() (string, error) {
	dredgeDir, err := GetDredgeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dredgeDir, storageDirName), nil
}

// GetStoragePath returns the full path for a binary blob file
func GetStoragePath(id string) (string, error) {
	storageDir, err := GetStorageDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(storageDir, id), nil
}

// WriteStorageBlob encrypts and writes binary data to storage/id
func WriteStorageBlob(id string, data []byte, key []byte) error {
	storageDir, err := GetStorageDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(storageDir, dirPermissions); err != nil {
		return fmt.Errorf("failed to create storage directory: %w", err)
	}

	encrypted, err := crypto.Encrypt(data, key)
	if err != nil {
		return fmt.Errorf("failed to encrypt storage blob: %w", err)
	}

	blobPath := filepath.Join(storageDir, id)
	if err := os.WriteFile(blobPath, encrypted, itemFilePermissions); err != nil {
		return fmt.Errorf("failed to write storage blob: %w", err)
	}
	return nil
}

// ReadStorageBlob decrypts and returns binary data from storage/id
func ReadStorageBlob(id string, key []byte) ([]byte, error) {
	blobPath, err := GetStoragePath(id)
	if err != nil {
		return nil, err
	}

	encrypted, err := os.ReadFile(blobPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("storage blob for '%s' not found", id)
		}
		return nil, fmt.Errorf("failed to read storage blob: %w", err)
	}

	data, err := crypto.Decrypt(encrypted, key)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt storage blob: %w", err)
	}
	return data, nil
}

// DeleteStorageBlob removes a binary blob from storage/; silent if missing
func DeleteStorageBlob(id string) error {
	blobPath, err := GetStoragePath(id)
	if err != nil {
		return err
	}
	if err := os.Remove(blobPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete storage blob: %w", err)
	}
	return nil
}

// GetLinksFilePath returns the links.json file path
func GetLinksFilePath() (string, error) {
	dredgeDir, err := GetDredgeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dredgeDir, linksFileName), nil
}

// GetItemPath returns the full path for an item file
func GetItemPath(id string) (string, error) {
	itemsDir, err := GetItemsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(itemsDir, id+itemFileExt), nil
}

// EnsureDirectories creates the dredge directory structure if it doesn't exist
func EnsureDirectories() error {
	dredgeDir, err := GetDredgeDir()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(dredgeDir, dirPermissions); err != nil {
		return fmt.Errorf("failed to create dredge directory: %w", err)
	}

	itemsDir, err := GetItemsDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(itemsDir, dirPermissions); err != nil {
		return fmt.Errorf("failed to create items directory: %w", err)
	}

	spawnedDir, err := GetSpawnedDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(spawnedDir, dirPermissions); err != nil {
		return fmt.Errorf("failed to create spawned directory: %w", err)
	}

	storageDir, err := GetStorageDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(storageDir, dirPermissions); err != nil {
		return fmt.Errorf("failed to create storage directory: %w", err)
	}

	gitignorePath := filepath.Join(dredgeDir, gitignoreFileName)
	if _, err := os.Stat(gitignorePath); os.IsNotExist(err) {
		if err := os.WriteFile(gitignorePath, []byte(gitignoreContent), gitignorePermissions); err != nil {
			return fmt.Errorf("failed to create .gitignore: %w", err)
		}
	}

	return nil
}

// CreateItem creates a new item and saves it to disk (encrypted)
func CreateItem(id string, item *Item, key []byte) error {
	if err := EnsureDirectories(); err != nil {
		return fmt.Errorf("failed to ensure directories: %w", err)
	}

	itemPath, err := GetItemPath(id)
	if err != nil {
		return fmt.Errorf("failed to get item path: %w", err)
	}

	if _, err := os.Stat(itemPath); err == nil {
		return fmt.Errorf("item with ID '%s' already exists", id)
	}

	var buf bytes.Buffer
	encoder := toml.NewEncoder(&buf)
	if err := encoder.Encode(item); err != nil {
		return fmt.Errorf("failed to encode item to TOML: %w", err)
	}

	tomlData := buf.Bytes()

	// Encrypt the TOML data
	encryptedData, err := crypto.Encrypt(tomlData, key)
	if err != nil {
		return fmt.Errorf("failed to encrypt item: %w", err)
	}

	if err := os.WriteFile(itemPath, encryptedData, itemFilePermissions); err != nil {
		return fmt.Errorf("failed to write item file: %w", err)
	}

	return nil
}

// ReadItem reads an item from disk by ID (decrypts automatically)
func ReadItem(id string, key []byte) (*Item, error) {
	// If linked, sync spawned file changes before reading
	if IsLinked(id) {
		if err := syncItemIfNeeded(id, key); err != nil {
			// Non-fatal: log warning but continue
			fmt.Fprintf(os.Stderr, "Warning: sync failed for %s: %v\n", id, err)
		}
	}

	itemPath, err := GetItemPath(id)
	if err != nil {
		return nil, fmt.Errorf("failed to get item path: %w", err)
	}

	encryptedData, err := os.ReadFile(itemPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("item '%s' not found", id)
		}
		return nil, fmt.Errorf("failed to read item file: %w", err)
	}

	data, err := crypto.Decrypt(encryptedData, key)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt item: %w", err)
	}

	var item Item
	if err := toml.Unmarshal(data, &item); err != nil {
		return nil, fmt.Errorf("failed to decode TOML: %w", err)
	}

	// Binary content lives in storage/; don't return it via ReadItem
	if item.Type == TypeBinary {
		item.Content.Text = ""
	}

	return &item, nil
}

// UpdateItem updates an existing item on disk (encrypted)
func UpdateItem(id string, item *Item, key []byte) error {
	itemPath, err := GetItemPath(id)
	if err != nil {
		return fmt.Errorf("failed to get item path: %w", err)
	}

	if _, err := os.Stat(itemPath); os.IsNotExist(err) {
		return fmt.Errorf("item '%s' not found", id)
	}

	item.UpdateModified()

	var buf bytes.Buffer
	encoder := toml.NewEncoder(&buf)
	if err := encoder.Encode(item); err != nil {
		return fmt.Errorf("failed to encode item to TOML: %w", err)
	}

	tomlData := buf.Bytes()

	// Encrypt the TOML data
	encryptedData, err := crypto.Encrypt(tomlData, key)
	if err != nil {
		return fmt.Errorf("failed to encrypt item: %w", err)
	}

	if err := os.WriteFile(itemPath, encryptedData, itemFilePermissions); err != nil {
		return fmt.Errorf("failed to write item file: %w", err)
	}

	// If linked, update spawned file and manifest hash
	if IsLinked(id) {
		if err := CreateSpawnedFile(id, item.Content.Text); err != nil {
			return fmt.Errorf("failed to update spawned file: %w", err)
		}
		if err := UpdateManifestHash(id); err != nil {
			return fmt.Errorf("failed to update manifest hash: %w", err)
		}
	}

	return nil
}

// DeleteItem removes an item from disk
func DeleteItem(id string) error {
	itemPath, err := GetItemPath(id)
	if err != nil {
		return fmt.Errorf("failed to get item path: %w", err)
	}

	if _, err := os.Stat(itemPath); os.IsNotExist(err) {
		return fmt.Errorf("item '%s' not found", id)
	}

	if err := os.Remove(itemPath); err != nil {
		return fmt.Errorf("failed to delete item: %w", err)
	}

	// Also remove storage blob if present (silent if missing)
	_ = DeleteStorageBlob(id)

	return nil
}

// ListItemIDs returns a list of all item IDs
func ListItemIDs() ([]string, error) {
	itemsDir, err := GetItemsDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get items directory: %w", err)
	}

	entries, err := os.ReadDir(itemsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, fmt.Errorf("failed to read items directory: %w", err)
	}

	var ids []string
	extLen := len(itemFileExt)
	for _, entry := range entries {
		name := entry.Name()
		if !entry.IsDir() && len(name) > extLen && name[len(name)-extLen:] == itemFileExt {
			id := name[:len(name)-extLen]
			ids = append(ids, id)
		}
	}

	return ids, nil
}

// ItemExists checks if an item with the given ID exists
func ItemExists(id string) (bool, error) {
	itemPath, err := GetItemPath(id)
	if err != nil {
		return false, fmt.Errorf("failed to get item path: %w", err)
	}

	_, err = os.Stat(itemPath)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, fmt.Errorf("failed to check item existence: %w", err)
}

