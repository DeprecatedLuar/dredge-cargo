package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/urfave/cli/v2"

	"github.com/DeprecatedLuar/dredge/internal/commands"
	"github.com/DeprecatedLuar/dredge/internal/crypto"
	"github.com/DeprecatedLuar/dredge/internal/selfheal"
	"github.com/DeprecatedLuar/dredge/internal/session"
)

var (
	debugMode  bool
	luckMode   bool
	searchMode bool
)

func main() {
	app := &cli.App{
		Name:  "dredge",
		Usage: "Encrypted storage for secrets, credentials, and config files",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "password",
				Aliases: []string{"p"},
				Usage:   "Password for decryption (skips prompt)",
			},
			&cli.BoolFlag{
				Name:        "debug",
				Usage:       "Enable debug output",
				Destination: &debugMode,
			},
			&cli.BoolFlag{
				Name:        "luck",
				Aliases:     []string{"l"},
				Usage:       "Force view top search result",
				Destination: &luckMode,
			},
			&cli.BoolFlag{
				Name:        "search",
				Aliases:     []string{"s"},
				Usage:       "Force show search list",
				Destination: &searchMode,
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
					return commands.HandleSearch(query, luckMode, searchMode)
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
				Action: func(c *cli.Context) error {
					return commands.HandleView(c.Args().Slice())
				},
			},
			{
				Name:                   "edit",
				Aliases:                []string{"e"},
				Usage:                  "Edit an item",
				SkipFlagParsing:        true,
				UseShortOptionHandling: false,
				Action: func(c *cli.Context) error {
					return commands.HandleEdit(c.Args().Slice())
				},
			},
			{
				Name:  "rm",
				Usage: "Remove an item",
				Action: func(c *cli.Context) error {
					return commands.HandleRemove(c.Args().Slice())
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
					return commands.HandleMove(c.Args().Slice())
				},
			},
			{
				Name:                   "link",
				Aliases:                []string{"ln"},
				Usage:                  "Link an item to a system path",
				SkipFlagParsing:        true,
				UseShortOptionHandling: false,
				Action: func(c *cli.Context) error {
					return commands.HandleLink(c.Args().Slice())
				},
			},
			{
				Name:  "unlink",
				Usage: "Unlink an item from system path",
				Action: func(c *cli.Context) error {
					return commands.HandleUnlink(c.Args().Slice())
				},
			},
			{
				Name:  "export",
				Usage: "Export a binary item to filesystem",
				Action: func(c *cli.Context) error {
					return commands.HandleExport(c.Args().Slice())
				},
			},
			{
				Name:  "init",
				Usage: "Initialize git repository for sync",
				Action: func(c *cli.Context) error {
					return commands.HandleInit(c.Args().Slice())
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
				Name:  "passwd",
				Usage: "Change vault password",
				Action: func(c *cli.Context) error {
					return commands.HandlePasswd()
				},
			},
		},
		Before: func(c *cli.Context) error {
			// Set debug mode for crypto package
			crypto.DebugMode = debugMode

			// Check if this is a new session (no cached password)
			isNewSession := !crypto.HasActiveSession()

			// If password provided via flag, try to derive and cache key immediately.
			// If vault doesn't exist yet, store as pending (used once by GetKeyWithVerification).
			if password := c.String("password"); password != "" {
				Debugf("Password provided via --password flag")
				if crypto.PasswordVerificationExists() {
					key, err := crypto.DeriveKeyFromVault(password)
					if err != nil {
						fmt.Fprintf(os.Stderr, "Warning: failed to verify --password flag: %v\n", err)
					} else {
						if err := crypto.CacheKey(key); err != nil {
							fmt.Fprintf(os.Stderr, "Warning: failed to cache key: %v\n", err)
						} else {
							Debugf("Key derived and cached from --password flag")
							isNewSession = true
						}
					}
				} else {
					// First-time vault — store pending, GetKeyWithVerification will use it
					crypto.SetPendingPassword(password)
					Debugf("Stored pending password for first-time vault setup")
					isNewSession = true
				}
			}

			// Run self-healing on new session
			if isNewSession {
				selfheal.Run()
			}

			// Ensure a git repo is connected (skip for init/help — those don't need it)
			sub := c.Args().First()
			if sub != "init" && sub != "help" && sub != "h" {
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
				cli.ShowAppHelp(c)
				return nil
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
			return commands.HandleSearch(query, luckMode, searchMode)
		},
	}

	if err := app.Run(os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func Debugf(format string, args ...any) {
	if debugMode {
		fmt.Printf("[DEBUG] "+format+"\n", args...)
	}
}
