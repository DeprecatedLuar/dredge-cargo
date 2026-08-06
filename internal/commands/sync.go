package commands

import (
	"fmt"

	"github.com/DeprecatedLuar/dredge-cargo/internal/crypto"
	"github.com/DeprecatedLuar/dredge-cargo/internal/git"
	"github.com/DeprecatedLuar/dredge-cargo/internal/storage"
)

func HandleSync(args []string) error {
	// Get dredge directory
	dredgeDir, err := storage.GetDredgeDir()
	if err != nil {
		return fmt.Errorf("failed to get dredge directory: %w", err)
	}

	manifest, err := storage.LoadManifest()
	if err != nil {
		return fmt.Errorf("failed to load link manifest: %w", err)
	}

	// Only touch the key/linked items if there's actually something linked,
	// so unlinked vaults keep syncing without a password prompt.
	var key []byte
	if len(manifest) > 0 {
		key, err = crypto.GetKeyWithVerification()
		if err != nil {
			return fmt.Errorf("key error: %w", err)
		}
		if err := storage.FlushLinkedItems(key); err != nil {
			return fmt.Errorf("failed to flush linked items: %w", err)
		}
	}

	// Sync (pull + push)
	if err := git.Sync(dredgeDir); err != nil {
		return err
	}

	if len(manifest) > 0 {
		return storage.MaterializeLinkedItems(key)
	}
	return nil
}
