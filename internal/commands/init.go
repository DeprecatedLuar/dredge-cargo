package commands

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/DeprecatedLuar/dredge/internal/crypto"
	"github.com/DeprecatedLuar/dredge/internal/git"
	"github.com/DeprecatedLuar/dredge/internal/storage"
)

// HandleInit bootstraps a vault at the given path (default: current dir) and activates it.
func HandleInit(args []string) error {
	if len(args) > 1 {
		return fmt.Errorf("usage: dredge init [path]")
	}

	path := "."
	if len(args) == 1 {
		path = args[0]
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("failed to resolve path: %w", err)
	}

	// Already a dredge vault — just activate it
	if isVaultDir(absPath) {
		_ = crypto.ClearSession()
		if err := storage.SetActivePath(absPath); err != nil {
			return fmt.Errorf("failed to set active vault: %w", err)
		}
		if url, ok := git.RemoteURL(absPath); ok {
			fmt.Printf("Initialized %s\n", url)
		} else {
			fmt.Printf("Initialized %s\n", absPath)
		}
		return nil
	}

	// Non-empty directory that doesn't look like a vault — prompt before continuing
	if entries, err := os.ReadDir(absPath); err == nil && len(entries) > 0 {
		fmt.Printf("Directory is not empty and does not look like a dredge vault. Continue? [y/N] ")
		scanner := bufio.NewScanner(os.Stdin)
		if !scanner.Scan() {
			return fmt.Errorf("no input provided")
		}
		if r := strings.ToLower(strings.TrimSpace(scanner.Text())); r != "y" && r != "yes" {
			fmt.Println("Aborted.")
			return nil
		}
	}

	// Create vault directory and structure
	if err := os.MkdirAll(absPath, 0700); err != nil {
		return fmt.Errorf("failed to create vault directory: %w", err)
	}

	_ = crypto.ClearSession()
	if err := storage.SetActivePath(absPath); err != nil {
		return fmt.Errorf("failed to set active vault: %w", err)
	}

	if err := storage.EnsureDirectories(); err != nil {
		return fmt.Errorf("failed to create vault structure: %w", err)
	}

	if err := git.Init(absPath, ""); err != nil {
		return fmt.Errorf("failed to initialize git repository: %w", err)
	}

	fmt.Printf("Initialized %s\n", absPath)
	return nil
}

// EnsureInitialized checks that an active vault exists and is accessible.
func EnsureInitialized() error {
	vaultDir, err := storage.GetDredgeDir()
	if err != nil {
		return fmt.Errorf("failed to determine vault directory: %w", err)
	}
	if !isVaultDir(vaultDir) {
		return fmt.Errorf("no vault initialized - run 'dredge init [path]'")
	}
	return nil
}

// isVaultDir returns true if dir contains the items/ subdirectory (dredge vault marker).
func isVaultDir(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, "items"))
	return err == nil
}
