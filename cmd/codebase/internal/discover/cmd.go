package discovercmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/mkucharz/codebase/cmd/codebase/internal/config"
)

// NewCmd creates the `codebase discover` command.
func NewCmd(flagProjectID *string) *cobra.Command {
	var rootDir string
	var write bool

	cmd := &cobra.Command{
		Use:   "discover",
		Short: "Discover project structure, apps, and patterns",
		Long: `Scan the local repo to detect applications and structural patterns,
then write the results to .codebase.yml.

Patterns are defined in embedded YAML files and detect:
  - Application types (CLI, backend, library, frontend)
  - Structural patterns (cobra commands, service endpoints)
  - What checks and constitution rules apply

Examples:
  codebase discover                              # preview detected apps
  codebase discover --write                      # write to .codebase.yml
  codebase discover --dir /path/to/repo --write  # scan a different directory
`,
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			_ = ctx

			// Load pattern definitions from embedded YAML
			patterns, err := LoadPatternDefs()
			if err != nil {
				return fmt.Errorf("loading pattern definitions: %w", err)
			}
			if len(patterns) == 0 {
				return fmt.Errorf("no pattern definitions found (embeded YAML may be missing)")
			}

			// Determine repo root
			repoDir := rootDir
			if repoDir == "" {
				repoDir, err = os.Getwd()
				if err != nil {
					return fmt.Errorf("getting working directory: %w", err)
				}
			}

			// Run detection
			apps, err := DetectApps(repoDir, patterns)
			if err != nil {
				return fmt.Errorf("detection failed: %w", err)
			}

			if len(apps) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No known application patterns detected.")
				return nil
			}

			// Print summary
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Detected %d app(s):\n\n", len(apps))
			for _, app := range apps {
				fmt.Fprintf(out, "  %s\n", app.Name)
				fmt.Fprintf(out, "    Type:     %s\n", app.AppType)
				fmt.Fprintf(out, "    Patterns:\n")
				for _, p := range app.PatternDefs {
					fmt.Fprintf(out, "      - %s (%s)\n", p.Label, p.Name)
				}
				fmt.Fprintf(out, "\n")
			}

			// Write to .codebase.yml if --write is set
			if write {
				ymlPath := filepath.Join(repoDir, ".codebase.yml")
				if err := writeDiscoverResult(ymlPath, &apps); err != nil {
					return fmt.Errorf("writing .codebase.yml: %w", err)
				}
				fmt.Fprintf(out, "Wrote apps section to %s\n", ymlPath)
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&rootDir, "dir", "", "Repository root directory (default: current directory)")
	cmd.Flags().BoolVarP(&write, "write", "w", false, "Write results to .codebase.yml")

	return cmd
}

// writeDiscoverResult merges the detected apps into .codebase.yml.
func writeDiscoverResult(path string, apps *[]DetectedApp) error {
	// Read existing .codebase.yml
	var yml config.CodebaseYML
	if data, err := os.ReadFile(path); err == nil {
		if err := yaml.Unmarshal(data, &yml); err != nil {
			yml = config.CodebaseYML{}
		}
	}

	// Build app configs from detected apps
	yml.Apps = make([]config.AppYML, 0, len(*apps))
	for _, app := range *apps {
		yml.Apps = append(yml.Apps, config.AppYML{
			Name:     app.Name,
			RootPath: app.RootPath,
			AppType:  app.AppType,
			Patterns: app.PatternNames,
		})
	}

	data, err := yaml.Marshal(&yml)
	if err != nil {
		return fmt.Errorf("marshaling yaml: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("writing file: %w", err)
	}

	return nil
}
