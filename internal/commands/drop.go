package commands

import (
	"fmt"

	"github.com/DeprecatedLuar/dredge-cargo/internal/git"
	"github.com/DeprecatedLuar/dredge-cargo/internal/storage"
)

func HandleDrop(args []string, dropAll bool) error {
	dredgeDir, err := storage.GetDredgeDir()
	if err != nil {
		return fmt.Errorf("failed to get vault directory: %w", err)
	}

	// Check if this is a git repository
	if !git.IsInitialized(dredgeDir) {
		return fmt.Errorf("vault is not a git repository - run 'dredge remote <url>' first")
	}

	var ids []string
	if !dropAll && len(args) > 0 {
		// Resolve numbered args to IDs
		var err error
		ids, err = ResolveArgs(args)
		if err != nil {
			return err
		}
	}

	// Drop changes (empty ids = drop all if dropAll is true)
	return git.Drop(dredgeDir, ids)
}
