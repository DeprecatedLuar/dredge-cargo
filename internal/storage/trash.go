package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	// Trash directory names
	trashDirName      = "Trash"
	trashFilesDirName = "files"
	trashInfoDirName  = "info"

	// Trash file naming
	trashItemPrefix        = "dredge-"
	trashStorageBlobPrefix = "dredge-storage-"
	trashInfoExt           = ".trashinfo"
)

// GetTrashDir returns the system trash directory path
func GetTrashDir() (string, error) {
	baseDir := os.Getenv(xdgDataHomeEnv)
	if baseDir == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to get home directory: %w", err)
		}
		baseDir = filepath.Join(homeDir, defaultLocalDir, defaultShareDir)
	}
	return filepath.Join(baseDir, trashDirName), nil
}

// GetTrashFilesDir returns the trash files directory path
func GetTrashFilesDir() (string, error) {
	trashDir, err := GetTrashDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(trashDir, trashFilesDirName), nil
}

// GetTrashInfoDir returns the trash info directory path
func GetTrashInfoDir() (string, error) {
	trashDir, err := GetTrashDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(trashDir, trashInfoDirName), nil
}

// GetTrashItemPath returns the path for an item in trash/files/
func GetTrashItemPath(id string) (string, error) {
	trashFilesDir, err := GetTrashFilesDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(trashFilesDir, trashItemPrefix+id), nil
}

// GetTrashInfoPath returns the path for an item's .trashinfo file
func GetTrashInfoPath(id string) (string, error) {
	trashInfoDir, err := GetTrashInfoDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(trashInfoDir, trashItemPrefix+id+trashInfoExt), nil
}

// GetTrashStorageBlobPath returns the trash path for a binary storage blob
func GetTrashStorageBlobPath(id string) (string, error) {
	trashFilesDir, err := GetTrashFilesDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(trashFilesDir, trashStorageBlobPrefix+id), nil
}

// EnsureTrashDirectories creates the trash directory structure if it doesn't exist
func EnsureTrashDirectories() error {
	trashFilesDir, err := GetTrashFilesDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(trashFilesDir, dirPermissions); err != nil {
		return fmt.Errorf("failed to create trash files directory: %w", err)
	}

	trashInfoDir, err := GetTrashInfoDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(trashInfoDir, dirPermissions); err != nil {
		return fmt.Errorf("failed to create trash info directory: %w", err)
	}

	return nil
}

// MoveToTrash moves an item to system trash and creates .trashinfo
func MoveToTrash(id string) error {
	// Ensure trash directories exist
	if err := EnsureTrashDirectories(); err != nil {
		return fmt.Errorf("failed to ensure trash directories: %w", err)
	}

	// Get paths
	itemPath, err := GetItemPath(id)
	if err != nil {
		return fmt.Errorf("failed to get item path: %w", err)
	}

	trashItemPath, err := GetTrashItemPath(id)
	if err != nil {
		return fmt.Errorf("failed to get trash item path: %w", err)
	}

	trashInfoPath, err := GetTrashInfoPath(id)
	if err != nil {
		return fmt.Errorf("failed to get trash info path: %w", err)
	}

	// Check if item exists
	if _, err := os.Stat(itemPath); os.IsNotExist(err) {
		return fmt.Errorf("item '%s' not found", id)
	}

	// Move item to trash
	if err := os.Rename(itemPath, trashItemPath); err != nil {
		return fmt.Errorf("failed to move item to trash: %w", err)
	}

	// Create .trashinfo file
	deletionDate := time.Now().Format(time.RFC3339)
	trashInfoContent := fmt.Sprintf("[Trash Info]\nPath=%s\nDeletionDate=%s\n", itemPath, deletionDate)

	if err := os.WriteFile(trashInfoPath, []byte(trashInfoContent), itemFilePermissions); err != nil {
		// Try to move item back if .trashinfo creation fails
		os.Rename(trashItemPath, itemPath)
		return fmt.Errorf("failed to create .trashinfo file: %w", err)
	}

	// Move storage blob to trash if it exists (binary items)
	blobPath, err := GetStoragePath(id)
	if err == nil {
		if _, err := os.Stat(blobPath); err == nil {
			trashBlobPath, err := GetTrashStorageBlobPath(id)
			if err == nil {
				os.Rename(blobPath, trashBlobPath) // Best-effort; non-fatal
			}
		}
	}

	return nil
}

// RestoreFromTrash restores an item from trash back to items directory
func RestoreFromTrash(id string) error {
	// Get paths
	itemPath, err := GetItemPath(id)
	if err != nil {
		return fmt.Errorf("failed to get item path: %w", err)
	}

	trashItemPath, err := GetTrashItemPath(id)
	if err != nil {
		return fmt.Errorf("failed to get trash item path: %w", err)
	}

	trashInfoPath, err := GetTrashInfoPath(id)
	if err != nil {
		return fmt.Errorf("failed to get trash info path: %w", err)
	}

	// Check if item exists in trash
	if _, err := os.Stat(trashItemPath); os.IsNotExist(err) {
		return fmt.Errorf("item '%s' not found in trash", id)
	}

	// Check if item already exists in items directory
	if _, err := os.Stat(itemPath); err == nil {
		return fmt.Errorf("item '%s' already exists in items directory", id)
	}

	// Move item back to items directory
	if err := os.Rename(trashItemPath, itemPath); err != nil {
		return fmt.Errorf("failed to restore item from trash: %w", err)
	}

	// Delete .trashinfo file
	if err := os.Remove(trashInfoPath); err != nil {
		// Non-fatal, item is already restored
		fmt.Fprintf(os.Stderr, "Warning: failed to delete .trashinfo file: %v\n", err)
	}

	// Restore storage blob if it exists in trash (binary items)
	trashBlobPath, err := GetTrashStorageBlobPath(id)
	if err == nil {
		if _, err := os.Stat(trashBlobPath); err == nil {
			blobPath, err := GetStoragePath(id)
			if err == nil {
				os.Rename(trashBlobPath, blobPath) // Best-effort; non-fatal
			}
		}
	}

	return nil
}
