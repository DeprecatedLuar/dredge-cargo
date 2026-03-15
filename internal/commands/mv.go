package commands

import (
	"fmt"
	"os"
	"regexp"

	"github.com/DeprecatedLuar/dredge/internal/storage"
)

var idPattern = regexp.MustCompile(`^[a-zA-Z0-9]{3}$`)

func HandleMove(args []string) error {
	if len(args) != 2 {
		return fmt.Errorf("usage: dredge mv <old-id> <new-id>")
	}

	oldID := args[0]
	newID := args[1]

	// Resolve old ID if it's a number from cache
	if resolved, err := ResolveArgs([]string{oldID}); err == nil {
		oldID = resolved[0]
	}

	// Validate old ID exists
	exists, err := storage.ItemExists(oldID)
	if err != nil {
		return fmt.Errorf("failed to check if item exists: %w", err)
	}
	if !exists {
		return fmt.Errorf("item [%s] does not exist", oldID)
	}

	// Validate new ID format (3 alphanumeric chars)
	if !idPattern.MatchString(newID) {
		return fmt.Errorf("new ID must be 3 alphanumeric characters (got: %s)", newID)
	}

	// Validate new ID doesn't already exist
	exists, err = storage.ItemExists(newID)
	if err != nil {
		return fmt.Errorf("failed to check if new ID exists: %w", err)
	}
	if exists {
		return fmt.Errorf("item [%s] already exists (cannot overwrite)", newID)
	}

	// Get file paths
	oldPath, err := storage.GetItemPath(oldID)
	if err != nil {
		return fmt.Errorf("failed to get old item path: %w", err)
	}

	newPath, err := storage.GetItemPath(newID)
	if err != nil {
		return fmt.Errorf("failed to get new item path: %w", err)
	}

	// If item is linked, unlink first (saves target path for re-linking)
	var linkTarget string
	if storage.IsLinked(oldID) {
		// Get current link target before unlinking
		target, exists := storage.GetLinkedPath(oldID)
		if !exists {
			return fmt.Errorf("item marked as linked but not in manifest")
		}
		linkTarget = target

		// Unlink (syncs changes, removes symlink, removes spawned file, updates manifest)
		if err := storage.Unlink(oldID); err != nil {
			return fmt.Errorf("failed to unlink before rename: %w", err)
		}
	}

	// Rename the encrypted item file
	if err := os.Rename(oldPath, newPath); err != nil {
		return fmt.Errorf("failed to rename item file: %w", err)
	}

	// Rename storage blob if present (binary items)
	oldBlobPath, err := storage.GetStoragePath(oldID)
	if err == nil {
		newBlobPath, err := storage.GetStoragePath(newID)
		if err == nil {
			if _, err := os.Stat(oldBlobPath); err == nil {
				os.Rename(oldBlobPath, newBlobPath) // Best-effort; non-fatal
			}
		}
	}

	// If item was linked, re-link with new ID to same target
	if linkTarget != "" {
		if err := storage.Link(newID, linkTarget, true); err != nil {
			// Try to rollback the rename
			os.Rename(newPath, oldPath)
			return fmt.Errorf("failed to re-link after rename (rolled back): %w", err)
		}
	}

	fmt.Printf("✓ Renamed [%s] → [%s]\n", oldID, newID)
	warnIfUnpushed()
	return nil
}
