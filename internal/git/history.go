package git

import (
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	historymodel "github.com/DeprecatedLuar/dredge-cargo/internal/history"
)

const (
	itemHistoryDir    = "items"
	storageHistoryDir = "storage"
	historyFileMode   = 0600
	historyDirMode    = 0700
)

type treeEntry struct {
	blobID string
}

// RestoreDeletedItem restores the newest encrypted blobs retained in Git for a
// deleted ID. It treats the blobs as opaque bytes and leaves them uncommitted.
func RestoreDeletedItem(vaultDir, id string) error {
	if _, _, valid := exactHistoryPath(itemHistoryDir + "/" + id); !valid {
		return fmt.Errorf("invalid item ID %q", id)
	}

	items, err := ReadHistory(vaultDir)
	if err != nil {
		return err
	}
	var found *historymodel.Item
	for index := range items {
		if items[index].ID == id {
			found = &items[index]
			break
		}
	}
	if found == nil || len(found.ItemVersions) == 0 {
		return fmt.Errorf("no restorable Git history found for %q", id)
	}
	if found.Live {
		return fmt.Errorf("item [%s] is live in HEAD and cannot be restored", id)
	}

	itemPath := filepath.Join(vaultDir, itemHistoryDir, id)
	storagePath := filepath.Join(vaultDir, storageHistoryDir, id)
	if exists, err := pathExists(itemPath); err != nil {
		return err
	} else if exists {
		return fmt.Errorf("item [%s] already exists in the working tree", id)
	}
	if exists, err := pathExists(storagePath); err != nil {
		return err
	} else if exists {
		return fmt.Errorf("storage blob [%s] already exists in the working tree", id)
	}

	itemBlob, err := readGitBlob(vaultDir, found.ItemVersions[0].BlobID)
	if err != nil {
		return fmt.Errorf("failed to extract item [%s]: %w", id, err)
	}
	var storageBlob []byte
	hasStorage := len(found.StorageVersions) > 0
	if hasStorage {
		storageBlob, err = readGitBlob(vaultDir, found.StorageVersions[0].BlobID)
		if err != nil {
			return fmt.Errorf("incomplete recovery for [%s]: storage blob is unavailable: %w", id, err)
		}
	}

	itemTemp, err := prepareHistoryTemp(itemPath, itemBlob)
	if err != nil {
		return err
	}
	defer os.Remove(itemTemp)

	var storageTemp string
	if hasStorage {
		storageTemp, err = prepareHistoryTemp(storagePath, storageBlob)
		if err != nil {
			return err
		}
		defer os.Remove(storageTemp)
		if err := os.Rename(storageTemp, storagePath); err != nil {
			return fmt.Errorf("failed to install restored storage blob: %w", err)
		}
	}
	if err := os.Rename(itemTemp, itemPath); err != nil {
		if hasStorage {
			_ = os.Remove(storagePath)
		}
		return fmt.Errorf("failed to install restored item: %w", err)
	}
	return nil
}

func pathExists(filePath string) (bool, error) {
	_, err := os.Lstat(filePath)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, fmt.Errorf("failed to inspect %s: %w", filePath, err)
}

func readGitBlob(dir, blobID string) ([]byte, error) {
	cmd := exec.Command("git", "cat-file", "blob", blobID)
	cmd.Dir = dir
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to read Git object %s: %w", blobID, err)
	}
	return output, nil
}

func prepareHistoryTemp(destination string, data []byte) (string, error) {
	if err := os.MkdirAll(filepath.Dir(destination), historyDirMode); err != nil {
		return "", fmt.Errorf("failed to create restoration directory: %w", err)
	}
	temp, err := os.CreateTemp(filepath.Dir(destination), "."+filepath.Base(destination)+"-restore-*")
	if err != nil {
		return "", fmt.Errorf("failed to create restoration temporary file: %w", err)
	}
	tempPath := temp.Name()
	cleanup := func() {
		temp.Close()
		os.Remove(tempPath)
	}
	if err := temp.Chmod(historyFileMode); err != nil {
		cleanup()
		return "", fmt.Errorf("failed to set restoration permissions: %w", err)
	}
	if _, err := temp.Write(data); err != nil {
		cleanup()
		return "", fmt.Errorf("failed to write restoration temporary file: %w", err)
	}
	if err := temp.Sync(); err != nil {
		cleanup()
		return "", fmt.Errorf("failed to sync restoration temporary file: %w", err)
	}
	if err := temp.Close(); err != nil {
		os.Remove(tempPath)
		return "", fmt.Errorf("failed to close restoration temporary file: %w", err)
	}
	return tempPath, nil
}

