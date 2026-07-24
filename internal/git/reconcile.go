package git

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// reconcileRemote preserves synchronized working changes in a commit, fetches
// the current branch, and replays local-only commits when origin was rewritten.
// Recovery refs are retained when replay stops so neither side becomes
// unreachable.
func reconcileRemote(dir string) error {
	if !isGitRepo(dir) {
		return fmt.Errorf("not a git repository - run 'dredge remote <url>' to add a remote")
	}
	if _, ok := getRemoteURL(dir, "origin"); !ok {
		return fmt.Errorf("no git remote configured - run 'dredge remote <url>' to add a remote")
	}

	if isRebaseInProgress(dir) {
		if err := removeRedundantUntrackedConfig(dir); err != nil {
			return fmt.Errorf("failed to prepare interrupted reconciliation recovery: %w", err)
		}
		if output, err := runGitCommand(dir, "rebase", "--abort"); err != nil {
			return fmt.Errorf("failed to recover interrupted reconciliation: %s", strings.TrimSpace(output))
		}
	}
	if err := commitTrackedChanges(dir); err != nil {
		return err
	}

	branch, err := getCurrentBranch(dir)
	if err != nil || branch == "" {
		return fmt.Errorf("remote reconciliation requires an attached branch")
	}
	localHead, err := resolveRef(dir, "HEAD")
	if err != nil {
		return fmt.Errorf("failed to record local HEAD: %w", err)
	}
	remoteRef := "refs/remotes/origin/" + branch
	oldRemote, oldRemoteErr := resolveRef(dir, remoteRef)

	remoteHeads, err := runGitCommand(dir, "ls-remote", "--heads", "origin", "refs/heads/"+branch)
	if err != nil {
		return fmt.Errorf("failed to inspect remote: %s", strings.TrimSpace(remoteHeads))
	}
	if strings.TrimSpace(remoteHeads) == "" {
		// A new empty remote has nothing to reconcile; the outbound push will
		// establish the branch.
		return nil
	}
	if output, err := runGitCommand(dir, "fetch", "--prune", "origin", branch); err != nil {
		return fmt.Errorf("failed to fetch: %s", strings.TrimSpace(output))
	}
	newRemote, err := resolveRef(dir, remoteRef)
	if err != nil {
		return fmt.Errorf("remote branch origin/%s was not found after fetch", branch)
	}
	if localHead == newRemote {
		return nil
	}

	localBackup := reconcileRefNamespace + "/local/" + localHead
	if err := createRecoveryRef(dir, localBackup, localHead); err != nil {
		return err
	}
	backupRefs := []string{localBackup}
	rewritten := false
	if oldRemoteErr == nil {
		ancestor, ancestorErr := isAncestor(dir, oldRemote, newRemote)
		if ancestorErr != nil {
			return ancestorErr
		}
		rewritten = !ancestor
		if rewritten {
			remoteBackup := reconcileRefNamespace + "/remote/" + oldRemote
			if err := createRecoveryRef(dir, remoteBackup, oldRemote); err != nil {
				return err
			}
			backupRefs = append(backupRefs, remoteBackup)
		}
	}

	var output string
	if rewritten {
		subsumed, subsumedErr := dredgeChangesSubsumed(dir, oldRemote, localHead, newRemote)
		if subsumedErr != nil {
			return subsumedErr
		}
		if subsumed {
			output, err = runGitCommand(dir, "reset", "--hard", newRemote)
		} else {
			localOnly, err := commitCount(dir, oldRemote+".."+localHead)
			if err != nil {
				return err
			}
			if localOnly == 0 {
				output, err = runGitCommand(dir, "reset", "--hard", newRemote)
			} else {
				overlap, overlapErr := changedPathOverlap(dir, oldRemote, localHead, newRemote)
				if overlapErr != nil {
					return overlapErr
				}
				if len(overlap) > 0 {
					return fmt.Errorf(
						"remote reconciliation stopped because both histories changed %s; both states are recoverable at %s and %s",
						strings.Join(overlap, ", "), localBackup, remoteRef,
					)
				}
				// oldRemote is the exact boundary captured before fetch. Do not
				// let rebase.forkPoint expand that explicit commit range using
				// reflog history after a force-updated remote.
				output, err = runGitCommand(dir, "rebase", "--no-fork-point", "--onto", newRemote, oldRemote, branch)
			}
		}
	} else {
		output, err = runGitCommand(dir, "rebase", newRemote, branch)
	}
	if err != nil {
		return fmt.Errorf(
			"remote reconciliation stopped with conflicts; both states are recoverable at %s and %s:\n  cd %s\n  git status\n%s",
			localBackup, remoteRef, dir, strings.TrimSpace(output),
		)
	}
	for _, ref := range backupRefs {
		_, _ = runGitCommand(dir, "update-ref", "-d", ref)
	}
	return nil
}

