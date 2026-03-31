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
	GitIgnoreContent = `spawned/
links.json
`
)

// Init initializes a git repository for dredge and optionally connects a remote.
//
// If remote is empty, dredge runs in local-only mode (git repo without a remote).
// If remote looks like "owner/repo", it is treated as a GitHub HTTPS shorthand.
func Init(dredgeDir, remote string) error {
	// Check if git is available
	if _, err := exec.LookPath("git"); err != nil {
		return fmt.Errorf("git not found - install git")
	}

	normalizedRemote, err := normalizeRemote(remote)
	if err != nil {
		return err
	}

	// Check if already a git repo
	if isGitRepo(dredgeDir) {
		// Already initialized: if origin exists, do not overwrite.
		existing, hasOrigin := getRemoteURL(dredgeDir, "origin")
		if hasOrigin {
			if normalizedRemote != "" && strings.TrimSpace(existing) != strings.TrimSpace(normalizedRemote) {
				return fmt.Errorf("git remote 'origin' already set to %s", strings.TrimSpace(existing))
			}
			return nil
		}

		// Repo exists but no origin yet: add if provided.
		if normalizedRemote != "" {
			if err := addRemote(dredgeDir, normalizedRemote); err != nil {
				return err
			}
		}
		return nil
	}

	// Not a git repo, initialize
	if err := initRepo(dredgeDir); err != nil {
		return err
	}

	// Ensure .gitignore contains our entries
	gitignorePath := filepath.Join(dredgeDir, ".gitignore")
	if err := ensureGitIgnore(gitignorePath, GitIgnoreContent); err != nil {
		return err
	}

	// Add remote if provided
	if normalizedRemote != "" {
		if err := addRemote(dredgeDir, normalizedRemote); err != nil {
			return err
		}
	}

	// Initial commit if there are items
	itemsDir := filepath.Join(dredgeDir, "items")
	if entries, err := os.ReadDir(itemsDir); err == nil && len(entries) > 0 {
		if err := commitInitial(dredgeDir); err != nil {
			return err
		}
		if normalizedRemote != "" {
			if err := pushToRemote(dredgeDir); err != nil {
				return err
			}
		}
	}

	return nil
}

// Push commits and pushes changes to remote
func Push(dredgeDir string) error {
	if !isGitRepo(dredgeDir) {
		return fmt.Errorf("not a git repository - run 'dredge remote <url>' to add a remote")
	}

	if _, ok := getRemoteURL(dredgeDir, "origin"); !ok {
		return fmt.Errorf("no git remote configured - run 'dredge remote <url>' to add a remote")
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
		return fmt.Errorf("not a git repository - run 'dredge remote <url>' to add a remote")
	}

	if _, ok := getRemoteURL(dredgeDir, "origin"); !ok {
		return fmt.Errorf("no git remote configured - run 'dredge remote <url>' to add a remote")
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
		return fmt.Errorf("not a git repository - run 'dredge remote <url>' to add a remote")
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
	if _, ok := getRemoteURL(dredgeDir, "origin"); !ok {
		fmt.Println("\n(no remote configured - local-only mode)")
	}
	return nil
}

func commitInitial(dir string) error {
	if err := addTrackedFiles(dir); err != nil {
		return err
	}
	if _, err := runGitCommand(dir, "commit", "-m", "Initial commit"); err != nil {
		return fmt.Errorf("failed to create initial commit: %w", err)
	}
	return nil
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

	// Add storage/ if it exists (binary blobs)
	storageDir := filepath.Join(dir, "storage")
	if _, err := os.Stat(storageDir); err == nil {
		if _, err := runGitCommand(dir, "add", "storage/"); err != nil {
			return fmt.Errorf("failed to add storage: %w", err)
		}
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

// HasRemote returns true if dredgeDir is a git repo with an 'origin' remote.
func HasRemote(dredgeDir string) bool {
	_, ok := getRemoteURL(dredgeDir, "origin")
	return ok
}

// RemoteURL returns the origin remote URL and true if one is configured.
func RemoteURL(dredgeDir string) (string, bool) {
	return getRemoteURL(dredgeDir, "origin")
}

// HasUnpushedChanges returns true if dredgeDir is a git repo AND has either
// uncommitted changes in items/ or commits not yet pushed to remote.
// Silent on all errors (returns false).
func HasUnpushedChanges(dredgeDir string) bool {
	return CountUnpushedChanges(dredgeDir) > 0
}

// CountUnpushedChanges returns the number of changed items (added, modified,
// deleted) in items/ that have not been pushed. Silent on all errors (returns 0).
func CountUnpushedChanges(dredgeDir string) int {
	if !isGitRepo(dredgeDir) {
		return 0
	}

	count := 0

	// Count uncommitted changes in items/
	output, err := runGitCommand(dredgeDir, "status", "--short", "--", "items/")
	if err == nil {
		for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
			if strings.TrimSpace(line) != "" {
				count++
			}
		}
	}

	// Count items changed in unpushed commits
	output, err = runGitCommand(dredgeDir, "diff", "@{upstream}..HEAD", "--name-only", "--", "items/")
	if err == nil {
		for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
			if strings.TrimSpace(line) != "" {
				count++
			}
		}
	}

	return count
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

// addRemote adds a git remote named origin.
func addRemote(dir, remoteURL string) error {
	if _, err := runGitCommand(dir, "remote", "add", "origin", remoteURL); err != nil {
		return fmt.Errorf("failed to add remote: %w", err)
	}
	return nil
}

func getRemoteURL(dir, name string) (string, bool) {
	out, err := runGitCommand(dir, "remote", "get-url", name)
	if err != nil {
		return "", false
	}
	url := strings.TrimSpace(out)
	if url == "" {
		return "", false
	}
	return url, true
}

func normalizeRemote(remote string) (string, error) {
	r := strings.TrimSpace(remote)
	if r == "" {
		return "", nil
	}

	// GitHub shorthand: owner/repo
	if strings.Count(r, "/") == 1 &&
		!strings.Contains(r, ":") &&
		!strings.Contains(r, " ") &&
		!strings.HasPrefix(r, "./") &&
		!strings.HasPrefix(r, "../") &&
		!strings.HasPrefix(r, "/") &&
		!strings.HasSuffix(r, ".git") {
		return fmt.Sprintf("https://github.com/%s.git", r), nil
	}

	return r, nil
}

func initRepo(dir string) error {
	// Use the user's configured git default branch (init.defaultBranch).
	if _, err := runGitCommand(dir, "init"); err != nil {
		return fmt.Errorf("failed to initialize git: %w", err)
	}
	return nil
}

func ensureGitIgnore(path string, content string) error {
	// If missing, create as-is.
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			return fmt.Errorf("failed to create .gitignore: %w", err)
		}
		return nil
	}

	// If present, append any missing lines.
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read .gitignore: %w", err)
	}
	existing := string(data)

	missing := []string{}
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// match whole-line, simple contains is fine for our small patterns
		if !strings.Contains(existing, line) {
			missing = append(missing, line)
		}
	}
	if len(missing) == 0 {
		return nil
	}

	toAppend := "\n" + strings.Join(missing, "\n") + "\n"
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open .gitignore: %w", err)
	}
	defer f.Close()
	if _, err := f.WriteString(toAppend); err != nil {
		return fmt.Errorf("failed to update .gitignore: %w", err)
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
