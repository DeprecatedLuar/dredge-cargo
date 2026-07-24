package git

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// outboundSyncHook is used only by package tests to create a deterministic
// concurrent remote update between rewrite verification and the leased push.
var outboundSyncHook func(string) error

func outboundSync(dir string) error {
	if err := reconcileRemote(dir); err != nil {
		return err
	}
	needed, err := RetentionNeeded(dir, time.Now())
	if err != nil {
		return fmt.Errorf("failed to check history retention: %w", err)
	}
	if !needed {
		return pushToRemote(dir)
	}

	branch, err := getCurrentBranch(dir)
	if err != nil || branch == "" {
		return fmt.Errorf("history retention requires an attached branch")
	}
	remoteRef := "refs/remotes/origin/" + branch
	expectedRemote, remoteErr := resolveRef(dir, remoteRef)

	result, err := RewriteHistory(dir, time.Now())
	if err != nil {
		return fmt.Errorf("failed to apply history retention: %w", err)
	}
	if !result.Changed {
		return pushToRemote(dir)
	}
	if outboundSyncHook != nil {
		if err := outboundSyncHook("before-push"); err != nil {
			return err
		}
	}

	if remoteErr != nil {
		if err := pushToRemote(dir); err != nil {
			return err
		}
	} else if err := pushWithLease(dir, branch, expectedRemote); err != nil {
		return fmt.Errorf(
			"retention push rejected safely because origin/%s changed after fetch; remote history was not overwritten and local recovery remains at %s: %w",
			branch, result.BackupRef, err,
		)
	}
	if err := verifyRemoteTip(dir, branch, result.NewHead); err != nil {
		return fmt.Errorf("retention push completed but remote verification failed; recovery remains at %s: %w", result.BackupRef, err)
	}
	if result.BackupRef != "" {
		if output, err := runGitCommand(dir, "update-ref", "-d", result.BackupRef); err != nil {
			return fmt.Errorf("retention push verified but backup cleanup failed for %s: %s", result.BackupRef, strings.TrimSpace(output))
		}
	}
	pruneRewrittenObjects(dir)
	return nil
}

// pruneRewrittenObjects releases only reflog entries whose commits are already
// unreachable from the repository's current refs, then asks Git to reclaim the
// resulting objects. It is deliberately best effort and is called only after
// the replacement tip has been verified remotely and its backup ref removed.
func pruneRewrittenObjects(dir string) {
	_, _ = runGitCommand(dir, "reflog", "expire", "--expire-unreachable=now", "--all")
	_, _ = runGitCommand(dir, "gc", "--prune=now")
}

func pushWithLease(dir, branch, expected string) error {
	lease := "--force-with-lease=refs/heads/" + branch + ":" + expected
	cmd := exec.Command("git", "push", "-u", lease, "origin", branch)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to push replacement history: %w", err)
	}
	return nil
}

func verifyRemoteTip(dir, branch, want string) error {
	output, err := runGitCommand(dir, "ls-remote", "--heads", "origin", "refs/heads/"+branch)
	if err != nil {
		return fmt.Errorf("failed to inspect remote: %s", strings.TrimSpace(output))
	}
	fields := strings.Fields(output)
	if len(fields) < 1 || fields[0] != want {
		return fmt.Errorf("origin/%s is not the verified replacement commit", branch)
	}
	return nil
}
