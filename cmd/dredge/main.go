package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/DeprecatedLuar/dredge-cargo/internal/commands"
	"github.com/DeprecatedLuar/dredge-cargo/internal/crypto"
	"github.com/DeprecatedLuar/dredge-cargo/internal/selfheal"
	"github.com/DeprecatedLuar/dredge-cargo/internal/session"
	"github.com/DeprecatedLuar/dredge-cargo/internal/storage"
)

const githubRepo = "DeprecatedLuar/dredge-cargo"

var version = "dev"

var (
	debugMode bool
	luckMode  bool
	devMode   bool
	noLock    bool
)

// ============================================================================
// Flag Helpers - Simple, focused utilities for organic flag handling
// ============================================================================

// extractGlobalFlags pulls global flags from anywhere in args and returns remaining args.
// Supports: --debug, -l/--lets-go-gambling, --password=X/-p=X, --vault=X, --dev, --no-lock
func extractGlobalFlags(args []string) []string {
	var remaining []string
	skipNext := false

	for i, arg := range args {
		if skipNext {
			skipNext = false
			continue
		}

		switch {
		case arg == "--debug":
			debugMode = true
		case arg == "-l", arg == "--lets-go-gambling":
			luckMode = true
		case arg == "--dev":
			devMode = true
		case arg == "--no-lock":
			noLock = true
		case arg == "--password", arg == "-p":
			if i+1 < len(args) {
				os.Setenv("DREDGE_PASSWORD", args[i+1])
				skipNext = true
			}
		case strings.HasPrefix(arg, "--password="):
			os.Setenv("DREDGE_PASSWORD", strings.TrimPrefix(arg, "--password="))
		case strings.HasPrefix(arg, "-p="):
			os.Setenv("DREDGE_PASSWORD", strings.TrimPrefix(arg, "-p="))
		case arg == "--vault":
			if i+1 < len(args) {
				os.Setenv("DREDGE_VAULT", args[i+1])
				skipNext = true
			}
		case strings.HasPrefix(arg, "--vault="):
			os.Setenv("DREDGE_VAULT", strings.TrimPrefix(arg, "--vault="))
		default:
			remaining = append(remaining, arg)
		}
	}

	return remaining
}

// hasFlag checks if any of the given flags exist in args
func hasFlag(args []string, flags ...string) bool {
	for _, arg := range args {
		for _, flag := range flags {
			if arg == flag {
				return true
			}
		}
	}
	return false
}

// removeFlags returns args with all specified flags removed
func removeFlags(args []string, flags ...string) []string {
	var result []string
	for _, arg := range args {
		isFlag := false
		for _, flag := range flags {
			if arg == flag {
				isFlag = true
				break
			}
		}
		if !isFlag {
			result = append(result, arg)
		}
	}
	return result
}

// ============================================================================
// Luck Mode - Resolve search queries to item IDs
// ============================================================================

// resolveLucky replaces all non-flag args with the top search result when luckMode is on.
func resolveLucky(args []string) ([]string, error) {
	if !luckMode || len(args) == 0 {
		return args, nil
	}

	// Collect all non-flag args to form complete search query
	var queryParts []string
	var flags []string
	for _, arg := range args {
		if len(arg) > 0 && arg[0] == '-' {
			flags = append(flags, arg)
		} else if len(arg) > 0 {
			queryParts = append(queryParts, arg)
		}
	}

	if len(queryParts) == 0 {
		return args, nil
	}

	// Join all non-flag args into single query and resolve
	query := strings.Join(queryParts, " ")
	id, err := commands.ResolveSingle(query, true)
	if err != nil {
		return nil, err
	}

	// Return ID + flags
	result := []string{id}
	result = append(result, flags...)
	return result, nil
}

// ============================================================================
// Before Hook - Vault setup, password handling, self-heal
// ============================================================================

