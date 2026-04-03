package commands

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/DeprecatedLuar/dredge/internal/crypto"
	"github.com/DeprecatedLuar/dredge/internal/storage"
	"github.com/DeprecatedLuar/dredge/internal/ui"
)

func HandleCopy(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: dredge copy <id>")
	}

	ids, err := ResolveArgs(args[:1])
	if err != nil {
		return err
	}
	id := ids[0]

	key, err := crypto.GetKeyWithVerification()
	if err != nil {
		return fmt.Errorf("failed to get key: %w", err)
	}

	item, err := storage.ReadItem(id, key)
	if err != nil {
		return fmt.Errorf("failed to read item: %w", err)
	}

	if item.Type == storage.TypeBinary {
		return fmt.Errorf("binary items cannot be copied to clipboard — use 'dredge export %s' instead", id)
	}

	if err := writeToClipboard(item.Content.Text); err != nil {
		return fmt.Errorf("clipboard error: %w", err)
	}

	fmt.Printf("Copied %s to clipboard\n", ui.FormatItem(id, item.Title, item.Tags, "it#"))
	return nil
}

func writeToClipboard(text string) error {
	cmd, err := clipboardCmd()
	if err != nil {
		return err
	}
	cmd.Stdin = strings.NewReader(text)
	return cmd.Run()
}

func clipboardCmd() (*exec.Cmd, error) {
	if runtime.GOOS == "darwin" {
		return exec.Command("pbcopy"), nil
	}

	// Linux: prefer XDG_SESSION_TYPE, fall back to env var presence
	switch os.Getenv("XDG_SESSION_TYPE") {
	case "wayland":
		if path, err := exec.LookPath("wl-copy"); err == nil {
			return exec.Command(path), nil
		}
		return nil, fmt.Errorf("wl-copy not found — install wl-clipboard")
	case "x11":
		return x11ClipboardCmd()
	}

	// Fallback: check env vars directly
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		if path, err := exec.LookPath("wl-copy"); err == nil {
			return exec.Command(path), nil
		}
	}
	if os.Getenv("DISPLAY") != "" {
		return x11ClipboardCmd()
	}

	return nil, fmt.Errorf("no clipboard tool found (set DISPLAY or WAYLAND_DISPLAY, and install xclip/xsel or wl-clipboard)")
}

func x11ClipboardCmd() (*exec.Cmd, error) {
	if path, err := exec.LookPath("xclip"); err == nil {
		return exec.Command(path, "-selection", "clipboard"), nil
	}
	if path, err := exec.LookPath("xsel"); err == nil {
		return exec.Command(path, "--clipboard", "--input"), nil
	}
	return nil, fmt.Errorf("no X11 clipboard tool found — install xclip or xsel")
}
