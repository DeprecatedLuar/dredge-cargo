package commands

import (
	"fmt"

	"github.com/DeprecatedLuar/dredge/internal/git"
	"github.com/DeprecatedLuar/dredge/internal/storage"
)

// HandleRemote wires a git remote to the current active vault.
func HandleRemote(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: dredge remote <url>")
	}

	vaultDir, err := storage.GetDredgeDir()
	if err != nil {
		return fmt.Errorf("failed to get vault directory: %w", err)
	}

	if err := git.Init(vaultDir, args[0]); err != nil {
		return err
	}
	fmt.Printf("Remote set: %s\n", args[0])
	return nil
}
