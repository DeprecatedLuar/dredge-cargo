package commands

import (
	"fmt"
	"strings"

	"github.com/DeprecatedLuar/dredge/internal/crypto"
	"github.com/DeprecatedLuar/dredge/internal/storage"
	"github.com/DeprecatedLuar/dredge/internal/ui"
)

func HandleView(args []string, raw ...bool) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: dredge view <id>")
	}
	rawMode := len(raw) > 0 && raw[0]

	// Resolve numbered arg to ID
	ids, err := ResolveArgs(args[:1])
	if err != nil {
		return err
	}
	id := ids[0]

	// Get master key (checks session cache, prompts if needed)
	key, err := crypto.GetKeyWithVerification()
	if err != nil {
		return fmt.Errorf("failed to get key: %w", err)
	}

	// Read and decrypt item
	item, err := storage.ReadItem(id, key)
	if err != nil {
		return fmt.Errorf("failed to read item: %w", err)
	}

	if rawMode {
		if item.Type == storage.TypeBinary {
			return fmt.Errorf("item %s is binary — use 'dredge export' to extract it", id)
		}
		fmt.Print(item.Content.Text)
		return nil
	}

	// Print [ID] Title #tags (use <ID> for binary items)
	line := ui.FormatItem(id, item.Title, item.Tags, "it#")
	if item.Type == storage.TypeBinary {
		// Replace [id] with <id> for binary items
		line = strings.Replace(line, "["+id+"]", "<"+id+">", 1)
	}
	fmt.Println(line)
	fmt.Println()

	// For binary items, show metadata instead of base64
	if item.Type == storage.TypeBinary {
		fmt.Printf("Type: binary\n")
		if item.Filename != "" {
			fmt.Printf("Filename: %s\n", item.Filename)
		}
		if item.Size != nil {
			fmt.Printf("Size: %d bytes (%.2f KB)\n", *item.Size, float64(*item.Size)/1024.0)
		}
		fmt.Printf("\nUse 'dredge export %s [path]' to extract this file.\n", id)
	} else {
		// For text items, show content
		if item.Content.Text != "" {
			fmt.Println(item.Content.Text)
		}
	}

	return nil
}
