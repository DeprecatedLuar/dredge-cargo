package commands

import (
	"fmt"
	"os"

	"github.com/DeprecatedLuar/dredge/internal/crypto"
	"github.com/DeprecatedLuar/dredge/internal/session"
	"github.com/DeprecatedLuar/dredge/internal/storage"
	"github.com/DeprecatedLuar/dredge/internal/ui"
)

func HandleRemove(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: dredge rm <id> [<id>...]")
	}

	// Resolve numbered args to IDs
	ids, err := ResolveArgs(args)
	if err != nil {
		return err
	}

	// Get master key once for all items
	key, err := crypto.GetKeyWithVerification()
	if err != nil {
		return fmt.Errorf("key error: %w", err)
	}

	// Track successfully deleted IDs for undo cache
	var deletedIDs []string

	// Remove each item
	for _, id := range ids {
		// Check if item exists
		exists, err := storage.ItemExists(id)
		if err != nil {
			return fmt.Errorf("failed to check item [%s]: %w", id, err)
		}
		if !exists {
			return fmt.Errorf("item [%s] not found", id)
		}

		// Read item to display title
		item, err := storage.ReadItem(id, key)
		if err != nil {
			return fmt.Errorf("failed to read item [%s]: %w", id, err)
		}

		// Unlink if item has active link (cleans up spawned file and symlink)
		if storage.IsLinked(id) {
			if err := storage.Unlink(id); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to unlink item [%s]: %v\n", id, err)
				// Continue with removal anyway
			}
		}

		// Move to trash
		if err := storage.MoveToTrash(id); err != nil {
			return fmt.Errorf("failed to move item [%s] to trash: %w", id, err)
		}

		deletedIDs = append(deletedIDs, id)
		fmt.Println(ui.FormatItem(id, item.Title, nil, "-it"))
	}

	// Cache all deleted IDs for undo
	if len(deletedIDs) > 0 {
		if err := session.CacheDeleted(deletedIDs); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to cache deleted IDs: %v\n", err)
		}
	}

	warnIfUnpushed()
	return nil
}
