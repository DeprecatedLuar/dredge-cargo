package commands

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/DeprecatedLuar/dredge/internal/crypto"
	"github.com/DeprecatedLuar/dredge/internal/search"
	"github.com/DeprecatedLuar/dredge/internal/session"
	"github.com/DeprecatedLuar/dredge/internal/storage"
	"github.com/DeprecatedLuar/dredge/internal/ui"
)

const (
	smartThreshold = 2.5 // Top score must be 2.5x higher than second to auto-view
)

func HandleSearch(query string, luck bool, forceSearch bool) error {
	// Get password (with verification and caching)
	password, err := crypto.GetPasswordWithVerification()
	if err != nil {
		return fmt.Errorf("password error: %w", err)
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
	items := make(map[string]*storage.Item)
	for _, id := range ids {
		item, err := storage.ReadItem(id, password)
		if err != nil {
			// Skip items that fail to decrypt (corrupted/wrong format)
			continue
		}
		items[id] = item
	}

	// Perform search
	results := search.Search(items, query)

	// Display results
	if len(results) == 0 {
		fmt.Printf("No results found for: %s\n", query)
		return nil
	}

	// Determine viewing mode:
	// 1. -l flag: always view top result
	// 2. -s flag: always show list
	// 3. Smart default: auto-view if clear winner, else list
	// 4. Never auto-view binary items (force list instead)
	shouldAutoView := false

	if luck {
		// Force view top result
		shouldAutoView = true
	} else if !forceSearch {
		// Never auto-view binary items (they don't have readable content)
		if results[0].Item.Type == storage.TypeBinary {
			shouldAutoView = false
		} else if len(results) == 1 {
			// Only one result, definitely view it
			shouldAutoView = true
		} else if len(results) > 1 {
			// Check if top result is significantly better than second
			topScore := float64(results[0].Score)
			secondScore := float64(results[1].Score)
			if secondScore > 0 && topScore/secondScore >= smartThreshold {
				shouldAutoView = true
			}
		}
	}

	// Auto-view top result if conditions met
	if shouldAutoView {
		return HandleView([]string{results[0].ID})
	}

	// Show list
	for _, result := range results {
		line := ui.FormatItem(result.ID, result.Item.Title, result.Item.Tags, "it#")

		// Use angle brackets for binary items
		if result.Item.Type == storage.TypeBinary {
			// Replace [id] with <id>
			line = strings.Replace(line, "["+result.ID+"]", "<"+result.ID+">", 1)
		}

		fmt.Println(line)
	}

	// Cache results for numbered access
	resultIDs := make([]string, len(results))
	for i, r := range results {
		resultIDs[i] = r.ID
	}
	session.CacheResults(resultIDs) // Ignore errors (non-fatal)

	return nil
}

// ResolveArgs converts numbered args to IDs using cached search results
// Non-numeric args are passed through as-is (assumed to be IDs)
func ResolveArgs(args []string) ([]string, error) {
	resolved := make([]string, len(args))

	for i, arg := range args {
		// Try parsing as number (strconv.Atoi requires entire string to be numeric)
		// Limit to 1-2 digits to avoid IDs like "123xyz" or long numbers
		if num, err := strconv.Atoi(arg); err == nil && num > 0 && len(arg) <= 2 {
			// It's a number, resolve from cache
			id, cacheErr := session.GetCachedResult(num)
			if cacheErr != nil {
				return nil, fmt.Errorf("arg %q: %w", arg, cacheErr)
			}
			resolved[i] = id
		} else {
			// Not a number or too long, assume it's an ID
			resolved[i] = arg
		}
	}

	return resolved, nil
}
