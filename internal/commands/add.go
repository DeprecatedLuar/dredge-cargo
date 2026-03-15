package commands

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/DeprecatedLuar/dredge/internal/crypto"
	"github.com/DeprecatedLuar/dredge/internal/editor"
	"github.com/DeprecatedLuar/dredge/internal/git"
	"github.com/DeprecatedLuar/dredge/internal/storage"
	"github.com/DeprecatedLuar/dredge/internal/ui"
)

// warnIfUnpushed prints a dim warning if the git repo has uncommitted or unpushed changes.
// Silent on all errors.
func warnIfUnpushed() {
	dredgeDir, err := storage.GetDredgeDir()
	if err != nil {
		return
	}
	if git.HasUnpushedChanges(dredgeDir) {
		ui.PrintUnpushedWarning()
	}
}

const (
	idLength   = 3
	maxRetries = 10
)

// isTextContent checks if content is text (valid UTF-8, no null bytes)
func isTextContent(data []byte) bool {
	return utf8.Valid(data) && !bytes.Contains(data, []byte{0})
}

func generateID() (string, error) {
	bytes := make([]byte, idLength)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("failed to generate random bytes: %w", err)
	}
	id := base64.RawURLEncoding.EncodeToString(bytes)
	return id[:idLength], nil
}

// parseAddArgs manually parses args to extract title, content, tags, and file path
// Supports flexible flag ordering: title can come first, -c, -t, and --file can be in any order
func parseAddArgs(args []string) (title, content, filePath string, tags []string) {
	if len(args) == 0 {
		return "", "", "", nil
	}

	// Find flag positions
	cPos := -1
	tPos := -1
	filePos := -1
	for i, arg := range args {
		if arg == "-c" {
			cPos = i
		} else if arg == "-t" {
			tPos = i
		} else if arg == "--file" {
			filePos = i
		}
	}

	// Extract title (everything before first flag)
	firstFlagPos := len(args)
	flagPositions := []int{}
	if cPos != -1 {
		flagPositions = append(flagPositions, cPos)
	}
	if tPos != -1 {
		flagPositions = append(flagPositions, tPos)
	}
	if filePos != -1 {
		flagPositions = append(flagPositions, filePos)
	}

	if len(flagPositions) > 0 {
		firstFlagPos = flagPositions[0]
		for _, pos := range flagPositions {
			if pos < firstFlagPos {
				firstFlagPos = pos
			}
		}
	}

	if firstFlagPos > 0 {
		title = strings.Join(args[:firstFlagPos], " ")
	}

	// Extract content (between -c and next flag or end)
	if cPos != -1 {
		contentStart := cPos + 1
		contentEnd := len(args)
		// If -t comes after -c, content ends at -t
		if tPos != -1 && tPos > cPos {
			contentEnd = tPos
		}
		if contentStart < contentEnd {
			content = strings.Join(args[contentStart:contentEnd], " ")
		}
	}

	// Extract tags (after -t until next flag or end)
	if tPos != -1 {
		tagsStart := tPos + 1
		tagsEnd := len(args)
		// If -c comes after -t, tags end at -c
		if cPos != -1 && cPos > tPos {
			tagsEnd = cPos
		}
		// If --file comes after -t, tags end at --file
		if filePos != -1 && filePos > tPos {
			if cPos == -1 || filePos < cPos {
				tagsEnd = filePos
			}
		}
		if tagsStart < tagsEnd {
			tags = args[tagsStart:tagsEnd]
		}
	}

	// Extract file path (single arg after --file)
	if filePos != -1 && filePos+1 < len(args) {
		filePath = args[filePos+1]
	}

	return title, content, filePath, tags
}

