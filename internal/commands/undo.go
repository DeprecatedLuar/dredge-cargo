package commands

import (
	"fmt"
	"os"

	"github.com/DeprecatedLuar/dredge/internal/crypto"
	"github.com/DeprecatedLuar/dredge/internal/session"
	"github.com/DeprecatedLuar/dredge/internal/storage"
	"github.com/DeprecatedLuar/dredge/internal/ui"
)

func HandleUndo(args []string) error {
	if len(args) > 1 {
		return fmt.Errorf("usage: dredge undo [count]")
	}

	// Parse count argument (default: restore all)
	count := 0
	if len(args) == 1 {
		if _, err := fmt.Sscanf(args[0], "%d", &count); err != nil || count <= 0 {
			return fmt.Errorf("invalid count: %s (must be positive integer)", args[0])
		}
	}

	// Get last deleted IDs from cache
	ids, err := session.GetDeleted(count)
	if err != nil {
		return fmt.Errorf("cannot undo: %w", err)
	}

	// Get password to read item titles
	password, err := crypto.GetPasswordWithVerification()
	if err != nil {
		return fmt.Errorf("password error: %w", err)
	}

	// Restore each item
	restoredIDs := []string{}
	for _, id := range ids {
		// Restore item from trash
		if err := storage.RestoreFromTrash(id); err != nil {
			// If restore fails, warn and continue with remaining items
			fmt.Fprintf(os.Stderr, "Warning: failed to restore [%s]: %v\n", id, err)
			continue
		}

		restoredIDs = append(restoredIDs, id)

		// Read item to display title in confirmation
		item, err := storage.ReadItem(id, password)
		if err != nil {
			// Non-fatal, item is already restored
			fmt.Printf("+ [%s]\n", id)
			continue
		}

		fmt.Println("+ " + ui.FormatItem(id, item.Title, item.Tags, "it#"))
	}

	// Update cache to remove restored IDs
	if len(restoredIDs) > 0 && len(restoredIDs) < len(ids) {
		// Some items were restored, update cache with remaining
		remainingIDs := []string{}
		for _, id := range ids {
			found := false
			for _, restored := range restoredIDs {
				if id == restored {
					found = true
					break
				}
			}
			if !found {
				remainingIDs = append(remainingIDs, id)
			}
		}
		if len(remainingIDs) > 0 {
			session.CacheDeleted(remainingIDs)
		}
	}

	return nil
}
