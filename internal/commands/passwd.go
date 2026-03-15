package commands

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
	"github.com/DeprecatedLuar/dredge/internal/crypto"
	"github.com/DeprecatedLuar/dredge/internal/storage"
	"github.com/DeprecatedLuar/dredge/internal/ui"
)

const (
	tmpDirName = "items.tmp"
	oldDirName = "items.old"
	keyTmpName = ".dredge-key.tmp"
	keyOldName = ".dredge-key.old"
)

// HandlePasswd handles password change command
// Flow: verify current password → prompt new password → re-encrypt all items → atomic swap
func HandlePasswd() error {
	fmt.Fprintln(os.Stderr, "Changing password for Dredge.")

	// 1. Always prompt for current password (bypass cache for security)
	currentPassword, err := ui.PromptPasswordCustom("Current password: ")
	if err != nil {
		return fmt.Errorf("failed to prompt for current password: %w", err)
	}

	// 2. Derive current master key (also verifies the password)
	currentKey, err := crypto.DeriveKeyFromVault(currentPassword)
	if err != nil {
		return fmt.Errorf("current password verification failed: %w", err)
	}

	// 3. Prompt for new password (with confirmation)
	newPassword, err := ui.PromptPasswordWithConfirmationCustom("New password: ", "Retype new password: ")
	if err != nil {
		return fmt.Errorf("failed to get new password: %w", err)
	}

	if newPassword == currentPassword {
		return fmt.Errorf("new password must be different from current password")
	}

	// 4. Get all item IDs
	itemIDs, err := storage.ListItemIDs()
	if err != nil {
		return fmt.Errorf("failed to list items: %w", err)
	}

	// Generate new key file bytes and derive new master key
	newKeyFileBytes, newKey, err := crypto.NewVerificationFileBytes(newPassword)
	if err != nil {
		return fmt.Errorf("failed to generate new verification: %w", err)
	}

	if len(itemIDs) == 0 {
		// No items to re-encrypt, just update the key file
		if err := updatePasswordVerification(newKeyFileBytes, newKey); err != nil {
			return fmt.Errorf("failed to update password verification: %w", err)
		}
		warnIfUnpushed()
		return nil
	}

	// 5. Load all items into memory (decrypt with current key)
	items := make(map[string]*storage.Item)
	for _, id := range itemIDs {
		item, err := storage.ReadItem(id, currentKey)
		if err != nil {
			return fmt.Errorf("failed to decrypt item %s: %w", id, err)
		}
		items[id] = item
	}

	// 6. Get paths
	dredgeDir, err := storage.GetDredgeDir()
	if err != nil {
		return fmt.Errorf("failed to get dredge directory: %w", err)
	}

	itemsDir, err := storage.GetItemsDir()
	if err != nil {
		return fmt.Errorf("failed to get items directory: %w", err)
	}

	tmpDir := filepath.Join(dredgeDir, tmpDirName)
	oldDir := filepath.Join(dredgeDir, oldDirName)

	// 7. Clean up any leftover tmp/old directories from failed previous runs
	_ = os.RemoveAll(tmpDir)
	_ = os.RemoveAll(oldDir)

	// 8. Create items.tmp/ directory
	if err := os.MkdirAll(tmpDir, 0700); err != nil {
		return fmt.Errorf("failed to create temporary directory: %w", err)
	}

	// 9. Re-encrypt all items and write to items.tmp/
	for id, item := range items {
		// Encode to TOML
		var buf bytes.Buffer
		encoder := toml.NewEncoder(&buf)
		if err := encoder.Encode(item); err != nil {
			_ = os.RemoveAll(tmpDir)
			return fmt.Errorf("failed to encode item %s: %w", id, err)
		}

		// Encrypt with new key
		encryptedData, err := crypto.Encrypt(buf.Bytes(), newKey)
		if err != nil {
			_ = os.RemoveAll(tmpDir)
			return fmt.Errorf("failed to encrypt item %s: %w", id, err)
		}

		// Write to tmp directory
		tmpItemPath := filepath.Join(tmpDir, id)
		if err := os.WriteFile(tmpItemPath, encryptedData, 0600); err != nil {
			_ = os.RemoveAll(tmpDir)
			return fmt.Errorf("failed to write item %s: %w", id, err)
		}
	}

	// 10. Verify count (paranoia check)
	tmpEntries, err := os.ReadDir(tmpDir)
	if err != nil {
		_ = os.RemoveAll(tmpDir)
		return fmt.Errorf("failed to verify tmp directory: %w", err)
	}

	if len(tmpEntries) != len(itemIDs) {
		_ = os.RemoveAll(tmpDir)
		return fmt.Errorf("re-encryption failed: expected %d items, got %d", len(itemIDs), len(tmpEntries))
	}

	// 11. Write new .dredge-key.tmp
	keyPath, err := crypto.GetVerifyFilePath()
	if err != nil {
		_ = os.RemoveAll(tmpDir)
		return fmt.Errorf("failed to get key file path: %w", err)
	}

	keyTmpPath := filepath.Join(dredgeDir, keyTmpName)
	keyOldPath := filepath.Join(dredgeDir, keyOldName)

	if err := os.WriteFile(keyTmpPath, newKeyFileBytes, 0600); err != nil {
		_ = os.RemoveAll(tmpDir)
		return fmt.Errorf("failed to write new verification key: %w", err)
	}

	// 12. ATOMIC SWAP (the critical moment)
	// Rename original items/ to items.old
	if err := os.Rename(itemsDir, oldDir); err != nil {
		_ = os.RemoveAll(tmpDir)
		_ = os.Remove(keyTmpPath)
		return fmt.Errorf("failed to backup items directory: %w", err)
	}

	// Rename items.tmp/ to items/
	if err := os.Rename(tmpDir, itemsDir); err != nil {
		// Critical failure - try to restore
		_ = os.Rename(oldDir, itemsDir)
		_ = os.Remove(keyTmpPath)
		return fmt.Errorf("failed to activate new items directory (restored backup): %w", err)
	}

	// Rename .dredge-key to .dredge-key.old
	if err := os.Rename(keyPath, keyOldPath); err != nil {
		// Items already swapped, but key backup failed - continue anyway
		fmt.Fprintf(os.Stderr, "Warning: failed to backup old key file: %v\n", err)
	}

	// Rename .dredge-key.tmp to .dredge-key
	if err := os.Rename(keyTmpPath, keyPath); err != nil {
		// Critical failure - try to restore key
		_ = os.Rename(keyOldPath, keyPath)
		return fmt.Errorf("failed to activate new key file (restored backup): %w", err)
	}

	// 13. Success! Delete backups
	_ = os.RemoveAll(oldDir)
	_ = os.Remove(keyOldPath)

	// 14. Update session cache with new key
	if err := crypto.CacheKey(newKey); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to update session cache: %v\n", err)
	}

	warnIfUnpushed()
	return nil
}

