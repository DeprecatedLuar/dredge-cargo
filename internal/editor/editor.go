package editor

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/DeprecatedLuar/dredge-cargo/internal/session"
	"github.com/DeprecatedLuar/dredge-cargo/internal/storage"
)

const (
	defaultEditor         = "vim"
	tempFilePrefix        = "dredge-"
	defaultTempFileSuffix = ".md"
)

// OpenForNewItem opens editor with initial title/tags, returns new Item
// If title is empty, opens with blank template for user to fill in
func OpenForNewItem(title string, tags []string) (*storage.Item, error) {
	// Create template (may be empty for "dredge add" with no args)
	templateContent := createTemplate(title, tags, "")

	// Open editor and get edited content
	editedContent, err := openEditor(templateContent, defaultTempFileSuffix)
	if err != nil {
		return nil, err
	}

	// Parse template back to values
	parsedTitle, parsedContent, parsedTags, err := parseTemplate(editedContent)
	if err != nil {
		return nil, err
	}

	// Validate that user provided a title
	if parsedTitle == "" {
		return nil, fmt.Errorf("title cannot be empty")
	}

	// Create new item with current timestamp
	item := storage.NewTextItem(parsedTitle, parsedContent, parsedTags)
	return item, nil
}

// OpenForExisting opens editor with existing item, returns updated Item
func OpenForExisting(item *storage.Item) (*storage.Item, error) {
	// Create template from existing item
	templateContent := createTemplate(item.Title, item.Tags, item.Content.Text)

	suffix := defaultTempFileSuffix
	if item.Filename != "" {
		suffix = filepath.Ext(item.Filename)
	}

	// Open editor and get edited content
	editedContent, err := openEditor(templateContent, suffix)
	if err != nil {
		return nil, err
	}

	// Parse template back to values
	parsedTitle, parsedContent, parsedTags, err := parseTemplate(editedContent)
	if err != nil {
		return nil, err
	}

	if parsedTitle == "" {
		return nil, fmt.Errorf("title cannot be empty")
	}

	// Create updated item, preserving metadata
	updated := &storage.Item{
		Title:    parsedTitle,
		Tags:     parsedTags,
		Type:     item.Type,
		Created:  item.Created,
		Modified: time.Now(),
		Filename: item.Filename,
		Content: storage.ItemContent{
			Text: parsedContent,
		},
	}

	return updated, nil
}

// createTemplate creates the simple template format:
// Line 1: title #tag1 #tag2
// Line 2: blank
// Lines 3+: content
func createTemplate(title string, tags []string, content string) string {
	var sb strings.Builder

	// Line 1: Title and tags
	sb.WriteString(title)
	if len(tags) > 0 {
		for _, tag := range tags {
			sb.WriteString(" #")
			sb.WriteString(tag)
		}
	}
	sb.WriteString("\n")

	// Line 2: Blank separator
	sb.WriteString("\n")

	// Lines 3+: Content
	sb.WriteString(content)

	return sb.String()
}

// parseTemplate parses the template format back into components
// Tags must be trailing: "title #tag1 #tag2" (not "title #tag word")
// Handles minimal input: just a title line is valid (content optional)
func parseTemplate(content string) (title, contentText string, tags []string, err error) {
	// Check for empty content
	if strings.TrimSpace(content) == "" {
		return "", "", nil, fmt.Errorf("empty template")
	}

	lines := strings.Split(content, "\n")

	// Parse line 1: title #tag1 #tag2
	firstLine := lines[0]
	title, tags = parseTitleAndTags(firstLine)

	// If there are at least 3 lines, lines 3+ are content
	// Line 2 is expected to be blank separator (but we're lenient)
	if len(lines) >= 3 {
		contentText = strings.Join(lines[2:], "\n")
	}

	return title, contentText, tags, nil
}

// parseTitleAndTags extracts title and tags from first line
// Rule: After each #word, next 2 chars must be " #" or end/whitespace
func parseTitleAndTags(line string) (title string, tags []string) {
	// Find first # that could start a tag section
	firstHash := strings.Index(line, "#")
	if firstHash == -1 {
		// No hashtags, entire line is title
		return strings.TrimSpace(line), nil
	}

	// Split into potential title and tag section
	potentialTitle := line[:firstHash]
	tagSection := line[firstHash:]

	// Validate tag section: must match pattern #word( #word)*
	// Rule: after each #word, next chars must be " #" or end/whitespace
	validTags := []string{}
	i := 0
	for i < len(tagSection) {
		// Expect '#' at current position
		if tagSection[i] != '#' {
			// Invalid pattern, no tags
			return strings.TrimSpace(line), nil
		}

		// Find end of this tag word (next space or end)
		wordStart := i + 1
		wordEnd := wordStart
		for wordEnd < len(tagSection) && tagSection[wordEnd] != ' ' {
			wordEnd++
		}

		if wordEnd == wordStart {
			// Empty tag like "# ", invalid
			return strings.TrimSpace(line), nil
		}

		tagWord := tagSection[wordStart:wordEnd]
		validTags = append(validTags, tagWord)

		// Check what comes after this tag
		i = wordEnd
		if i >= len(tagSection) {
			// End of string, valid
			break
		}

		// Skip whitespace
		for i < len(tagSection) && tagSection[i] == ' ' {
			i++
		}

		if i >= len(tagSection) {
			// Only whitespace after tag, valid end
			break
		}

		// Next char must be '#' for another tag
		if tagSection[i] != '#' {
			// Found non-hashtag word after tags, invalid pattern
			return strings.TrimSpace(line), nil
		}
	}

	return strings.TrimSpace(potentialTitle), validTags
}

// OpenRawContent opens editor with raw text content, returns edited content
// This is a low-level primitive for direct content editing (e.g., raw TOML)
func OpenRawContent(initialContent string) (string, error) {
	return openEditor(initialContent, ".txt")
}

// openEditor creates temp file, opens editor, returns edited content
func openEditor(initialContent, suffix string) (string, error) {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = defaultEditor
	}

	// Ensure session directory exists
	if err := os.MkdirAll(session.Dir(), 0700); err != nil {
		return "", fmt.Errorf("failed to create session directory: %w", err)
	}

	// Create temp file
	tmpFile, err := os.CreateTemp(session.Dir(), tempFilePrefix+"*"+suffix)
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	// Write initial content
	if _, err := tmpFile.WriteString(initialContent); err != nil {
		tmpFile.Close()
		return "", fmt.Errorf("failed to write temp file: %w", err)
	}
	tmpFile.Close()

	// Launch editor
	cmd := exec.Command(editor, tmpPath)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("editor exited with error: %w", err)
	}

	// Read edited content
	editedBytes, err := os.ReadFile(tmpPath)
	if err != nil {
		return "", fmt.Errorf("failed to read edited content: %w", err)
	}

	return string(editedBytes), nil
}
