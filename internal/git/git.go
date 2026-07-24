package git

import (
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	gitIgnoreFileName      = ".gitignore"
	dredgeKeyFileName      = ".dredge-key"
	dredgeConfigPath       = "dredge.toml"
	defaultDirPermissions  = 0700
	defaultFilePermissions = 0644
	atomicTempFilePattern  = ".*.tmp"
	negationRulePrefix     = "!"
	reconcileRefNamespace  = "refs/dredge/reconcile"
)

//go:embed default.gitignore
var requiredGitIgnore []byte

var requiredGitIgnoreRules = parseGitIgnoreRules(requiredGitIgnore)

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

	if err := EnsureGitIgnore(dredgeDir); err != nil {
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
	return outboundSync(dredgeDir)
}

// Pull pulls latest changes from remote
func Pull(dredgeDir string) error {
	oldHead, _ := resolveRef(dredgeDir, "HEAD")
	if err := reconcileRemote(dredgeDir); err != nil {
		return err
	}
	newHead, _ := resolveRef(dredgeDir, "HEAD")
	if oldHead == newHead {
		fmt.Println("Already up to date")
	} else {
		changes, err := getChangedItemsBetweenCommits(dredgeDir, oldHead, newHead)
		if err == nil && (len(changes["add"]) > 0 || len(changes["upd"]) > 0 || len(changes["del"]) > 0) {
			fmt.Println()
			printColoredChanges(changes)
		} else {
			fmt.Println("Pulled latest changes")
		}
	}
	return nil
}

// Drop discards uncommitted changes to specified items, reverting to last commit.
// If ids is empty, discards all uncommitted changes.
func Drop(dredgeDir string, ids []string) error {
	if !isGitRepo(dredgeDir) {
		return fmt.Errorf("not a git repository")
	}

	// Get list of items with uncommitted changes
	output, err := runGitCommand(dredgeDir, "status", "--short", "--", "items/")
	if err != nil {
		return fmt.Errorf("failed to check status: %w", err)
	}

	if strings.TrimSpace(output) == "" {
		fmt.Println("No uncommitted changes to drop")
		return nil
	}

	// Parse changed items
	changedItems := make(map[string]bool)
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if line == "" {
			continue
		}
		// Format: " M items/abc" or "?? items/xyz" or "D  items/foo"
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		path := parts[len(parts)-1] // Last field is always the path
		if strings.HasPrefix(path, "items/") {
			id := strings.TrimPrefix(path, "items/")
			changedItems[id] = true
		}
	}

	// Determine what to drop
	toDrop := []string{}
	if len(ids) == 0 {
		// Drop all
		for id := range changedItems {
			toDrop = append(toDrop, id)
		}
	} else {
		// Drop specified IDs
		for _, id := range ids {
			if !changedItems[id] {
				return fmt.Errorf("item [%s] has no uncommitted changes", id)
			}
			toDrop = append(toDrop, id)
		}
	}

	if len(toDrop) == 0 {
		fmt.Println("No items to drop")
		return nil
	}

	// Revert each item
	for _, id := range toDrop {
		itemPath := filepath.Join("items", id)
		if _, err := runGitCommand(dredgeDir, "checkout", "HEAD", "--", itemPath); err != nil {
			// If checkout fails, item might be untracked (newly added)
			// In that case, just remove it
			if _, rmErr := runGitCommand(dredgeDir, "rm", "-f", itemPath); rmErr != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to drop [%s]: %v\n", id, err)
				continue
			}
		}
		fmt.Printf("Dropped [%s]\n", id)
	}

	// Clean index to remove any staged changes
	if _, err := runGitCommand(dredgeDir, "reset", "HEAD", "--", "items/"); err != nil {
		// Ignore error, just best-effort cleanup
	}

	return nil
}

// Sync pulls then pushes (convenience function)
func Sync(dredgeDir string) error {
	return outboundSync(dredgeDir)
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
	vaultFiles, err := getChangedVaultFiles(dredgeDir)
	if err != nil {
		return fmt.Errorf("failed to detect changed vault files: %w", err)
	}
	if totalChanges == 0 && len(vaultFiles) == 0 {
		fmt.Println("No changes to push")
		return nil
	}

	// Print colored changes
	if totalChanges > 0 {
		printColoredChanges(changes)
	}
	for _, path := range vaultFiles {
		fmt.Printf("Updated %s\n", path)
	}
	if _, ok := getRemoteURL(dredgeDir, "origin"); !ok {
		fmt.Println("\n(no remote configured - local-only mode)")
	}
	return nil
}

