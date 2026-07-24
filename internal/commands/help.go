package commands

import (
	gohelp "github.com/DeprecatedLuar/gohelp-luar"
)

func HandleHelp(args []string) error {
	root := gohelp.NewPage("dredge", "Encrypted storage for secrets, credentials, and config files").
		Usage("dredge <command> [flags]").
		Section("Items",
			gohelp.Item("add, a, new, +", "Add a new item", "dredge add 'ssh config' #ssh #config"),
			gohelp.Item("search, s", "Search for items", "dredge search ssh"),
			gohelp.Item("list, ls", "List all items"),
			gohelp.Item("view, v", "View an item"),
			gohelp.Item("edit, e", "Edit an item"),
			gohelp.Item("rm", "Remove an item"),
			gohelp.Item("undo", "Restore last deleted item"),
			gohelp.Item("mv, rename, rn", "Rename an item"),
			gohelp.Item("cat, c", "Output raw item content (for piping)"),
			gohelp.Item("copy, cp", "Copy item content to clipboard"),
			gohelp.Item("export", "Export a binary item to the filesystem"),
		).
		Section("Links",
			gohelp.Item("link, ln", "Link an item to a system path", "dredge link ssh-config ~/.ssh/config"),
			gohelp.Item("unlink", "Unlink an item from a system path"),
		).
		Section("Vault",
			gohelp.Item("init, use", "Initialize or activate a vault", "dredge init /path/to/vault"),
			gohelp.Item("lock", "Lock the vault (clears cached session key)"),
			gohelp.Item("passwd", "Change vault password"),
		).
		Section("Sync",
			gohelp.Item("remote", "Wire a git remote to the active vault", "dredge remote owner/repo"),
			gohelp.Item("push", "Reconcile, apply configured retention, and push"),
			gohelp.Item("pull", "Pull changes from remote (auto-commits first)"),
			gohelp.Item("sync", "Reconcile, apply configured retention, and push"),
			gohelp.Item("status", "Show pending changes"),
			gohelp.Item("history", "Inspect encrypted item history without a password", "dredge history <id>"),
			gohelp.Item("drop", "Discard uncommitted changes to prioritize remote", "dredge drop <id> OR dredge drop --all"),
		).
		Section("Flags",
			gohelp.Item("--password, -p", "Password for decryption (skips prompt)"),
			gohelp.Item("--vault", "Vault directory for this command (does not persist)"),
			gohelp.Item("--luck, -l", "Force view the top search result"),
			gohelp.Item("--no-lock", "Disable session timeout for this command"),
		).
		Text("Tip: bare args route automatically — 'dredge ssh' searches, 'dredge 1' opens result #1.")

	addPage := gohelp.NewPage("add", "Add a new item to the vault").
		Usage("dredge add [title] [-c content] [-t tag...] [--file path]").
		Text("Without flags, opens your $EDITOR with a template. Fill in the title, tags, and content, then save and close to create the item.").
		Section("Flags",
			gohelp.Item("-c CONTENT", "Inline content — skips the editor entirely", "dredge add 'db password' -c 'hunter2'"),
			gohelp.Item("-t TAG...", "One or more tags", "dredge add 'ssh key' -t ssh config"),
			gohelp.Item("--file, --import PATH", "Import a file — text files are stored inline, binaries go to encrypted blob storage", "dredge add --file ~/.ssh/id_ed25519"),
		).
		Text("Tags can also be written inline in the title as #words. Any #word trailing the title is treated as a tag.").
		Section("Editor format",
			gohelp.Item("line 1", "Title and optional trailing #tags"),
			gohelp.Item("line 2", "(blank)"),
			gohelp.Item("line 3+", "Content"),
		).
		Text("Saving an empty buffer cancels the add.")

	viewPage := gohelp.NewPage("view", "View an item's content").
		Usage("dredge view <id|number> [--raw]").
		Text("Accepts an item ID, a numbered result from the last search, or a search query that resolves to a single match.").
		Section("Flags",
			gohelp.Item("--raw, -r", "Print content only — no header, no formatting. Useful for piping.", "dredge view abc --raw | pbcopy"),
		).
		Text("'dredge cat' is shorthand for 'dredge view --raw' and is pipe-friendly by default.")

	editPage := gohelp.NewPage("edit", "Edit an existing item").
		Usage("dredge edit <id|number> [--metadata]").
		Text("Opens the item in $EDITOR using the same template format as add: title and #tags on line 1, content from line 3 onward.").
		Section("Flags",
			gohelp.Item("--metadata, -m", "Edit metadata only (title, tags, type, filename, mode) as raw TOML — content is untouched.", "dredge edit abc --metadata"),
		).
		Text("Saving without changes leaves the item unmodified. The modified timestamp is only updated when content actually changes.")

	linkPage := gohelp.NewPage("link", "Link an item to a path on the filesystem").
		Usage("dredge link <id|number> [path] [--force] [-p]").
		Text("Creates a plain-text copy of the item in .spawned/ and symlinks it to the target path. Changes to the spawned file are synced back into the vault automatically on next read.").
		Text("If no path is given, defaults to the current directory using the item's original filename or ID.").
		Section("Flags",
			gohelp.Item("--force, -f", "Overwrite an existing file or symlink at the target path"),
			gohelp.Item("-p, --parents", "Create parent directories if they don't exist", "dredge link abc ~/.config/app/config.toml -p"),
		).
		Text("Only text items can be linked. Use 'dredge unlink <id>' to remove the symlink and spawned copy.").
		Text("Auto-repair: Renamed symlinks are detected and tracked automatically. Deleted symlinks are cleaned up on session start.")

	historyPage := gohelp.NewPage("history", "Inspect encrypted Git history without decrypting it").
		Usage("dredge history [list|deleted|<id>|restore <id>]").
		Section("Commands",
			gohelp.Item("history, history list", "Summarize all current and historical IDs"),
			gohelp.Item("history deleted", "List deleted IDs still retained for recovery"),
			gohelp.Item("history <id>", "List item and storage blob versions with retention decisions"),
			gohelp.Item("history restore <id>", "Restore the newest retained encrypted item and storage blobs"),
		).
		Text("History is derived from exact Git paths and object IDs. It never prompts for the vault password. Restored blobs are left as ordinary uncommitted additions.").
		Text("Retention is configured in dredge.toml. Item and storage version counts and encrypted-byte limits are independent; deleted IDs remain recoverable for history.deleted.retain_for. Push and sync compact only when the policy removes history. Pull never compacts or force-pushes.").
		Text("Compaction uses an exact force-with-lease expectation. A concurrent remote update is never overwritten; synchronization stops safely and retains a local recovery reference.").
		Text("Password rotation limitation: historical blobs are restored byte-for-byte. Blobs created before a password change may not decrypt with the current password; historical re-encryption is not performed.")

	gohelp.Run(append([]string{"help"}, args...), root, addPage, viewPage, editPage, linkPage, historyPage)
	return nil
}
