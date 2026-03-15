package git

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Constants
const (
	DefaultBranch    = "main"
	GitIgnoreContent = `spawned/
links.json
`
)

// Init initializes or connects to a GitHub repository
func Init(dredgeDir, repoSlug string) error {
	// Check if gh CLI is available
	if _, err := exec.LookPath("gh"); err != nil {
		return fmt.Errorf("gh CLI not found - install from https://cli.github.com")
	}

	// Check if authenticated with GitHub
	authCmd := exec.Command("gh", "auth", "status")
	if err := authCmd.Run(); err != nil {
		return fmt.Errorf("not authenticated with GitHub - run 'gh auth login' first")
	}

	// Check if directory exists
	if _, err := os.Stat(dredgeDir); os.IsNotExist(err) {
		return fmt.Errorf("dredge directory not found: %s", dredgeDir)
	}

	// Check if already a git repo
	if isGitRepo(dredgeDir) {
		// Already initialized, just verify remote
		remote, err := runGitCommand(dredgeDir, "remote", "get-url", "origin")
		if err == nil && remote != "" {
			return fmt.Errorf("git repository already initialized with remote: %s", strings.TrimSpace(remote))
		}
		// Has .git but no remote, add it
		if err := addRemote(dredgeDir, repoSlug); err != nil {
			return err
		}
		fmt.Printf("Added remote: %s\n", repoSlug)
		return nil
	}

	// Not a git repo, initialize
	if _, err := runGitCommand(dredgeDir, "init"); err != nil {
		return fmt.Errorf("failed to initialize git: %w", err)
	}

	// Create .gitignore
	gitignorePath := filepath.Join(dredgeDir, ".gitignore")
	if err := os.WriteFile(gitignorePath, []byte(GitIgnoreContent), 0644); err != nil {
		return fmt.Errorf("failed to create .gitignore: %w", err)
	}

	// Check if GitHub repo exists, create if not
	checkCmd := exec.Command("gh", "repo", "view", repoSlug)
	if err := checkCmd.Run(); err != nil {
		// Repo doesn't exist, create it
		fmt.Printf("Creating private repository: %s\n", repoSlug)
		createCmd := exec.Command("gh", "repo", "create", repoSlug, "--private", "--source", dredgeDir, "--remote", "origin")
		createCmd.Dir = dredgeDir
		if output, err := createCmd.CombinedOutput(); err != nil {
			return fmt.Errorf("failed to create GitHub repo: %s", string(output))
		}
	} else {
		// Repo exists, just add remote
		if err := addRemote(dredgeDir, repoSlug); err != nil {
			return err
		}
	}

	// Initial commit and push if there are items
	itemsDir := filepath.Join(dredgeDir, "items")
	if entries, err := os.ReadDir(itemsDir); err == nil && len(entries) > 0 {
		// Use centralized commit+push logic
		if err := commitAndPush(dredgeDir, true); err != nil {
			return err
		}
		fmt.Println("Initialized and pushed to GitHub")
	} else {
		fmt.Println("Initialized (no items to push yet)")
	}

	return nil
}

// Push commits and pushes changes to remote
func Push(dredgeDir string) error {
	if !isGitRepo(dredgeDir) {
		return fmt.Errorf("not a git repository - run 'dredge init <user/repo>' first")
	}

	// Always stage tracked files first
	if err := addTrackedFiles(dredgeDir); err != nil {
		return err
	}

	// Check if there are staged changes to commit
	_, err := runGitCommand(dredgeDir, "diff", "--cached", "--quiet")
	hasStagedChanges := err != nil // Error means changes exist (--quiet returns exit code 1)

	// If we have staged changes, commit them
	if hasStagedChanges {
		if err := commitChanges(dredgeDir); err != nil {
			return err
		}
	}

	// Now push everything (new commits + any unpushed commits)
	return pushToRemote(dredgeDir)
}

// Pull pulls latest changes from remote
func Pull(dredgeDir string) error {
	if !isGitRepo(dredgeDir) {
		return fmt.Errorf("not a git repository - run 'dredge init <user/repo>' first")
	}

	// Get current branch name
	branch, err := getCurrentBranch(dredgeDir)
	if err != nil {
		return fmt.Errorf("failed to get current branch: %w", err)
	}

	// Pull with rebase
	output, err := runGitCommand(dredgeDir, "pull", "--rebase", "origin", branch)
	if err != nil {
		if strings.Contains(output, "CONFLICT") || strings.Contains(err.Error(), "conflict") {
			return fmt.Errorf("merge conflicts detected - resolve manually:\n  cd %s\n  git status", dredgeDir)
		}
		return fmt.Errorf("failed to pull: %s", strings.TrimSpace(output))
	}

	if strings.Contains(output, "Already up to date") {
		fmt.Println("Already up to date")
	} else {
		fmt.Println("Pulled latest changes")
	}
	return nil
}

