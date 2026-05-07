package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/urfave/cli/v2"

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

// hoistGlobalFlag moves any occurrence of the named flags to immediately after the binary name
// so urfave/cli parses them as global flags regardless of where the user placed them.
func hoistGlobalFlag(args []string, flags ...string) []string {
	if len(args) < 2 {
		return args
	}
	isFlagMatch := func(a string) bool {
		for _, f := range flags {
			if a == f {
				return true
			}
		}
		return false
	}
	var hoisted, rest []string
	for _, a := range args[1:] {
		if isFlagMatch(a) {
			hoisted = append(hoisted, a)
		} else {
			rest = append(rest, a)
		}
	}
	if len(hoisted) == 0 {
		return args
	}
	return append([]string{args[0]}, append(hoisted, rest...)...)
}

// resolveLucky replaces the first non-flag arg with the top search result when luckMode is on.
// This lets any command accept a search query in place of a direct item ID.
func resolveLucky(args []string) ([]string, error) {
	if !luckMode || len(args) == 0 {
		return args, nil
	}
	for i, arg := range args {
		if len(arg) == 0 || arg[0] == '-' {
			continue
		}
		id, err := commands.ResolveSingle(arg, true)
		if err != nil {
			return nil, err
		}
		result := make([]string, len(args))
		copy(result, args)
		result[i] = id
		return result, nil
	}
	return args, nil
}