// updatePasswordVerification updates the .dredge-key file when there are no items to re-encrypt
func updatePasswordVerification(newKeyFileBytes []byte, newKey []byte) error {
	keyPath, err := crypto.GetVerifyFilePath()
	if err != nil {
		return fmt.Errorf("failed to get key file path: %w", err)
	}

	dredgeDir := filepath.Dir(keyPath)
	keyTmpPath := filepath.Join(dredgeDir, keyTmpName)
	keyOldPath := filepath.Join(dredgeDir, keyOldName)

	if err := os.WriteFile(keyTmpPath, newKeyFileBytes, 0600); err != nil {
		return fmt.Errorf("failed to write new verification key: %w", err)
	}

	// Atomic swap
	if err := os.Rename(keyPath, keyOldPath); err != nil {
		_ = os.Remove(keyTmpPath)
		return fmt.Errorf("failed to backup old key file: %w", err)
	}

	if err := os.Rename(keyTmpPath, keyPath); err != nil {
		_ = os.Rename(keyOldPath, keyPath) // Restore
		return fmt.Errorf("failed to activate new key file: %w", err)
	}

	// Success - delete backup
	_ = os.Remove(keyOldPath)

	// Update session cache
	if err := crypto.CacheKey(newKey); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to update session cache: %v\n", err)
	}

	return nil
}