// ReadHistory derives Dredge item history from exact paths and Git objects.
// Commits are inspected along HEAD's first-parent chain, oldest-first.
func ReadHistory(vaultDir string) ([]historymodel.Item, error) {
	commits, err := historyCommits(vaultDir)
	if err != nil {
		return nil, err
	}

	items := make(map[string]*historymodel.Item)
	previousItems := make(map[string]treeEntry)
	previousStorage := make(map[string]treeEntry)
	for _, commitID := range commits {
		timestamp, err := commitTimestamp(vaultDir, commitID)
		if err != nil {
			return nil, err
		}
		currentItems, currentStorage, err := historyTree(vaultDir, commitID)
		if err != nil {
			return nil, err
		}

		ids := unionHistoryIDs(previousItems, previousStorage, currentItems, currentStorage)
		for id := range ids {
			item := ensureHistoryItem(items, id)
			if current, present := currentItems[id]; present {
				if previous, existed := previousItems[id]; !existed || previous.blobID != current.blobID {
					version, err := objectVersion(vaultDir, commitID, current.blobID, timestamp)
					if err != nil {
						return nil, err
					}
					item.ItemVersions = append(item.ItemVersions, version)
				}
				item.DeletedAt = nil
			} else if _, existed := previousItems[id]; existed {
				deletedAt := timestamp
				item.DeletedAt = &deletedAt
				item.DeletedCommitID = commitID
			}
			if current, present := currentStorage[id]; present {
				if previous, existed := previousStorage[id]; !existed || previous.blobID != current.blobID {
					version, err := objectVersion(vaultDir, commitID, current.blobID, timestamp)
					if err != nil {
						return nil, err
					}
					item.StorageVersions = append(item.StorageVersions, version)
				}
			}
		}
		previousItems = currentItems
		previousStorage = currentStorage
	}

	result := make([]historymodel.Item, 0, len(items))
	for _, item := range items {
		_, item.Live = previousItems[item.ID]
		_, item.StorageLive = previousStorage[item.ID]
		if item.Live {
			item.DeletedCommitID = ""
		}
		item.ItemVersions = distinctVersionsNewest(item.ItemVersions)
		item.StorageVersions = distinctVersionsNewest(item.StorageVersions)
		result = append(result, *item)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}

func historyCommits(dir string) ([]string, error) {
	output, err := runGitCommand(dir, "rev-list", "--first-parent", "--reverse", "HEAD")
	if err != nil {
		return nil, fmt.Errorf("failed to read Git history: %s", strings.TrimSpace(output))
	}
	if strings.TrimSpace(output) == "" {
		return nil, fmt.Errorf("Git history is empty")
	}
	return strings.Fields(output), nil
}

func commitTimestamp(dir, commitID string) (time.Time, error) {
	output, err := runGitCommand(dir, "show", "-s", "--format=%ct", commitID)
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to read commit timestamp %s: %w", commitID, err)
	}
	seconds, err := strconv.ParseInt(strings.TrimSpace(output), 10, 64)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid timestamp for commit %s: %w", commitID, err)
	}
	return time.Unix(seconds, 0).UTC(), nil
}

func historyTree(dir, commitID string) (map[string]treeEntry, map[string]treeEntry, error) {
	output, err := runGitCommand(dir, "ls-tree", "-rz", commitID, "--", itemHistoryDir, storageHistoryDir)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to inspect commit %s: %w", commitID, err)
	}
	items := make(map[string]treeEntry)
	storage := make(map[string]treeEntry)
	for _, record := range strings.Split(output, "\x00") {
		if record == "" {
			continue
		}
		header, filePath, found := strings.Cut(record, "\t")
		fields := strings.Fields(header)
		if !found || len(fields) != 3 || fields[1] != "blob" {
			continue
		}
		dirName, id, valid := exactHistoryPath(filePath)
		if !valid {
			continue
		}
		entry := treeEntry{blobID: fields[2]}
		if dirName == itemHistoryDir {
			items[id] = entry
		} else {
			storage[id] = entry
		}
	}
	return items, storage, nil
}

func exactHistoryPath(filePath string) (string, string, bool) {
	if path.Clean(filePath) != filePath {
		return "", "", false
	}
	parts := strings.Split(filePath, "/")
	if len(parts) != 2 || parts[1] == "" {
		return "", "", false
	}
	if parts[0] != itemHistoryDir && parts[0] != storageHistoryDir {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func objectVersion(dir, commitID, blobID string, timestamp time.Time) (historymodel.Version, error) {
	output, err := runGitCommand(dir, "cat-file", "-s", blobID)
	if err != nil {
		return historymodel.Version{}, fmt.Errorf("failed to size Git object %s: %w", blobID, err)
	}
	size, err := strconv.ParseInt(strings.TrimSpace(output), 10, 64)
	if err != nil {
		return historymodel.Version{}, fmt.Errorf("invalid size for Git object %s: %w", blobID, err)
	}
	return historymodel.Version{
		CommitID:  commitID,
		BlobID:    blobID,
		Size:      size,
		Timestamp: timestamp,
	}, nil
}

func ensureHistoryItem(items map[string]*historymodel.Item, id string) *historymodel.Item {
	if item, ok := items[id]; ok {
		return item
	}
	item := &historymodel.Item{ID: id}
	items[id] = item
	return item
}

func unionHistoryIDs(maps ...map[string]treeEntry) map[string]bool {
	ids := make(map[string]bool)
	for _, entries := range maps {
		for id := range entries {
			ids[id] = true
		}
	}
	return ids
}

func distinctVersionsNewest(versions []historymodel.Version) []historymodel.Version {
	for left, right := 0, len(versions)-1; left < right; left, right = left+1, right-1 {
		versions[left], versions[right] = versions[right], versions[left]
	}
	seen := make(map[string]bool, len(versions))
	distinct := make([]historymodel.Version, 0, len(versions))
	for _, version := range versions {
		if seen[version.BlobID] {
			continue
		}
		seen[version.BlobID] = true
		distinct = append(distinct, version)
	}
	return distinct
}