func main() {
	app := &cli.App{
		Name:  "dredge",
		Usage: "Encrypted storage for secrets, credentials, and config files",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "password",
				Aliases: []string{"p"},
				Usage:   "Password for decryption (skips prompt)",
				EnvVars: []string{"DREDGE_PASSWORD"},
			},
			&cli.StringFlag{
				Name:    "vault",
				Usage:   "Vault directory to use for this command (does not persist)",
				EnvVars: []string{"DREDGE_VAULT"},
			},
			&cli.BoolFlag{
				Name:        "debug",
				Usage:       "Enable debug output",
				Destination: &debugMode,
			},
			&cli.BoolFlag{
				Name:        "lets-go-gambling",
				Aliases:     []string{"l"},
				Usage:       "Resolve query to top search result and pass to command",
				Destination: &luckMode,
			},
			&cli.BoolFlag{
				Name:        "dev",
				Usage:       "Skip git repo check (for local testing without a remote)",
				Destination: &devMode,
			},
			&cli.BoolFlag{
				Name:        "no-lock",
				Usage:       "Disable session timeout for this command",
				Destination: &noLock,
			},
		},
		Commands: []*cli.Command{
			{
				Name:                   "add",
				Aliases:                []string{"a", "new", "+"},
				Usage:                  "Add a new item",
				SkipFlagParsing:        true,
				UseShortOptionHandling: false,
				Action: func(c *cli.Context) error {
					// Manual arg parsing handles all flags (-t, -c, --file)
					// We pass all args and let HandleAdd parse them
					return commands.HandleAdd(c.Args().Slice(), "")
				},
			},
			{
				Name:    "search",
				Aliases: []string{"s"},
				Usage:   "Search for items",
				Action: func(c *cli.Context) error {
					query := strings.Join(c.Args().Slice(), " ")
					return commands.HandleSearch(query, luckMode)
				},
			},
			{
				Name:    "list",
				Aliases: []string{"ls"},
				Usage:   "List all items",
				Action: func(c *cli.Context) error {
					return commands.HandleList(c.Args().Slice())
				},
			},
			{
				Name:    "view",
				Aliases: []string{"v"},
				Usage:   "View an item by ID",
				Flags: []cli.Flag{
					&cli.BoolFlag{Name: "raw", Aliases: []string{"r"}, Usage: "Output raw content only"},
				},
				Action: func(c *cli.Context) error {
					args, err := resolveLucky(c.Args().Slice())
					if err != nil {
						return err
					}
					return commands.HandleView(args, c.Bool("raw"))
				},
			},
			{
				Name:    "cat",
				Aliases: []string{"c"},
				Usage:   "Output raw item content (for piping)",
				Action: func(c *cli.Context) error {
					args, err := resolveLucky(c.Args().Slice())
					if err != nil {
						return err
					}
					return commands.HandleCat(args)
				},
			},
			{
				Name:                   "edit",
				Aliases:                []string{"e"},
				Usage:                  "Edit an item",
				SkipFlagParsing:        true,
				UseShortOptionHandling: false,
				Action: func(c *cli.Context) error {
					args, err := resolveLucky(c.Args().Slice())
					if err != nil {
						return err
					}
					return commands.HandleEdit(args)
				},
			},
			{
				Name:  "rm",
				Usage: "Remove an item",
				Action: func(c *cli.Context) error {
					args := c.Args().Slice()
					if luckMode {
						id, err := commands.SearchTopResult(strings.Join(args, " "))
						if err != nil {
							return err
						}
						args = []string{id}
					}
					return commands.HandleRemove(args)
				},
			},
			{
				Name:  "undo",
				Usage: "Restore last deleted item",
				Action: func(c *cli.Context) error {
					return commands.HandleUndo(c.Args().Slice())
				},
			},
			{
				Name:    "mv",
				Aliases: []string{"rename", "rn"},
				Usage:   "Rename an item ID",
				Action: func(c *cli.Context) error {
					args, err := resolveLucky(c.Args().Slice())
					if err != nil {
						return err
					}
					return commands.HandleMove(args)
				},
			},
			{
				Name:                   "link",
				Aliases:                []string{"ln"},
				Usage:                  "Link an item to a system path",
				SkipFlagParsing:        true,
				UseShortOptionHandling: false,
				Action: func(c *cli.Context) error {
					args, err := resolveLucky(c.Args().Slice())
					if err != nil {
						return err
					}
					return commands.HandleLink(args)
				},
			},
			{
				Name:  "unlink",
				Usage: "Unlink an item from system path",
				Action: func(c *cli.Context) error {
					args, err := resolveLucky(c.Args().Slice())
					if err != nil {
						return err
					}
					return commands.HandleUnlink(args)
				},
			},
			{
				Name:    "copy",
				Aliases: []string{"cp"},
				Usage:   "Copy item content to clipboard",
				Action: func(c *cli.Context) error {
					args, err := resolveLucky(c.Args().Slice())
					if err != nil {
						return err
					}
					return commands.HandleCopy(args)
				},
			},
			{
				Name:  "export",
				Usage: "Export a binary item to filesystem",
				Action: func(c *cli.Context) error {
					args, err := resolveLucky(c.Args().Slice())
					if err != nil {
						return err
					}
					return commands.HandleExport(args)
				},
			},
			{
				Name:    "init",
				Aliases: []string{"use"},
				Usage:   "Initialize or activate a vault at the given path (default: current dir)",
				Action: func(c *cli.Context) error {
					return commands.HandleInit(c.Args().Slice())
				},
			},
			{
				Name:  "remote",
				Usage: "Wire a git remote to the active vault",
				Action: func(c *cli.Context) error {
					return commands.HandleRemote(c.Args().Slice())
				},
			},
			{
				Name:  "push",
				Usage: "Push changes to remote",
				Action: func(c *cli.Context) error {
					return commands.HandlePush(c.Args().Slice())
				},
			},
			{
				Name:  "pull",
				Usage: "Pull changes from remote",
				Action: func(c *cli.Context) error {
					return commands.HandlePull(c.Args().Slice())
				},
			},
			{
				Name:  "sync",
				Usage: "Sync with remote (pull + push)",
				Action: func(c *cli.Context) error {
					return commands.HandleSync(c.Args().Slice())
				},
			},
			{
				Name:  "status",
				Usage: "Show pending changes",
				Action: func(c *cli.Context) error {
					return commands.HandleStatus(c.Args().Slice())
				},
			},
			{
				Name:  "lock",
				Usage: "Lock the vault (clears cached session key)",
				Action: func(c *cli.Context) error {
					return commands.HandleLock()
				},
			},
			{
				Name:  "passwd",
				Usage: "Change vault password",
				Action: func(c *cli.Context) error {
					return commands.HandlePasswd()
				},
			},
			{
				Name:    "update",
				Aliases: []string{"up"},
				Usage:   "Update dredge to the latest version",
				Action: func(c *cli.Context) error {
					return commands.HandleUpdate(version, githubRepo)
				},
			},
			{
				Name:    "help",
				Aliases: []string{"h"},
				Usage:   "Show help",
				Action: func(c *cli.Context) error {
					return commands.HandleHelp(c.Args().Slice())
				},
			},
		},
		Before: func(c *cli.Context) error {
			// If --vault/DREDGE_VAULT is set, override for this invocation only
			if v := strings.TrimSpace(c.String("vault")); v != "" {
				abs, err := filepath.Abs(v)
				if err != nil {
					return fmt.Errorf("failed to resolve vault path: %w", err)
				}
				storage.SetVaultOverride(abs)
				session.SetVaultPath(abs)
			} else if vaultDir, err := storage.GetDredgeDir(); err == nil {
				session.SetVaultPath(vaultDir)
			}

			// Set debug mode for crypto package
			crypto.DebugMode = debugMode
			crypto.NoLock = noLock

			// Check if this is a new session (no cached password)
			isNewSession := !crypto.HasActiveSession()

			// If password provided via --password flag or DREDGE_PASSWORD env var,
			// verify immediately and hard-error on failure — never fall back to prompt.
			// If vault doesn't exist yet, store as pending (used once by GetKeyWithVerification).
			if password := c.String("password"); password != "" {
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

			// Determine the subcommand (empty string means no args → show help)
			sub := c.Args().First()

			// Commands that don't need vault access
			passiveCommands := []string{"", "help", "h", "update", "up", "init", "use", "lock"}

			contains := func(list []string, s string) bool {
				for _, v := range list {
					if v == s {
						return true
					}
				}
				return false
			}

			isPassiveCommand := contains(passiveCommands, sub)

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
		},
		Action: func(c *cli.Context) error {
			// Default action: smart query routing
			// Handles: dredge 1, dredge <id>, dredge <search-query>
			if c.NArg() == 0 {
				return commands.HandleHelp(nil)
			}

			args := c.Args().Slice()
			firstArg := args[0]

			// Try as numbered result first (if single numeric arg)
			if len(args) == 1 {
				if num, err := strconv.Atoi(firstArg); err == nil && num > 0 {
					if id, cacheErr := session.GetCachedResult(num); cacheErr == nil {
						return commands.HandleView([]string{id})
					}
					// If cache miss, fall through to try as ID/search
				}

				// Try as direct ID
				if viewErr := commands.HandleView([]string{firstArg}); viewErr == nil {
					return nil
				} else {
					Debugf("HandleView failed, falling back to search: %v", viewErr)
				}
			}

			// Fall back to search
			query := strings.Join(args, " ")
			return commands.HandleSearch(query, luckMode)
		},
	}

	cli.HelpPrinter = func(_ io.Writer, _ string, _ interface{}) {
		commands.HandleHelp(nil) //nolint
	}

	// Hoist -l/--lets-go-gambling to before the subcommand so urfave treats it as a global flag
	// regardless of where the user places it (e.g. `dredge cp foo -l`).
	runArgs := hoistGlobalFlag(os.Args, "-l", "--lets-go-gambling")

	if err := app.Run(runArgs); err != nil && err != flag.ErrHelp {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func Debugf(format string, args ...any) {
	if debugMode {
		fmt.Printf("[DEBUG] "+format+"\n", args...)
	}
}
