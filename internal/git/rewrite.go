package git

import (
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/DeprecatedLuar/dredge-cargo/internal/config"
	historymodel "github.com/DeprecatedLuar/dredge-cargo/internal/history"
)

const rewriteBackupNamespace = "refs/dredge/backups"

// RewriteResult describes a verified local retention rewrite. BackupRef remains
// available after success and is intentionally not pruned by this operation.
type RewriteResult struct {
	Changed   bool
	OldHead   string
	NewHead   string
	BackupRef string
}

type rewriteEvent struct {
	order     int
	timestamp time.Time
	changes   map[string]string
	message   string
}

// rewriteFailureHook is used only by package tests to verify transaction
// recovery at major boundaries.
var rewriteFailureHook func(string) error

// RetentionNeeded reports whether applying the configured policy would remove
// at least one distinct encrypted version or an expired deleted item.
func RetentionNeeded(vaultDir string, now time.Time) (bool, error) {
	cfg, err := config.Load(vaultDir)
	if err != nil {
		return false, err
	}
	items, err := ReadHistory(vaultDir)
	if err != nil {
		return false, err
	}
	policy := retentionPolicy(cfg)
	for _, item := range items {
		plan := historymodel.PlanRetention(item, policy, now)
		if plan.Expired && (len(item.ItemVersions) > 0 || len(item.StorageVersions) > 0) {
			return true, nil
		}
		if distinctBlobCount(item.ItemVersions) > len(plan.ItemVersions) ||
			distinctBlobCount(item.StorageVersions) > len(plan.StorageVersions) {
			return true, nil
		}
	}
	return false, nil
}

