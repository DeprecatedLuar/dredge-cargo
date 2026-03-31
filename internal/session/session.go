package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const (
	resultsCacheFile = "results"
	deletedCacheFile = "deleted"
)

// vaultPath holds the active vault directory for this process, set once at startup.
var vaultPath string

// SetVaultPath registers the active vault directory. Must be called at startup
// before any session key operations.
func SetVaultPath(path string) {
	vaultPath = path
}

// GetVaultPath returns the active vault directory registered at startup.
func GetVaultPath() string {
	return vaultPath
}

// Dir returns the session-specific directory path.
// The base runtime directory is resolved per-platform by runtimeDir() (see session_*.go).
func Dir() string {
	return filepath.Join(runtimeDir(), "dredge", fmt.Sprintf("%d", os.Getppid()))
}

func ensureDir() error {
	return os.MkdirAll(Dir(), 0700)
}

// CacheResults saves item IDs for numbered access
func CacheResults(ids []string) error {
	if err := ensureDir(); err != nil {
		return err
	}
	data, err := json.Marshal(ids)
	if err != nil {
		return fmt.Errorf("failed to marshal IDs: %w", err)
	}
	return os.WriteFile(filepath.Join(Dir(), resultsCacheFile), data, 0600)
}

// GetCachedResult retrieves a single ID by number (1-indexed)
func GetCachedResult(num int) (string, error) {
	data, err := os.ReadFile(filepath.Join(Dir(), resultsCacheFile))
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("no recent search results")
		}
		return "", fmt.Errorf("failed to read cache: %w", err)
	}

	var ids []string
	if err := json.Unmarshal(data, &ids); err != nil {
		return "", fmt.Errorf("invalid cache format")
	}

	if num < 1 || num > len(ids) {
		return "", fmt.Errorf("result number out of range (1-%d)", len(ids))
	}

	return ids[num-1], nil
}

// CacheDeleted saves deleted item IDs for undo
func CacheDeleted(ids []string) error {
	if err := ensureDir(); err != nil {
		return err
	}
	data, err := json.Marshal(ids)
	if err != nil {
		return fmt.Errorf("failed to marshal IDs: %w", err)
	}
	return os.WriteFile(filepath.Join(Dir(), deletedCacheFile), data, 0600)
}

// GetDeleted retrieves deleted IDs for undo (count <= 0 returns all)
func GetDeleted(count int) ([]string, error) {
	data, err := os.ReadFile(filepath.Join(Dir(), deletedCacheFile))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("no recent deletion found")
		}
		return nil, fmt.Errorf("failed to read cache: %w", err)
	}

	var ids []string
	if err := json.Unmarshal(data, &ids); err != nil {
		return nil, fmt.Errorf("invalid cache format")
	}

	if len(ids) == 0 {
		return nil, fmt.Errorf("no items to restore")
	}

	if count <= 0 || count > len(ids) {
		return ids, nil
	}

	return ids[:count], nil
}