// Sync pulls then pushes (convenience function)
func Sync(dredgeDir string) error {
	if err := Pull(dredgeDir); err != nil {
		return err
	}
	return Push(dredgeDir)
}

// Status shows what changes will be pushed
func Status(dredgeDir string) error {
	if !isGitRepo(dredgeDir) {
		return fmt.Errorf("not a git repository - run 'dredge init <user/repo>' first")
	}

	// Stage tracked files to see what would be committed
	if err := addTrackedFiles(dredgeDir); err != nil {
		return err
	}

	// Get changed items with actions
	changes, err := getChangedItemsWithActions(dredgeDir)
	if err != nil {
		return fmt.Errorf("failed to detect changes: %w", err)
	}

	// Check if there are any changes
	totalChanges := len(changes["add"]) + len(changes["upd"]) + len(changes["del"])
	if totalChanges == 0 {
		fmt.Println("No changes to push")
		return nil
	}

	// Print colored changes
	printColoredChanges(changes)
	return nil
}

// commitAndPush is the core commit+push workflow used by Init
func commitAndPush(dir string, isInitial bool) error {
	// Add tracked files
	if err := addTrackedFiles(dir); err != nil {
		return err
	}

	// Create commit (initial or smart message)
	if isInitial {
		if _, err := runGitCommand(dir, "commit", "-m", "Initial commit"); err != nil {
			return fmt.Errorf("failed to create initial commit: %w", err)
		}
	} else {
		if err := commitChanges(dir); err != nil {
			return err
		}
	}

	// Push to remote
	return pushToRemote(dir)
}

// addTrackedFiles adds items/ and .dredge-key to git staging
func addTrackedFiles(dir string) error {
	// Add .gitignore if this is initial setup
	gitignorePath := filepath.Join(dir, ".gitignore")
	if _, err := os.Stat(gitignorePath); err == nil {
		if _, err := runGitCommand(dir, "add", ".gitignore"); err != nil {
			return fmt.Errorf("failed to add .gitignore: %w", err)
		}
	}

	// Always add items/
	if _, err := runGitCommand(dir, "add", "items/"); err != nil {
		return fmt.Errorf("failed to add items: %w", err)
	}

	// Add .dredge-key if it exists
	keyFile := filepath.Join(dir, ".dredge-key")
	if _, err := os.Stat(keyFile); err == nil {
		if _, err := runGitCommand(dir, "add", ".dredge-key"); err != nil {
			return fmt.Errorf("failed to add .dredge-key: %w", err)
		}
	}

	return nil
}

// commitChanges creates a commit with smart message based on changes
func commitChanges(dir string) error {
	// Get changed items with action types
	changes, err := getChangedItemsWithActions(dir)
	if err != nil {
		return fmt.Errorf("failed to detect changed items: %w", err)
	}

	// Format as plain text (no colors in commit message)
	commitMsg := formatCommitMessage(changes)
	if commitMsg == "" {
		commitMsg = fmt.Sprintf("Update: %s", time.Now().Format("2006-01-02 15:04:05"))
	}

	// Commit
	if _, err := runGitCommand(dir, "commit", "-m", commitMsg); err != nil {
		return fmt.Errorf("failed to commit: %w", err)
	}

	// Print colored summary
	fmt.Println()
	printColoredChanges(changes)

	return nil
}

// pushToRemote pushes all commits to remote
func pushToRemote(dir string) error {
	// Get current branch
	branch, err := getCurrentBranch(dir)
	if err != nil {
		return fmt.Errorf("failed to get current branch: %w", err)
	}

	// Push with live output
	pushCmd := exec.Command("git", "push", "-u", "origin", branch)
	pushCmd.Dir = dir
	pushCmd.Stdout = os.Stdout
	pushCmd.Stderr = os.Stderr
	if err := pushCmd.Run(); err != nil {
		return fmt.Errorf("failed to push: %w", err)
	}

	return nil
}

// IsInitialized returns true if dredgeDir is a git repository.
func IsInitialized(dredgeDir string) bool {
	return isGitRepo(dredgeDir)
}

