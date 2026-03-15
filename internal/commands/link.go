package commands

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/DeprecatedLuar/dredge/internal/crypto"
	"github.com/DeprecatedLuar/dredge/internal/storage"
)

func HandleLink(args []string) error {
	// Parse flags from any position
	var force, createParent bool
	var positionalArgs []string

	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "--force", "-f":
			force = true
		case "-p", "--parents":
			createParent = true
		default:
			positionalArgs = append(positionalArgs, arg)
		}
	}

	if len(positionalArgs) < 2 {
		return fmt.Errorf("usage: dredge link <id|number> <path> [--force|-f] [-p|--parents]")
	}

	// Resolve ID from first argument (supports numbered access)
	ids, err := ResolveArgs([]string{positionalArgs[0]})
	if err != nil {
		return err
	}

	if len(ids) == 0 {
		return fmt.Errorf("no item found")
	}

	id := ids[0]
	targetPath := positionalArgs[1]

	// Validate target path
	if !filepath.IsAbs(targetPath) {
		// Try to resolve relative path to absolute
		absPath, err := filepath.Abs(targetPath)
		if err != nil {
			return fmt.Errorf("target path must be absolute: %s", targetPath)
		}
		targetPath = absPath
	}

	// Check parent directory if -p flag not provided
	if !createParent {
		parentDir := filepath.Dir(targetPath)
		if _, err := os.Stat(parentDir); os.IsNotExist(err) {
			return fmt.Errorf("parent directory does not exist: %s (use -p to create)", parentDir)
		}
	} else {
		// Create parent directories if -p flag provided
		parentDir := filepath.Dir(targetPath)
		if err := os.MkdirAll(parentDir, 0755); err != nil {
			return fmt.Errorf("failed to create parent directory: %w", err)
		}
	}

	// Perform link operation
	if err := storage.Link(id, targetPath, force); err != nil {
		return err
	}

	// Get item for display
	key, err := crypto.GetKeyWithVerification()
	if err != nil {
		return err
	}

	item, err := storage.ReadItem(id, key)
	if err != nil {
		return fmt.Errorf("link created but failed to read item for display: %w", err)
	}

	fmt.Printf("Linked [%s] %s -> %s\n", id, item.Title, targetPath)
	return nil
}
