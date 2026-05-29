// Package upgradecmd implements the `codebase upgrade` command — self-updating
// the CLI binary from GitHub releases.
package upgradecmd

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
)

type Release struct {
	TagName string  `json:"tag_name"`
	Body    string  `json:"body"`
	Draft   bool    `json:"draft"`
	Assets  []Asset `json:"assets"`
}

type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

func NewCmd(version *string) *cobra.Command {
	var flagForce bool

	cmd := &cobra.Command{
		Use:   "upgrade",
		Short: "Upgrade the codebase CLI binary",
		Long: `Upgrades the codebase CLI binary to the latest release.

Downloads the latest binary from GitHub and replaces the current binary
in-place.

Examples:
  codebase upgrade            # Upgrade the CLI binary
  codebase upgrade --force    # Upgrade even when running a dev build`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUpgrade(*version, flagForce, cmd.OutOrStdout())
		},
	}

	cmd.Flags().BoolVarP(&flagForce, "force", "f", false, "Force upgrade even for dev versions")

	return cmd
}

func runUpgrade(currentVersion string, force bool, out io.Writer) error {
	fmt.Fprintln(out, "Checking for updates...")

	release, err := getLatestRelease()
	if err != nil {
		return fmt.Errorf("checking for updates: %w", err)
	}

	latestVersion := strings.TrimPrefix(release.TagName, "v")
	curVer := strings.TrimPrefix(currentVersion, "v")

	if currentVersion == "dev" && !force {
		fmt.Fprintln(out, "You are running a development version. Upgrade skipped.")
		fmt.Fprintf(out, "Latest release: %s\n", latestVersion)
		fmt.Fprintln(out, "Use --force to upgrade anyway.")
		return nil
	}

	if curVer == latestVersion && !force {
		fmt.Fprintf(out, "Already up to date: %s\n", curVer)
		return nil
	}

	fmt.Fprintln(out)
	fmt.Fprintf(out, "Current version: %s\n", curVer)
	fmt.Fprintf(out, "Latest version:  %s\n", latestVersion)
	fmt.Fprintln(out)

	if currentVersion == "dev" {
		fmt.Fprintf(out, "Will upgrade CLI from dev version to %s\n", latestVersion)
	} else {
		fmt.Fprintf(out, "Will upgrade CLI: %s → %s\n", curVer, latestVersion)
	}
	fmt.Fprintln(out)

	if !force {
		fmt.Fprint(out, "Proceed? [y/N]: ")
		var confirm string
		_, _ = fmt.Scanln(&confirm)
		if strings.ToLower(confirm) != "y" {
			fmt.Fprintln(out, "Upgrade canceled.")
			return nil
		}
	}

	assetURL, assetName, err := findAsset(release.Assets)
	if err != nil {
		return fmt.Errorf("finding compatible asset: %w", err)
	}

	fmt.Fprintf(out, "Downloading %s...\n", assetName)
	if _, err := installUpdate(assetURL, assetName); err != nil {
		return fmt.Errorf("CLI upgrade failed: %w", err)
	}

	fmt.Fprintln(out)
	fmt.Fprintf(out, "CLI upgraded to %s\n", latestVersion)

	// Show changelog between old and new version (best-effort)
	if changelog := changelogBetween(currentVersion, latestVersion, release.Body); changelog != "" {
		fmt.Fprint(out, changelog)
	}

	return nil
}

func getLatestRelease() (*Release, error) {
	resp, err := http.Get("https://api.github.com/repos/emergent-company/codebase/releases/latest")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned status: %d", resp.StatusCode)
	}

	var release Release
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, err
	}

	return &release, nil
}

func findAsset(assets []Asset) (string, string, error) {
	target := fmt.Sprintf("codebase-%s-%s.tar.gz", runtime.GOOS, runtime.GOARCH)

	for _, asset := range assets {
		if asset.Name == target {
			return asset.BrowserDownloadURL, asset.Name, nil
		}
	}

	return "", "", fmt.Errorf("no asset found for %s/%s", runtime.GOOS, runtime.GOARCH)
}

// installUpdate downloads and installs the new binary, replacing the current one.
func installUpdate(url, filename string) (string, error) {
	tmpFile, err := os.CreateTemp("", "codebase-update-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmpFile.Name())

	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if _, err := io.Copy(tmpFile, resp.Body); err != nil {
		return "", err
	}
	tmpFile.Close()

	binaryData, err := extractBinary(tmpFile.Name())
	if err != nil {
		return "", fmt.Errorf("extraction failed: %w", err)
	}

	// Capture the current executable path before replacing.
	currentExec, err := os.Executable()
	if err != nil {
		return "", err
	}
	currentExec, err = filepath.EvalSymlinks(currentExec)
	if err != nil {
		return "", err
	}

	newExecPath := currentExec + ".new"
	if err := os.WriteFile(newExecPath, binaryData, 0755); err != nil {
		return "", err
	}

	oldExecPath := currentExec + ".old"
	os.Remove(oldExecPath)

	if err := os.Rename(currentExec, oldExecPath); err != nil {
		return "", fmt.Errorf("failed to move current binary: %w", err)
	}

	if err := os.Rename(newExecPath, currentExec); err != nil {
		_ = os.Rename(oldExecPath, currentExec)
		return "", fmt.Errorf("failed to replace binary: %w", err)
	}

	_ = os.Remove(oldExecPath)

	return currentExec, nil
}

func extractBinary(tarPath string) ([]byte, error) {
	f, err := os.Open(tarPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	gzr, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}

		if header.Typeflag == tar.TypeReg {
			// Accept any file starting with "codebase" (goreleaser names the binary
			// after the platform, e.g. codebase-darwin-arm64).
			if strings.HasPrefix(header.Name, "codebase") && !strings.HasSuffix(header.Name, ".tar.gz") {
				return io.ReadAll(tr)
			}
		}
	}

	return nil, fmt.Errorf("codebase binary not found in archive")
}

// changelogBetween extracts relevant changelog sections from the release body.
func changelogBetween(currentVersion, latestVersion, body string) string {
	if body == "" || currentVersion == "dev" {
		return ""
	}
	// Truncate to keep it concise — return first 1KB of release notes
	const maxLen = 1024
	if len(body) > maxLen {
		body = body[:maxLen] + "\n..."
	}
	return "\n---\n" + body
}