func runBefore(commandName string) error {
	// If --vault/DREDGE_VAULT is set, override for this invocation only
	if v := strings.TrimSpace(os.Getenv("DREDGE_VAULT")); v != "" {
		abs, err := filepath.Abs(v)
		if err != nil {
			return fmt.Errorf("failed to resolve vault path: %w", err)
		}
		Debugf("Vault override: %s (via --vault or DREDGE_VAULT)", abs)
		storage.SetVaultOverride(abs)
		session.SetVaultPath(abs)
	} else if vaultDir, err := storage.GetDredgeDir(); err == nil {
		Debugf("Vault path from registry: %s", vaultDir)
		session.SetVaultPath(vaultDir)
	} else {
		Debugf("Failed to get vault directory: %v", err)
	}

	// Set debug mode for crypto package
	crypto.DebugMode = debugMode
	crypto.NoLock = noLock

	// Check if this is a new session (no cached password)
	isNewSession := !crypto.HasActiveSession()

	// If password provided via --password flag or DREDGE_PASSWORD env var,
	// verify immediately and hard-error on failure — never fall back to prompt.
	// If vault doesn't exist yet, store as pending (used once by GetKeyWithVerification).
	if password := os.Getenv("DREDGE_PASSWORD"); password != "" {
		Debugf("Password provided via --password/DREDGE_PASSWORD")
		if crypto.PasswordVerificationExists() {
			key, err := crypto.DeriveKeyFromVault(password)
			if err != nil {
				return fmt.Errorf("wrong password")
			}
			if err := crypto.CacheKey(key); err != nil {
				return fmt.Errorf("failed to cache key: %w", err)
			}
			Debugf("Key derived and cached from --password/DREDGE_PASSWORD")
			isNewSession = true
		} else {
			// First-time vault — store pending, GetKeyWithVerification will use it
			crypto.SetPendingPassword(password)
			Debugf("Stored pending password for first-time vault setup")
			isNewSession = true
		}
	}

	// Commands that don't need vault access
	passiveCommands := map[string]bool{
		"help": true, "h": true,
		"update": true, "up": true,
		"init": true, "use": true,
		"lock": true,
	}

	isPassiveCommand := passiveCommands[commandName]

	// Run self-healing on new session (skip for passive commands — no vault access needed)
	if isNewSession && !isPassiveCommand {
		selfheal.Run()
	}

	// Ensure vault is initialized
	if !devMode && !isPassiveCommand {
		if err := commands.EnsureInitialized(); err != nil {
			return err
		}
	}

	return nil
}

// ============================================================================
// Main Router - Simple switch-based command dispatch
// ============================================================================

