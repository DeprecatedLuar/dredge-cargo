package commands

import (
	"fmt"
	"slices"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/DeprecatedLuar/dredge-cargo/internal/crypto"
	"github.com/DeprecatedLuar/dredge-cargo/internal/editor"
	"github.com/DeprecatedLuar/dredge-cargo/internal/storage"
)

func HandleEdit(args []string) error {
	// Parse flags manually (flexible positioning)
	var id string
	var metadataMode bool

	for _, arg := range args {
		switch arg {
		case "--metadata", "-m":
			metadataMode = true
		default:
			if id == "" && !isFlag(arg) {
				id = arg
			}
		}
	}

	if id == "" {
		return fmt.Errorf("usage: dredge edit <id> [--metadata|-m]")
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

	updatedItem, changed, err := editor.OpenForExisting(item)
	if err != nil {
		return fmt.Errorf("failed to edit item: %w", err)
	}
	if !changed {
		fmt.Printf("→ [%s] %s (no changes)\n", id, item.Title)
		return nil
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

	// Always show filename and mode (even if empty) so users can add/edit them
	filename := item.Filename
	metadataTOML += fmt.Sprintf("\nfilename = %q", filename)

	mode := ""
	if item.Mode != nil {
		mode = fmt.Sprintf("%o", *item.Mode)
	}
	metadataTOML += fmt.Sprintf("\nmode = %q", mode)

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

	if metadata.Title == item.Title &&
		slices.Equal(metadata.Tags, item.Tags) &&
		metadata.Type == item.Type &&
		metadata.Filename == item.Filename &&
		modeEqual(parsedMode, item.Mode) {
		fmt.Printf("→ [%s] %s (no changes)\n", id, item.Title)
		return nil
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

// modeEqual reports whether two optional file-mode pointers represent the same value.
func modeEqual(a, b *uint32) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
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
