package commands

import (
	gohelp "github.com/DeprecatedLuar/gohelp-luar"
)

func HandleHelp(args []string) error {
	root := gohelp.NewPage("dredge", "Encrypted storage for secrets, credentials, and config files").
		Usage("dredge <command> [flags]").
		Section("Items",
			gohelp.Cmd("add, a, new, +", "Add a new item").
				Example("dredge add 'ssh config' #ssh #config"),
			gohelp.Cmd("search, s", "Search for items").
				Example("dredge search ssh"),
			gohelp.Cmd("list, ls", "List all items"),
			gohelp.Cmd("view, v", "View an item"),
			gohelp.Cmd("edit, e", "Edit an item"),
			gohelp.Cmd("rm", "Remove an item"),
			gohelp.Cmd("undo", "Restore last deleted item"),
			gohelp.Cmd("mv, rename, rn", "Rename an item"),
			gohelp.Cmd("cat, c", "Output raw item content (for piping)"),
			gohelp.Cmd("copy, cp", "Copy item content to clipboard"),
			gohelp.Cmd("export", "Export a binary item to the filesystem"),
		).
		Section("Links",
			gohelp.Cmd("link, ln", "Link an item to a system path").
				Example("dredge link ssh-config ~/.ssh/config"),
			gohelp.Cmd("unlink", "Unlink an item from a system path"),
		).
		Section("Vault",
			gohelp.Cmd("init, use", "Initialize or activate a vault").
				Example("dredge init /path/to/vault"),
			gohelp.Cmd("lock", "Lock the vault (clears cached session key)"),
			gohelp.Cmd("passwd", "Change vault password"),
		).
		Section("Sync",
			gohelp.Cmd("remote", "Wire a git remote to the active vault").
				Example("dredge remote owner/repo"),
			gohelp.Cmd("push", "Push changes to remote"),
			gohelp.Cmd("pull", "Pull changes from remote"),
			gohelp.Cmd("sync", "Sync with remote (pull + push)"),
			gohelp.Cmd("status", "Show pending changes"),
		).
		Section("Flags",
			gohelp.Cmd("--password, -p", "Password for decryption (skips prompt)"),
			gohelp.Cmd("--vault", "Vault directory for this command (does not persist)"),
			gohelp.Cmd("--luck, -l", "Force view the top search result"),
			gohelp.Cmd("--no-lock", "Disable session timeout for this command"),
		).
		Text("Tip: bare args route automatically — 'dredge ssh' searches, 'dredge 1' opens result #1.").
		Text("Run 'dredge help <topic>' for details. Topics: add, view, edit, link")

	addPage := gohelp.NewPage("add", "Add a new item to the vault").
		Usage("dredge add [title] [-c content] [-t tag...] [--file path]").
		Text("Without flags, opens your $EDITOR with a template. Fill in the title, tags, and content, then save and close to create the item.").
		Section("Flags",
			gohelp.Cmd("-c CONTENT", "Inline content — skips the editor entirely").
				Example("dredge add 'db password' -c 'hunter2'"),
			gohelp.Cmd("-t TAG...", "One or more tags").
				Example("dredge add 'ssh key' -t ssh config"),
			gohelp.Cmd("--file, --import PATH", "Import a file — text files are stored inline, binaries go to encrypted blob storage").
				Example("dredge add --file ~/.ssh/id_ed25519"),
		).
		Text("Tags can also be written inline in the title as #words. Any #word trailing the title is treated as a tag.").
		Section("Editor format",
			gohelp.Cmd("line 1", "Title and optional trailing #tags"),
			gohelp.Cmd("line 2", "(blank)"),
			gohelp.Cmd("line 3+", "Content"),
		).
		Text("Saving an empty buffer cancels the add.")

	viewPage := gohelp.NewPage("view", "View an item's content").
		Usage("dredge view <id|number> [--raw]").
		Text("Accepts an item ID, a numbered result from the last search, or a search query that resolves to a single match.").
		Section("Flags",
			gohelp.Cmd("--raw, -r", "Print content only — no header, no formatting. Useful for piping.").
				Example("dredge view abc --raw | pbcopy"),
		).
		Text("'dredge cat' is shorthand for 'dredge view --raw' and is pipe-friendly by default.")

	editPage := gohelp.NewPage("edit", "Edit an existing item").
		Usage("dredge edit <id|number> [--metadata]").
		Text("Opens the item in $EDITOR using the same template format as add: title and #tags on line 1, content from line 3 onward.").
		Section("Flags",
			gohelp.Cmd("--metadata, -m", "Edit metadata only (title, tags, type, filename, mode) as raw TOML — content is untouched.").
				Example("dredge edit abc --metadata"),
		).
		Text("Saving without changes leaves the item unmodified. The modified timestamp is only updated when content actually changes.")

	linkPage := gohelp.NewPage("link", "Link an item to a path on the filesystem").
		Usage("dredge link <id|number> [path] [--force] [-p]").
		Text("Creates a plain-text copy of the item in .spawned/ and symlinks it to the target path. Changes to the spawned file are synced back into the vault automatically on next read.").
		Text("If no path is given, defaults to the current directory using the item's original filename or ID.").
		Section("Flags",
			gohelp.Cmd("--force, -f", "Overwrite an existing file or symlink at the target path"),
			gohelp.Cmd("-p, --parents", "Create parent directories if they don't exist").
				Example("dredge link abc ~/.config/app/config.toml -p"),
		).
		Text("Only text items can be linked. Use 'dredge unlink <id>' to remove the symlink and spawned copy.")

	gohelp.Run(append([]string{"help"}, args...), root, addPage, viewPage, editPage, linkPage)
	return nil
}
