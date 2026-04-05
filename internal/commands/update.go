// Portable self-update handler — copy-paste across projects with no changes.
// Project-specific values live in main.go: const githubRepo = "user/repo" and var version = "dev".
// Call site: commands.HandleUpdate(version, githubRepo)
package commands

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	updateDevSentinel = "dev"
	satelliteURL      = "https://raw.githubusercontent.com/DeprecatedLuar/the-satellite/main/satellite.sh"
)

func HandleUpdate(currentVersion, repo string) error {
	if currentVersion == updateDevSentinel {
		fmt.Println("Development build — skipping update")
		return nil
	}

	parts := strings.SplitN(repo, "/", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid repo format %q, expected user/repo", repo)
	}
	repoUser, repoName := parts[0], parts[1]

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("could not locate executable: %w", err)
	}
	installDir := filepath.Dir(exe)
	binaryName := filepath.Base(exe)

	fmt.Printf("Current version: %s\n", currentVersion)
	fmt.Println("Checking for updates...")

	checkOut, err := exec.Command("bash", "-c",
		fmt.Sprintf("bash <(curl -sSL %s) check-update %s %s %s",
			satelliteURL, currentVersion, repoUser, repoName),
	).Output()
	if err != nil {
		return fmt.Errorf("failed to check for updates: %w", err)
	}

	latestVersion := strings.TrimSpace(string(checkOut))
	if latestVersion == "" {
		fmt.Println("Already up to date")
		return nil
	}

	fmt.Printf("Updating to %s...\n", latestVersion)

	buildCmd := fmt.Sprintf("go build -ldflags='-s -w' -o %s ./cmd/%s", binaryName, binaryName)
	installCmd := exec.Command("bash", "-c",
		fmt.Sprintf(`bash <(curl -sSL %s) install "%s" "%s" "%s" "%s" "%s" "%s"`,
			satelliteURL, repoName, binaryName, repoUser, repoName, installDir, buildCmd),
	)
	installCmd.Stdout = os.Stdout
	installCmd.Stderr = os.Stderr
	return installCmd.Run()
}
