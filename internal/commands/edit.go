package commands

import (
	"fmt"
	"os"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/DeprecatedLuar/dredge/internal/crypto"
	"github.com/DeprecatedLuar/dredge/internal/editor"
	"github.com/DeprecatedLuar/dredge/internal/storage"
)

func HandleEdit(args []string) error {
	// Parse flags manually (flexible positioning)
	var id string
	var tomlMode, metadataMode bool

	for _, arg := range args {
		switch arg {
		case "--toml":
			tomlMode = true
		case "--metadata", "-m":
			metadataMode = true
		default:
			if id == "" && !isFlag(arg) {
				id = arg
			}
		}
	}

	if id == "" {
		return fmt.Errorf("usage: dredge edit <id> [--toml|--metadata|-m]")
	}

	// Can't use both flags
	if tomlMode && metadataMode {
		return fmt.Errorf("cannot use --toml and --metadata together")
	}

	// Resolve numbered arg to ID
	ids, err := ResolveArgs([]string{id})
	if err != nil {
		return err
	}
	id = ids[0]

	// Get master key
	key, err := crypto.GetKeyWithVerification()
	if err != nil {
		return fmt.Errorf("key error: %w", err)
	}

	if tomlMode {
		if err := editRawTOML(id, key); err != nil {
			return err
		}
		warnIfUnpushed()
		return nil
	}

	if metadataMode {
		if err := editMetadata(id, key); err != nil {
			return err
		}
		warnIfUnpushed()
		return nil
	}

	// Template-based editing (default)
	item, err := storage.ReadItem(id, key)
	if err != nil {
		return fmt.Errorf("failed to read item [%s]: %w", id, err)
	}

	updatedItem, err := editor.OpenForExisting(item)
	if err != nil {
		return fmt.Errorf("failed to edit item: %w", err)
	}

	if err := storage.UpdateItem(id, updatedItem, key); err != nil {
		return fmt.Errorf("failed to update item: %w", err)
	}

	fmt.Printf("✓ [%s] %s\n", id, updatedItem.Title)
	warnIfUnpushed()
	return nil
}

// isFlag checks if a string is a flag
func isFlag(s string) bool {
	return len(s) > 0 && s[0] == '-'
}

// editRawTOML: edit full TOML including content
func editRawTOML(id string, key []byte) error {
	// Get item file path
	itemPath, err := storage.GetItemPath(id)
	if err != nil {
		return fmt.Errorf("failed to get item path: %w", err)
	}

	// Read encrypted file
	encryptedData, err := os.ReadFile(itemPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("item '%s' not found", id)
		}
		return fmt.Errorf("failed to read item: %w", err)
	}

	// Decrypt to get raw TOML
	tomlData, err := crypto.Decrypt(encryptedData, key)
	if err != nil {
		return fmt.Errorf("failed to decrypt: %w", err)
	}

	// Open editor with raw TOML
	editedTOML, err := editor.OpenRawContent(string(tomlData))
	if err != nil {
		return fmt.Errorf("editor error: %w", err)
	}

	// Validate TOML syntax
	var item storage.Item
	if err := toml.Unmarshal([]byte(editedTOML), &item); err != nil {
		return fmt.Errorf("invalid TOML: %w", err)
	}

	// Encrypt edited TOML
	encryptedEdited, err := crypto.Encrypt([]byte(editedTOML), key)
	if err != nil {
		return fmt.Errorf("failed to encrypt: %w", err)
	}

	// Write back to file
	if err := os.WriteFile(itemPath, encryptedEdited, 0600); err != nil {
		return fmt.Errorf("failed to write item: %w", err)
	}

	fmt.Printf("✓ [%s] %s (raw TOML)\n", id, item.Title)
	return nil
}

// editMetadata: edit everything except [content] section
func editMetadata(id string, key []byte) error {
	// Read full item first
	item, err := storage.ReadItem(id, key)
	if err != nil {
		return fmt.Errorf("failed to read item [%s]: %w", id, err)
	}

	// Create metadata TOML (editable fields only - timestamps are auto-managed)
	metadataTOML := fmt.Sprintf(`title = %q
tags = %v
type = %q`,
		item.Title,
		formatTags(item.Tags),
		item.Type)

	// Add filename and mode if present (from --file imports)
	// Note: size is computed, not editable
	if item.Filename != "" {
		metadataTOML += fmt.Sprintf("\nfilename = %q", item.Filename)
	}
	if item.Mode != nil {
		metadataTOML += fmt.Sprintf("\nmode = \"%o\"", *item.Mode)
	}

	// Open editor with metadata
	editedMetadata, err := editor.OpenRawContent(metadataTOML)
	if err != nil {
		return fmt.Errorf("editor error: %w", err)
	}

	// Parse edited metadata
	var metadata struct {
		Title    string           `toml:"title"`
		Tags     []string         `toml:"tags"`
		Type     storage.ItemType `toml:"type"`
		Filename string           `toml:"filename"`
		Mode     string           `toml:"mode"`
	}
	if err := toml.Unmarshal([]byte(editedMetadata), &metadata); err != nil {
		return fmt.Errorf("invalid metadata TOML: %w", err)
	}

	// Parse mode as octal string (e.g., "600" -> 0o600)
	var parsedMode *uint32
	if metadata.Mode != "" {
		var m uint64
		if _, err := fmt.Sscanf(metadata.Mode, "%o", &m); err != nil {
			return fmt.Errorf("invalid mode %q (use octal like \"600\" or \"755\")", metadata.Mode)
		}
		mode32 := uint32(m)
		parsedMode = &mode32
	}

	// Validate required fields
	if metadata.Title == "" {
		return fmt.Errorf("title cannot be empty")
	}
	if metadata.Type != storage.TypeText && metadata.Type != storage.TypeBinary {
		return fmt.Errorf("type must be 'text' or 'binary'")
	}

	// Update item with new metadata (timestamps auto-managed)
	// Note: size preserved from original (computed, not editable)
	item.Title = metadata.Title
	item.Tags = metadata.Tags
	item.Type = metadata.Type
	item.Modified = time.Now()
	item.Filename = metadata.Filename
	item.Mode = parsedMode

	// Save updated item
	if err := storage.UpdateItem(id, item, key); err != nil {
		return fmt.Errorf("failed to update item: %w", err)
	}

	fmt.Printf("✓ [%s] %s (metadata)\n", id, item.Title)
	return nil
}

// formatTags formats tags array for TOML
func formatTags(tags []string) string {
	if len(tags) == 0 {
		return "[]"
	}
	result := "["
	for i, tag := range tags {
		if i > 0 {
			result += ", "
		}
		result += fmt.Sprintf("%q", tag)
	}
	result += "]"
	return result
}