func getChangedVaultFiles(dir string) ([]string, error) {
	output, err := runGitCommand(
		dir,
		"diff",
		"--cached",
		"--name-only",
		"--",
		gitIgnoreFileName,
		dredgeKeyFileName,
		dredgeConfigPath,
	)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(output) == "" {
		return nil, nil
	}
	return strings.Split(strings.TrimSpace(output), "\n"), nil
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

// addTrackedFiles adds the vault's synchronized files to git staging.
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

	// Add the synchronized Dredge configuration.
	configFile := filepath.Join(dir, filepath.FromSlash(dredgeConfigPath))
	if _, err := os.Stat(configFile); err == nil {
		if _, err := runGitCommand(dir, "add", "--", dredgeConfigPath); err != nil {
			return fmt.Errorf("failed to add vault configuration: %w", err)
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
	output, err := runGitCommand(dir, "commit", "-m", commitMsg)
	if err != nil {
		return fmt.Errorf("failed to commit: %s", strings.TrimSpace(output))
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

// isRebaseInProgress checks for leftover rebase state from an interrupted pull
func isRebaseInProgress(dir string) bool {
	gitDir := filepath.Join(dir, ".git")
	for _, name := range []string{"rebase-merge", "rebase-apply"} {
		if info, err := os.Stat(filepath.Join(gitDir, name)); err == nil && info.IsDir() {
			return true
		}
	}
	return false
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

// getChangedItemsBetweenCommits returns a map of action -> IDs for items changed between two git refs
func getChangedItemsBetweenCommits(dir, fromRef, toRef string) (map[string][]string, error) {
	// Get changed files with status: A (added), M (modified), D (deleted)
	output, err := runGitCommand(dir, "diff", "--name-status", fromRef+".."+toRef, "--", "items/")
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

// EnsureGitIgnore creates or repairs the required vault ignore rules without
// changing unrelated user rules.
func EnsureGitIgnore(vaultDir string) error {
	path := filepath.Join(vaultDir, gitIgnoreFileName)
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to read .gitignore: %w", err)
	}
	permissions := os.FileMode(defaultFilePermissions)
	if info, statErr := os.Stat(path); statErr == nil {
		permissions = info.Mode().Perm()
	}

	existingRules := make(map[string]bool)
	for _, line := range strings.Split(string(data), "\n") {
		existingRules[strings.TrimSpace(line)] = true
	}
	var missing []string
	firstMissing := len(requiredGitIgnoreRules)
	for index, rule := range requiredGitIgnoreRules {
		if !existingRules[rule] {
			if firstMissing == len(requiredGitIgnoreRules) {
				firstMissing = index
			}
			missing = append(missing, rule)
		}
	}
	// Git ignore exceptions are order-sensitive. If the directory rule is
	// being repaired, repeat later required exceptions after the repaired
	// rules even when those exceptions already appeared in the user's file.
	for index := firstMissing + 1; index < len(requiredGitIgnoreRules); index++ {
		rule := requiredGitIgnoreRules[index]
		if strings.HasPrefix(rule, negationRulePrefix) && existingRules[rule] {
			missing = append(missing, rule)
		}
	}
	if err == nil && len(missing) == 0 {
		return nil
	}

	var updated []byte
	if os.IsNotExist(err) {
		updated = append(updated, requiredGitIgnore...)
	} else {
		updated = append(updated, data...)
		if len(updated) > 0 && updated[len(updated)-1] != '\n' {
			updated = append(updated, '\n')
		}
		if len(updated) > 0 {
			updated = append(updated, '\n')
		}
		updated = append(updated, []byte(strings.Join(missing, "\n"))...)
		updated = append(updated, '\n')
	}
	return atomicWriteFile(path, updated, permissions)
}

func parseGitIgnoreRules(data []byte) []string {
	var rules []string
	for _, line := range strings.Split(string(data), "\n") {
		if rule := strings.TrimSpace(line); rule != "" {
			rules = append(rules, rule)
		}
	}
	return rules
}

func atomicWriteFile(path string, data []byte, permissions os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), defaultDirPermissions); err != nil {
		return fmt.Errorf("failed to create directory for %s: %w", filepath.Base(path), err)
	}
	tempPattern := strings.Replace(atomicTempFilePattern, "*", filepath.Base(path)+"-*", 1)
	temp, err := os.CreateTemp(filepath.Dir(path), tempPattern)
	if err != nil {
		return fmt.Errorf("failed to create %s: %w", filepath.Base(path), err)
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(permissions); err != nil {
		temp.Close()
		return fmt.Errorf("failed to set %s permissions: %w", filepath.Base(path), err)
	}
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return fmt.Errorf("failed to write %s: %w", filepath.Base(path), err)
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return fmt.Errorf("failed to sync %s: %w", filepath.Base(path), err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("failed to close %s: %w", filepath.Base(path), err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("failed to install %s: %w", filepath.Base(path), err)
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