func handleAddFile(args []string, filePath string) error {
	// Parse title and tags from args (ignore -c content flag for files)
	title, _, _, tags := parseAddArgs(args)

	// Validate file exists
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	// Read file content
	fileBytes, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	// Get filename, size, and permissions
	filename := filepath.Base(filePath)
	fileSize := fileInfo.Size()
	fileMode := uint32(fileInfo.Mode().Perm())

	// Use filename without extension as title if not provided
	if title == "" {
		title = strings.TrimSuffix(filename, filepath.Ext(filename))
	}

	// Detect if content is text or binary
	var item *storage.Item
	if isTextContent(fileBytes) {
		// Text file: store as TypeText with plain content
		item = &storage.Item{
			Title:    title,
			Tags:     tags,
			Type:     storage.TypeText,
			Created:  time.Now(),
			Modified: time.Now(),
			Filename: filename,
			Size:     &fileSize,
			Mode:     &fileMode,
			Content: storage.ItemContent{
				Text: string(fileBytes),
			},
		}
	} else {
		// Binary file: store as TypeBinary with base64-encoded content
		encoded := base64.StdEncoding.EncodeToString(fileBytes)
		item = &storage.Item{
			Title:    title,
			Tags:     tags,
			Type:     storage.TypeBinary,
			Created:  time.Now(),
			Modified: time.Now(),
			Filename: filename,
			Size:     &fileSize,
			Mode:     &fileMode,
			Content: storage.ItemContent{
				Text: encoded,
			},
		}
	}

	// Generate unique ID
	var id string
	for i := 0; i < maxRetries; i++ {
		id, err = generateID()
		if err != nil {
			return fmt.Errorf("failed to generate ID: %w", err)
		}

		exists, err := storage.ItemExists(id)
		if err != nil {
			return fmt.Errorf("failed to check item existence: %w", err)
		}
		if !exists {
			break
		}

		if i == maxRetries-1 {
			return fmt.Errorf("failed to generate unique ID after %d attempts", maxRetries)
		}
	}

	// Get master key
	key, err := crypto.GetKeyWithVerification()
	if err != nil {
		return fmt.Errorf("failed to get key: %w", err)
	}

	if err := storage.CreateItem(id, item, key); err != nil {
		return fmt.Errorf("failed to create item: %w", err)
	}

	// Show appropriate output based on type
	if item.Type == storage.TypeText {
		fmt.Printf("+ %s (text from %s, %d bytes)\n", ui.FormatItem(id, item.Title, item.Tags, "it#"), filename, fileSize)
	} else {
		fmt.Printf("+ %s (binary: %s, %d bytes)\n", ui.FormatItem(id, item.Title, item.Tags, "it#"), filename, fileSize)
	}
	return nil
}

func HandleAdd(args []string, _ string) error {
	// Parse args (empty args returns empty title/content/tags/filePath)
	title, content, filePath, tags := parseAddArgs(args)

	// If --file flag provided, handle binary item
	if filePath != "" {
		if err := handleAddFile(args, filePath); err != nil {
			return err
		}
		warnIfUnpushed()
		return nil
	}

	// Get master key BEFORE opening editor (checks/creates .dredge-key)
	key, err := crypto.GetKeyWithVerification()
	if err != nil {
		return fmt.Errorf("failed to get key: %w", err)
	}

	var item *storage.Item

	// If no content provided, open editor (includes empty args case)
	if content == "" {
		item, err = editor.OpenForNewItem(title, tags)
		if err != nil {
			return fmt.Errorf("failed to create item via editor: %w", err)
		}
	} else {
		// Create item directly from CLI args
		if title == "" {
			return fmt.Errorf("title cannot be empty")
		}
		item = storage.NewTextItem(title, content, tags)
	}

	// Generate unique ID
	var id string
	for i := 0; i < maxRetries; i++ {
		id, err = generateID()
		if err != nil {
			return fmt.Errorf("failed to generate ID: %w", err)
		}

		exists, err := storage.ItemExists(id)
		if err != nil {
			return fmt.Errorf("failed to check item existence: %w", err)
		}
		if !exists {
			break
		}

		if i == maxRetries-1 {
			return fmt.Errorf("failed to generate unique ID after %d attempts", maxRetries)
		}
	}

	if err := storage.CreateItem(id, item, key); err != nil {
		return fmt.Errorf("failed to create item: %w", err)
	}

	fmt.Println("+ " + ui.FormatItem(id, item.Title, item.Tags, "it#"))
	warnIfUnpushed()
	return nil
}