// dredgeChangesSubsumed recognizes local changes already represented by a
// rewritten Dredge tree. Generated ignore rules may grow, but all other paths
// must exist at the same location with identical bytes.
func dredgeChangesSubsumed(dir, base, localHead, remoteHead string) (bool, error) {
	output, err := runGitCommand(dir, "diff", "--name-only", base, localHead)
	if err != nil {
		return false, fmt.Errorf("failed to inspect local paths: %s", strings.TrimSpace(output))
	}
	paths := strings.Fields(output)
	if len(paths) == 0 {
		return true, nil
	}
	for _, path := range paths {
		local, localErr := readBlob(dir, localHead, path)
		if localErr != nil {
			return false, nil
		}
		switch path {
		case gitIgnoreFileName:
			remote, remoteErr := readBlob(dir, remoteHead, path)
			if remoteErr != nil || !lineSetContains(remote, local) {
				return false, nil
			}
		default:
			remote, remoteErr := readBlob(dir, remoteHead, path)
			if remoteErr != nil || !bytes.Equal(local, remote) {
				return false, nil
			}
		}
	}
	return true, nil
}

func readBlob(dir, revision, path string) ([]byte, error) {
	output, err := runGitCommand(dir, "show", revision+":"+path)
	return []byte(output), err
}

func lineSetContains(superset, subset []byte) bool {
	lines := make(map[string]bool)
	for _, line := range strings.Split(string(superset), "\n") {
		lines[line] = true
	}
	for _, line := range strings.Split(string(subset), "\n") {
		if line != "" && !lines[line] {
			return false
		}
	}
	return true
}

// A failed replay can leave the legacy config untracked while ORIG_HEAD tracks
// the identical bytes. Removing only that provably redundant copy lets Git
// restore the original checkout without risking arbitrary untracked files.
func removeRedundantUntrackedConfig(dir string) error {
	output, err := runGitCommand(dir, "status", "--porcelain", "--", dredgeConfigPath)
	if err != nil || strings.TrimSpace(output) != "?? "+dredgeConfigPath {
		return nil
	}
	current, err := os.ReadFile(filepath.Join(dir, dredgeConfigPath))
	if err != nil {
		return err
	}
	original, err := readBlob(dir, "ORIG_HEAD", dredgeConfigPath)
	if err != nil || !bytes.Equal(current, original) {
		return nil
	}
	return os.Remove(filepath.Join(dir, dredgeConfigPath))
}

func commitTrackedChanges(dir string) error {
	if err := addTrackedFiles(dir); err != nil {
		return err
	}
	if _, err := runGitCommand(dir, "diff", "--cached", "--quiet"); err == nil {
		return nil
	}
	return commitChanges(dir)
}

func resolveRef(dir, ref string) (string, error) {
	output, err := runGitCommand(dir, "rev-parse", "--verify", ref)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(output), nil
}

func createRecoveryRef(dir, ref, target string) error {
	if output, err := runGitCommand(dir, "update-ref", ref, target); err != nil {
		return fmt.Errorf("failed to preserve %s: %s", target, strings.TrimSpace(output))
	}
	return nil
}

func isAncestor(dir, older, newer string) (bool, error) {
	_, err := runGitCommand(dir, "merge-base", "--is-ancestor", older, newer)
	if err == nil {
		return true, nil
	}
	// merge-base --is-ancestor uses status 1 for the ordinary "not ancestor"
	// result. Verify both commits separately so missing/corrupt refs are errors.
	if _, verifyErr := resolveRef(dir, older+"^{commit}"); verifyErr != nil {
		return false, fmt.Errorf("failed to inspect old remote tip: %w", verifyErr)
	}
	if _, verifyErr := resolveRef(dir, newer+"^{commit}"); verifyErr != nil {
		return false, fmt.Errorf("failed to inspect fetched remote tip: %w", verifyErr)
	}
	return false, nil
}

func commitCount(dir, revision string) (int, error) {
	output, err := runGitCommand(dir, "rev-list", "--count", revision)
	if err != nil {
		return 0, fmt.Errorf("failed to inspect local-only commits: %s", strings.TrimSpace(output))
	}
	var count int
	if _, err := fmt.Sscanf(strings.TrimSpace(output), "%d", &count); err != nil {
		return 0, fmt.Errorf("failed to parse local-only commit count")
	}
	return count, nil
}

func changedPathOverlap(dir, base, localHead, remoteHead string) ([]string, error) {
	localOutput, err := runGitCommand(dir, "diff", "--name-only", base, localHead)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect local paths: %s", strings.TrimSpace(localOutput))
	}
	remoteOutput, err := runGitCommand(dir, "diff", "--name-only", base, remoteHead)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect rewritten remote paths: %s", strings.TrimSpace(remoteOutput))
	}
	localPaths := make(map[string]bool)
	for _, path := range strings.Fields(localOutput) {
		localPaths[path] = true
	}
	var overlap []string
	for _, path := range strings.Fields(remoteOutput) {
		if localPaths[path] {
			overlap = append(overlap, path)
		}
	}
	return overlap, nil
}