func main() {
	// Extract global flags from anywhere in args
	args := extractGlobalFlags(os.Args[1:])

	// No args = show help
	if len(args) == 0 {
		if err := commands.HandleHelp(nil); err != nil {
			die(err)
		}
		return
	}

	cmd := args[0]
	cmdArgs := args[1:]

	// Route to command handlers
	var err error

	switch cmd {
	// Add
	case "add", "a", "new", "+":
		if err = runBefore(cmd); err != nil {
			die(err)
		}
		err = commands.HandleAdd(cmdArgs, "")

	// Search
	case "search", "s":
		if err = runBefore(cmd); err != nil {
			die(err)
		}
		query := strings.Join(cmdArgs, " ")
		err = commands.HandleSearch(query, luckMode)

	// List
	case "list", "ls":
		if err = runBefore(cmd); err != nil {
			die(err)
		}
		err = commands.HandleList(cmdArgs)

	// View
	case "view", "v":
		if err = runBefore(cmd); err != nil {
			die(err)
		}
		rawMode := hasFlag(cmdArgs, "--raw", "-r")
		cmdArgs = removeFlags(cmdArgs, "--raw", "-r")
		if cmdArgs, err = resolveLucky(cmdArgs); err != nil {
			die(err)
		}
		err = commands.HandleView(cmdArgs, rawMode)

	// Cat
	case "cat", "c":
		if err = runBefore(cmd); err != nil {
			die(err)
		}
		if cmdArgs, err = resolveLucky(cmdArgs); err != nil {
			die(err)
		}
		err = commands.HandleCat(cmdArgs)

	// Edit
	case "edit", "e":
		if err = runBefore(cmd); err != nil {
			die(err)
		}
		if cmdArgs, err = resolveLucky(cmdArgs); err != nil {
			die(err)
		}
		err = commands.HandleEdit(cmdArgs)

	// Remove
	case "rm":
		if err = runBefore(cmd); err != nil {
			die(err)
		}
		if luckMode {
			id, searchErr := commands.SearchTopResult(strings.Join(cmdArgs, " "))
			if searchErr != nil {
				die(searchErr)
			}
			cmdArgs = []string{id}
		}
		err = commands.HandleRemove(cmdArgs)

	// Undo
	case "undo":
		if err = runBefore(cmd); err != nil {
			die(err)
		}
		err = commands.HandleUndo(cmdArgs)

	// Move/Rename
	case "mv", "rename", "rn":
		if err = runBefore(cmd); err != nil {
			die(err)
		}
		if cmdArgs, err = resolveLucky(cmdArgs); err != nil {
			die(err)
		}
		err = commands.HandleMove(cmdArgs)

	// Link
	case "link", "ln":
		if err = runBefore(cmd); err != nil {
			die(err)
		}
		if cmdArgs, err = resolveLucky(cmdArgs); err != nil {
			die(err)
		}
		err = commands.HandleLink(cmdArgs)

	// Unlink
	case "unlink":
		if err = runBefore(cmd); err != nil {
			die(err)
		}
		if cmdArgs, err = resolveLucky(cmdArgs); err != nil {
			die(err)
		}
		err = commands.HandleUnlink(cmdArgs)

	// Copy
	case "copy", "cp":
		if err = runBefore(cmd); err != nil {
			die(err)
		}
		if cmdArgs, err = resolveLucky(cmdArgs); err != nil {
			die(err)
		}
		err = commands.HandleCopy(cmdArgs)

	// Export
	case "export":
		if err = runBefore(cmd); err != nil {
			die(err)
		}
		if cmdArgs, err = resolveLucky(cmdArgs); err != nil {
			die(err)
		}
		err = commands.HandleExport(cmdArgs)

	// Init
	case "init", "use":
		err = commands.HandleInit(cmdArgs)

	// Git commands
	case "remote":
		if err = runBefore(cmd); err != nil {
			die(err)
		}
		err = commands.HandleRemote(cmdArgs)

	case "push":
		if err = runBefore(cmd); err != nil {
			die(err)
		}
		err = commands.HandlePush(cmdArgs)

	case "pull":
		if err = runBefore(cmd); err != nil {
			die(err)
		}
		err = commands.HandlePull(cmdArgs)

	case "sync":
		if err = runBefore(cmd); err != nil {
			die(err)
		}
		err = commands.HandleSync(cmdArgs)

	case "status":
		if err = runBefore(cmd); err != nil {
			die(err)
		}
		err = commands.HandleStatus(cmdArgs)

	case "drop":
		if err = runBefore(cmd); err != nil {
			die(err)
		}
		dropAll := hasFlag(cmdArgs, "--all")
		cmdArgs = removeFlags(cmdArgs, "--all")
		err = commands.HandleDrop(cmdArgs, dropAll)

	// Lock
	case "lock":
		err = commands.HandleLock()

	// Passwd
	case "passwd":
		if err = runBefore(cmd); err != nil {
			die(err)
		}
		err = commands.HandlePasswd()

	// Update
	case "update", "up":
		err = commands.HandleUpdate(version, githubRepo)

	// Help
	case "help", "h":
		err = commands.HandleHelp(cmdArgs)

	// Default: Smart query routing (numeric → cache, ID → view, else → search)
	default:
		if err = runBefore(""); err != nil {
			die(err)
		}

		// Restore full args for smart routing (includes what looked like a command)
		fullArgs := append([]string{cmd}, cmdArgs...)

		// Try as numbered result first (if single numeric arg)
		if len(fullArgs) == 1 {
			if num, parseErr := strconv.Atoi(fullArgs[0]); parseErr == nil && num > 0 {
				if id, cacheErr := session.GetCachedResult(num); cacheErr == nil {
					err = commands.HandleView([]string{id}, false)
					break
				}
				// If cache miss, fall through to try as ID/search
			}

			// Try as direct ID
			if viewErr := commands.HandleView([]string{fullArgs[0]}, false); viewErr == nil {
				return // Success
			} else {
				Debugf("HandleView failed, falling back to search: %v", viewErr)
			}
		}

		// Fall back to search
		query := strings.Join(fullArgs, " ")
		err = commands.HandleSearch(query, luckMode)
	}

	if err != nil {
		die(err)
	}
}

// ============================================================================
// Utilities
// ============================================================================

func Debugf(format string, args ...any) {
	if debugMode {
		fmt.Printf("[DEBUG] "+format+"\n", args...)
	}
}

func die(err error) {
	fmt.Fprintf(os.Stderr, "Error: %v\n", err)
	os.Exit(1)
}
