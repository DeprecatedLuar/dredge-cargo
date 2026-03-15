package commands

import (
	"fmt"

	"github.com/DeprecatedLuar/dredge/internal/crypto"
	"github.com/DeprecatedLuar/dredge/internal/storage"
)

func HandleUnlink(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: dredge unlink <id|number>")
	}

	// Resolve ID from argument (supports numbered access)
	ids, err := ResolveArgs(args)
	if err != nil {
		return err
	}

	if len(ids) == 0 {
		return fmt.Errorf("no item found")
	}

	id := ids[0]

	// Get item for display before unlinking
	key, err := crypto.GetKeyWithVerification()
	if err != nil {
		return err
	}

	item, err := storage.ReadItem(id, key)
	if err != nil {
		return fmt.Errorf("failed to read item: %w", err)
	}

	// Get linked path for display
	targetPath, exists := storage.GetLinkedPath(id)
	if !exists {
		return fmt.Errorf("item %s is not linked", id)
	}

	// Perform unlink operation
	if err := storage.Unlink(id); err != nil {
		return err
	}

	fmt.Printf("Unlinked [%s] %s (was: %s)\n", id, item.Title, targetPath)
	return nil
}