// RewriteHistory constructs and verifies policy-retained history locally. It
// never fetches, pushes, prunes objects, or touches the working tree or the
// user's index.
func RewriteHistory(vaultDir string, now time.Time) (result RewriteResult, err error) {
	cfg, err := config.Load(vaultDir)
	if err != nil {
		return result, err
	}
	if !isGitRepo(vaultDir) {
		return result, fmt.Errorf("not a git repository")
	}

	branchRef, err := gitOutput(vaultDir, nil, "symbolic-ref", "-q", "HEAD")
	if err != nil || strings.TrimSpace(branchRef) == "" {
		return result, fmt.Errorf("history rewrite requires an attached branch")
	}
	branchRef = strings.TrimSpace(branchRef)
	oldHead, err := gitOutput(vaultDir, nil, "rev-parse", "HEAD")
	if err != nil {
		return result, fmt.Errorf("failed to resolve HEAD: %w", err)
	}
	oldHead = strings.TrimSpace(oldHead)
	result.OldHead = oldHead

	items, err := ReadHistory(vaultDir)
	if err != nil {
		return result, err
	}
	commits, err := historyCommits(vaultDir)
	if err != nil {
		return result, err
	}
	order := make(map[string]int, len(commits))
	for index, commitID := range commits {
		order[commitID] = index
	}
	policy := retentionPolicy(cfg)
	events, err := retentionEvents(items, policy, now, order)
	if err != nil {
		return result, err
	}
	if err := failRewriteAt("planned"); err != nil {
		return result, err
	}

	headTree, err := gitOutput(vaultDir, nil, "rev-parse", "HEAD^{tree}")
	if err != nil {
		return result, fmt.Errorf("failed to resolve current tree: %w", err)
	}
	headTree = strings.TrimSpace(headTree)
	indexFile, err := os.CreateTemp("", "dredge-rewrite-index-*")
	if err != nil {
		return result, fmt.Errorf("failed to create temporary Git index: %w", err)
	}
	indexPath := indexFile.Name()
	if err := indexFile.Close(); err != nil {
		os.Remove(indexPath)
		return result, fmt.Errorf("failed to close temporary Git index: %w", err)
	}
	if err := os.Remove(indexPath); err != nil {
		return result, fmt.Errorf("failed to prepare temporary Git index: %w", err)
	}
	defer os.Remove(indexPath)
	indexEnv := []string{"GIT_INDEX_FILE=" + indexPath}

	if _, err := gitOutput(vaultDir, indexEnv, "read-tree", "HEAD"); err != nil {
		return result, fmt.Errorf("failed to seed replacement index: %w", err)
	}
	if _, err := gitOutput(vaultDir, indexEnv, "rm", "-r", "--cached", "--ignore-unmatch", itemHistoryDir, storageHistoryDir); err != nil {
		return result, fmt.Errorf("failed to clear retained paths from replacement index: %w", err)
	}

	parent := ""
	if len(events) == 0 {
		timestamp, timestampErr := commitTimestamp(vaultDir, oldHead)
		if timestampErr != nil {
			return result, timestampErr
		}
		tree, treeErr := gitOutput(vaultDir, indexEnv, "write-tree")
		if treeErr != nil {
			return result, fmt.Errorf("failed to write replacement baseline tree: %w", treeErr)
		}
		parent, err = createSyntheticCommit(vaultDir, strings.TrimSpace(tree), "", timestamp, "dredge retention baseline")
		if err != nil {
			return result, err
		}
	}
	for _, event := range events {
		paths := make([]string, 0, len(event.changes))
		for filePath := range event.changes {
			paths = append(paths, filePath)
		}
		sort.Strings(paths)
		for _, filePath := range paths {
			blobID := event.changes[filePath]
			if blobID == "" {
				if _, err := gitOutput(vaultDir, indexEnv, "update-index", "--force-remove", "--", filePath); err != nil {
					return result, fmt.Errorf("failed to delete %s from replacement index: %w", filePath, err)
				}
				continue
			}
			if _, err := gitOutput(vaultDir, indexEnv, "update-index", "--add", "--cacheinfo", "100644,"+blobID+","+filePath); err != nil {
				return result, fmt.Errorf("failed to add %s to replacement index: %w", filePath, err)
			}
		}
		tree, err := gitOutput(vaultDir, indexEnv, "write-tree")
		if err != nil {
			return result, fmt.Errorf("failed to write replacement tree: %w", err)
		}
		parent, err = createSyntheticCommit(vaultDir, strings.TrimSpace(tree), parent, event.timestamp, event.message)
		if err != nil {
			return result, err
		}
	}
	result.NewHead = parent
	if err := failRewriteAt("built"); err != nil {
		return result, err
	}

	newTree, err := gitOutput(vaultDir, nil, "rev-parse", parent+"^{tree}")
	if err != nil {
		return result, fmt.Errorf("failed to resolve replacement tree: %w", err)
	}
	if strings.TrimSpace(newTree) != headTree {
		return result, fmt.Errorf("replacement history rejected: final tree differs from current HEAD")
	}
	if err := failRewriteAt("verified"); err != nil {
		return result, err
	}
	if parent == oldHead {
		return result, nil
	}

	backupRef := rewriteBackupNamespace + "/" + oldHead
	if _, err := gitOutput(vaultDir, nil, "update-ref", backupRef, oldHead); err != nil {
		return result, fmt.Errorf("failed to create rewrite backup reference: %w", err)
	}
	result.BackupRef = backupRef
	if err := failRewriteAt("backed-up"); err != nil {
		return result, err
	}
	if _, err := gitOutput(vaultDir, nil, "update-ref", branchRef, parent, oldHead); err != nil {
		return result, fmt.Errorf("failed to move branch to replacement history: %w", err)
	}
	moved := true
	defer func() {
		if err == nil || !moved {
			return
		}
		if _, restoreErr := gitOutput(vaultDir, nil, "update-ref", branchRef, oldHead, parent); restoreErr != nil {
			err = fmt.Errorf("%w; branch recovery failed (backup retained at %s): %v", err, backupRef, restoreErr)
		}
	}()
	if err := failRewriteAt("moved"); err != nil {
		return result, err
	}
	moved = false
	result.Changed = true
	return result, nil
}