// HasUnpushedChanges returns true if dredgeDir is a git repo AND has either
// uncommitted changes in items/ or commits not yet pushed to remote.
// Silent on all errors (returns false).
func HasUnpushedChanges(dredgeDir string) bool {
	if !isGitRepo(dredgeDir) {
		return false
	}

	// Check for uncommitted changes in items/ (tracked and untracked)
	output, err := runGitCommand(dredgeDir, "status", "--short", "--", "items/")
	if err == nil && strings.TrimSpace(output) != "" {
		return true
	}

	// Check for commits ahead of remote
	output, err = runGitCommand(dredgeDir, "rev-list", "@{upstream}..HEAD", "--count")
	if err == nil {
		count := strings.TrimSpace(output)
		return count != "" && count != "0"
	}

	return false
}

// isGitRepo checks if directory is a git repository
func isGitRepo(dir string) bool {
	gitDir := filepath.Join(dir, ".git")
	info, err := os.Stat(gitDir)
	return err == nil && info.IsDir()
}

// getChangedItemsWithActions returns a map of action -> IDs
func getChangedItemsWithActions(dir string) (map[string][]string, error) {
	// Get changed files with status: A (added), M (modified), D (deleted)
	output, err := runGitCommand(dir, "diff", "--name-status", "--cached", "items/")
	if err != nil {
		return nil, err
	}

	changes := map[string][]string{
		"add": {},
		"upd": {},
		"del": {},
	}

	lines := strings.Split(strings.TrimSpace(output), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		// Format: "A\titems/xKP" or "M\titems/gMn" or "D\titems/old"
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}

		action := parts[0]
		path := parts[1]

		// Extract ID from path: items/xKP -> xKP
		pathParts := strings.Split(path, "/")
		if len(pathParts) < 2 {
			continue
		}
		id := pathParts[1]

		// Map git status to our format
		switch action {
		case "A":
			changes["add"] = append(changes["add"], id)
		case "M":
			changes["upd"] = append(changes["upd"], id)
		case "D":
			changes["del"] = append(changes["del"], id)
		}
	}

	return changes, nil
}

// formatCommitMessage formats changes as plain text for git commit (no colors)
func formatCommitMessage(changes map[string][]string) string {
	parts := []string{}

	if len(changes["add"]) > 0 {
		ids := make([]string, len(changes["add"]))
		for i, id := range changes["add"] {
			ids[i] = "[" + id + "]"
		}
		parts = append(parts, "add "+strings.Join(ids, " "))
	}

	if len(changes["upd"]) > 0 {
		ids := make([]string, len(changes["upd"]))
		for i, id := range changes["upd"] {
			ids[i] = "[" + id + "]"
		}
		parts = append(parts, "upd "+strings.Join(ids, " "))
	}

	if len(changes["del"]) > 0 {
		ids := make([]string, len(changes["del"]))
		for i, id := range changes["del"] {
			ids[i] = "[" + id + "]"
		}
		parts = append(parts, "del "+strings.Join(ids, " "))
	}

	return strings.Join(parts, " ")
}

// printColoredChanges prints changes with colors to terminal (not for git)
func printColoredChanges(changes map[string][]string) {
	const (
		colorGreen = "\033[32m"
		colorBlue  = "\033[34m"
		colorRed   = "\033[31m"
		colorReset = "\033[0m"
	)

	if len(changes["add"]) > 0 {
		ids := make([]string, len(changes["add"]))
		for i, id := range changes["add"] {
			ids[i] = "[" + id + "]"
		}
		fmt.Println(colorGreen + "add " + strings.Join(ids, " ") + colorReset)
	}

	if len(changes["upd"]) > 0 {
		ids := make([]string, len(changes["upd"]))
		for i, id := range changes["upd"] {
			ids[i] = "[" + id + "]"
		}
		fmt.Println(colorBlue + "upd " + strings.Join(ids, " ") + colorReset)
	}

	if len(changes["del"]) > 0 {
		ids := make([]string, len(changes["del"]))
		for i, id := range changes["del"] {
			ids[i] = "[" + id + "]"
		}
		fmt.Println(colorRed + "del " + strings.Join(ids, " ") + colorReset)
	}
}

// addRemote adds a git remote using HTTPS (works with gh CLI auth)
func addRemote(dir, repoSlug string) error {
	remoteURL := fmt.Sprintf("https://github.com/%s.git", repoSlug)
	if _, err := runGitCommand(dir, "remote", "add", "origin", remoteURL); err != nil {
		return fmt.Errorf("failed to add remote: %w", err)
	}
	return nil
}

// getCurrentBranch returns the current git branch name
func getCurrentBranch(dir string) (string, error) {
	output, err := runGitCommand(dir, "branch", "--show-current")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(output), nil
}

// runGitCommand runs a git command in the specified directory
func runGitCommand(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	return string(output), err
}
