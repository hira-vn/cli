package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/hira-vn/cli/server/internal/cli"
)

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update hira to the latest version",
	RunE:  runUpdate,
}

func runUpdate(_ *cobra.Command, _ []string) error {
	fmt.Fprintf(os.Stderr, "Current version: %s (commit: %s, built: %s)\n", version, commit, date)

	// Refuse to overwrite a developer's locally-built binary. This binary's
	// version string comes from `git describe --tags --always --dirty`, so
	// anything that isn't a clean release tag means the user built from
	// source and an auto-update would silently roll back uncommitted work
	// (and any in-progress feature branches living alongside it).
	if cli.IsDevBuild(version) {
		fmt.Fprintf(os.Stderr,
			"\nDev build detected (%s). Auto-update is disabled to avoid overwriting your local build.\n"+
				"To refresh: run `make build` from the repo root.\n"+
				"\n"+
				"Phát hiện bản dev (%s). Auto-update đã bị tắt để tránh ghi đè binary build cục bộ.\n"+
				"Để rebuild: chạy `make build` ở thư mục gốc của repo.\n",
			version, version)
		return fmt.Errorf("auto-update disabled for dev builds")
	}

	// Check latest version from GitHub.
	latest, err := cli.FetchLatestRelease()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not check latest version: %v\n", err)
	} else {
		latestVer := strings.TrimPrefix(latest.TagName, "v")
		currentVer := strings.TrimPrefix(version, "v")
		if currentVer == latestVer {
			fmt.Fprintln(os.Stderr, "Already up to date.")
			return nil
		}
		fmt.Fprintf(os.Stderr, "Latest version:  %s\n\n", latest.TagName)
	}

	// Detect installation method and update accordingly.
	if cli.IsBrewInstall() {
		fmt.Fprintln(os.Stderr, "Updating via Homebrew...")
		output, err := cli.UpdateViaBrew()
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s\n", output)
			return fmt.Errorf("brew upgrade failed: %w\nYou can try manually: brew upgrade hira-vn/tap/cli", err)
		}
		fmt.Fprintln(os.Stderr, "Update complete.")
		return nil
	}

	// Not installed via brew — download binary directly from GitHub Releases.
	if latest == nil {
		return fmt.Errorf("could not determine latest version; check https://github.com/hira-vn/cli/releases/latest")
	}
	targetVersion := latest.TagName
	fmt.Fprintf(os.Stderr, "Downloading %s from GitHub Releases...\n", targetVersion)
	output, err := cli.UpdateViaDownload(targetVersion)
	if err != nil {
		return fmt.Errorf("update failed: %w", err)
	}
	fmt.Fprintf(os.Stderr, "%s\nUpdate complete.\n", output)
	return nil
}