func retentionPolicy(cfg config.Config) historymodel.Policy {
	return historymodel.Policy{
		Items: historymodel.Limits{
			MaxVersions: cfg.History.Items.MaxVersions,
			MaxBytes:    cfg.History.Items.MaxBytesPerItem,
		},
		Storage: historymodel.Limits{
			MaxVersions: cfg.History.Storage.MaxVersions,
			MaxBytes:    cfg.History.Storage.MaxBytesPerItem,
		},
		RetainFor: cfg.History.Deleted.RetainFor,
	}
}

func distinctBlobCount(versions []historymodel.Version) int {
	seen := make(map[string]bool, len(versions))
	for _, version := range versions {
		seen[version.BlobID] = true
	}
	return len(seen)
}

func retentionEvents(items []historymodel.Item, policy historymodel.Policy, now time.Time, order map[string]int) ([]rewriteEvent, error) {
	byOrder := make(map[int]*rewriteEvent)
	addChange := func(version historymodel.Version, filePath string) error {
		position, ok := order[version.CommitID]
		if !ok {
			return fmt.Errorf("retention version references unknown commit %s", version.CommitID)
		}
		event := byOrder[position]
		if event == nil {
			event = &rewriteEvent{
				order:     position,
				timestamp: version.Timestamp,
				changes:   make(map[string]string),
			}
			byOrder[position] = event
		}
		event.changes[filePath] = version.BlobID
		return nil
	}
	for _, item := range items {
		plan := historymodel.PlanRetention(item, policy, now)
		for index := len(plan.ItemVersions) - 1; index >= 0; index-- {
			if err := addChange(plan.ItemVersions[index], itemHistoryDir+"/"+item.ID); err != nil {
				return nil, err
			}
		}
		for index := len(plan.StorageVersions) - 1; index >= 0; index-- {
			if err := addChange(plan.StorageVersions[index], storageHistoryDir+"/"+item.ID); err != nil {
				return nil, err
			}
		}
		if !item.Live && !plan.Expired && item.DeletedAt != nil {
			position, ok := order[item.DeletedCommitID]
			if !ok {
				return nil, fmt.Errorf("deleted item %s has incomplete deletion metadata", item.ID)
			}
			event := byOrder[position]
			if event == nil {
				event = &rewriteEvent{
					order:     position,
					timestamp: *item.DeletedAt,
					changes:   make(map[string]string),
				}
				byOrder[position] = event
			}
			event.changes[itemHistoryDir+"/"+item.ID] = ""
			if !item.StorageLive {
				event.changes[storageHistoryDir+"/"+item.ID] = ""
			}
		}
	}
	events := make([]rewriteEvent, 0, len(byOrder))
	for _, event := range byOrder {
		paths := make([]string, 0, len(event.changes))
		for filePath := range event.changes {
			paths = append(paths, filePath)
		}
		sort.Strings(paths)
		event.message = "dredge retention: " + strings.Join(paths, " ")
		events = append(events, *event)
	}
	sort.Slice(events, func(i, j int) bool { return events[i].order < events[j].order })
	return events, nil
}

func createSyntheticCommit(dir, tree, parent string, timestamp time.Time, message string) (string, error) {
	args := []string{"commit-tree", tree}
	if parent != "" {
		args = append(args, "-p", parent)
	}
	date := timestamp.UTC().Format(time.RFC3339)
	env := []string{"GIT_AUTHOR_DATE=" + date, "GIT_COMMITTER_DATE=" + date}
	output, err := gitInput(dir, env, message+"\n", args...)
	if err != nil {
		return "", fmt.Errorf("failed to create replacement commit: %w", err)
	}
	return strings.TrimSpace(output), nil
}

func gitOutput(dir string, extraEnv []string, args ...string) (string, error) {
	return gitInput(dir, extraEnv, "", args...)
}

func gitInput(dir string, extraEnv []string, input string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), extraEnv...)
	if input != "" {
		cmd.Stdin = strings.NewReader(input)
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("%s: %w", strings.TrimSpace(string(output)), err)
	}
	return string(output), nil
}

func failRewriteAt(step string) error {
	if rewriteFailureHook == nil {
		return nil
	}
	if err := rewriteFailureHook(step); err != nil {
		return fmt.Errorf("injected rewrite failure at %s: %w", step, err)
	}
	return nil
}
