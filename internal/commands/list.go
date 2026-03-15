package commands

import (
	"fmt"
	"sort"
	"strings"

	"github.com/DeprecatedLuar/dredge/internal/crypto"
	"github.com/DeprecatedLuar/dredge/internal/session"
	"github.com/DeprecatedLuar/dredge/internal/storage"
	"github.com/DeprecatedLuar/dredge/internal/ui"
)

func HandleList(args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("usage: dredge list")
	}

	// Get master key (checks session cache, prompts if needed)
	key, err := crypto.GetKeyWithVerification()
	if err != nil {
		return fmt.Errorf("key error: %w", err)
	}

	// Load all item IDs
	ids, err := storage.ListItemIDs()
	if err != nil {
		return fmt.Errorf("failed to list items: %w", err)
	}

	if len(ids) == 0 {
		fmt.Println("No items found. Use 'dredge add' to create one.")
		return nil
	}

	// Load and decrypt all items
	type itemEntry struct {
		id   string
		item *storage.Item
	}

	entries := make([]itemEntry, 0, len(ids))
	for _, id := range ids {
		item, err := storage.ReadItem(id, key)
		if err != nil {
			// Skip items that fail to decrypt
			continue
		}
		entries = append(entries, itemEntry{id: id, item: item})
	}

	// Sort by modification time (newest first)
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].item.Modified.After(entries[j].item.Modified)
	})

	// Print all items
	for _, entry := range entries {
		line := ui.FormatItem(entry.id, entry.item.Title, entry.item.Tags, "it#")

		// Use angle brackets for binary items
		if entry.item.Type == storage.TypeBinary {
			// Replace [id] with <id>
			line = strings.Replace(line, "["+entry.id+"]", "<"+entry.id+">", 1)
		}

		fmt.Println(line)
	}

	// Cache IDs for numbered access
	cachedIDs := make([]string, len(entries))
	for i, entry := range entries {
		cachedIDs[i] = entry.id
	}
	session.CacheResults(cachedIDs) // Ignore errors (non-fatal)

	return nil
}
