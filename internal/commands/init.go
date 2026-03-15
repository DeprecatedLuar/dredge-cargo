package commands

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/DeprecatedLuar/dredge/internal/git"
	"github.com/DeprecatedLuar/dredge/internal/storage"
)

func HandleInit(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: dredge init <user/repo>")
	}

	repoSlug := args[0]

	// Get dredge directory
	dredgeDir, err := storage.GetDredgeDir()
	if err != nil {
		return fmt.Errorf("failed to get dredge directory: %w", err)
	}

	// Initialize git repository
	return git.Init(dredgeDir, repoSlug)
}

// EnsureInitialized checks if a git repo is connected and prompts for one if not.
// Intended to be called from the app Before hook on every command except init/help.
func EnsureInitialized() error {
	dredgeDir, err := storage.GetDredgeDir()
	if err != nil {
		return fmt.Errorf("failed to get dredge directory: %w", err)
	}

	if git.IsInitialized(dredgeDir) {
		return nil
	}

	fmt.Fprintln(os.Stderr, "No GitHub repository connected.")
	fmt.Fprint(os.Stderr, "Enter your user/repo (e.g. alice/dredge-vault): ")

	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		return fmt.Errorf("no input provided")
	}
	repoSlug := strings.TrimSpace(scanner.Text())
	if repoSlug == "" {
		return fmt.Errorf("repository cannot be empty")
	}

	if err := os.MkdirAll(dredgeDir, 0700); err != nil {
		return fmt.Errorf("failed to create dredge directory: %w", err)
	}

	return git.Init(dredgeDir, repoSlug)
}
