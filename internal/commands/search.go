package commands

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/DeprecatedLuar/dredge-cargo/internal/crypto"
	"github.com/DeprecatedLuar/dredge-cargo/internal/search"
	"github.com/DeprecatedLuar/dredge-cargo/internal/session"
	"github.com/DeprecatedLuar/dredge-cargo/internal/storage"
	"github.com/DeprecatedLuar/dredge-cargo/internal/ui"
)

// SearchTopResult searches for query and returns the top matching item ID.
func SearchTopResult(query string) (string, error) {
	key, err := crypto.GetKeyWithVerification()
	if err != nil {
		return "", fmt.Errorf("key error: %w", err)
	}
	ids, err := storage.ListItemIDs()
	if err != nil {
		return "", fmt.Errorf("failed to list items: %w", err)
	}
	items := make(map[string]*storage.Item)
	for _, id := range ids {
		item, err := storage.ReadItem(id, key)
		if err != nil {
			continue
		}
		items[id] = item
	}
	results := search.Search(items, query)
	if len(results) == 0 {
		return "", fmt.Errorf("no results found for: %s", query)
	}
	return results[0].ID, nil
}

// ResolveSingle resolves a single arg to an item ID.
// Numbers resolve via cache; with luck=true a non-ID arg is treated as a search query.
func ResolveSingle(arg string, luck bool) (string, error) {
	if num, err := strconv.Atoi(arg); err == nil && num > 0 && len(arg) <= 2 {
		return session.GetCachedResult(num)
	}
	if luck && !idPattern.MatchString(arg) {
		return SearchTopResult(arg)
	}
	return arg, nil
}

func HandleSearch(query string, luck bool) error {
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
	items := make(map[string]*storage.Item)
	for _, id := range ids {
		item, err := storage.ReadItem(id, key)
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

	if luck {
		return HandleView([]string{results[0].ID}, false)
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
