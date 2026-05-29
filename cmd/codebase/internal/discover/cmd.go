package discovercmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/mkucharz/codebase/cmd/codebase/internal/config"
)

// NewCmd creates the `codebase discover` command.
func NewCmd(flagProjectID *string) *cobra.Command {
	var rootDir string
	var write bool
	var scanMode bool
	var guideMode bool
	var autoMode bool
	var appsFile string

	cmd := &cobra.Command{
		Use:   "discover",
		Short: "Scan project structure and recommend app configuration",
		Long: `Analyze the local repo and provide structural facts that an LLM
or human uses to decide application types and write .codebase.yml.

Modes:
  discover                  Show scan facts + recommendations
  discover --scan           Output raw structural facts as JSON (for LLM consumption)
  discover --guide          Output structured graph population guide as JSON (schema, naming, workflow)
  discover --write          Write .codebase.yml from explicit app definitions
  discover --write --apps-file <file>  Write from a JSON/YAML app definitions file
  discover --auto           Legacy heuristic pattern detection (deprecated)
  discover --auto --write   Legacy heuristic detection + write (deprecated)

Examples:
  codebase discover                              # show facts + recommendations
  codebase discover --scan > /tmp/facts.json     # pipe facts to LLM
  codebase discover --guide > /tmp/guide.json    # get graph population guide
  codebase discover --write --apps-file my-apps.json  # write explicit apps
`,
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			_ = ctx

			// Determine repo root
			repoDir := rootDir
			if repoDir == "" {
				var err error
				repoDir, err = os.Getwd()
				if err != nil {
					return fmt.Errorf("getting working directory: %w", err)
				}
			}

			// --- Mode 1: --guide (LLM graph population guide) ---
			if guideMode {
				return runGuide(cmd)
			}

			// --- Mode 2: --scan (raw facts JSON) ---
			if scanMode {
				return runScan(cmd, repoDir)
			}

			// --- Mode 2: --write with --apps-file (explicit) ---
			if write && appsFile != "" {
				return runWriteFromFile(cmd, repoDir, appsFile)
			}

			// --- Mode 3: --auto (legacy heuristic detection) ---
			if autoMode {
				return runAuto(cmd, repoDir, write)
			}

			// --- Mode 4: --write without --apps-file (interactive guidance) ---
			if write {
				return runWritePrompt(cmd, repoDir)
			}

			// --- Default mode: show scan facts + recommendations ---
			return runDefault(cmd, repoDir)
		},
	}

	cmd.Flags().StringVar(&rootDir, "dir", "", "Repository root directory (default: current directory)")
	cmd.Flags().BoolVarP(&write, "write", "w", false, "Write apps section to .codebase.yml")
	cmd.Flags().BoolVar(&scanMode, "scan", false, "Output raw structural facts as JSON (no classification)")
	cmd.Flags().BoolVar(&guideMode, "guide", false, "Output LLM graph population guide as JSON (schema, naming, workflow)")
	cmd.Flags().BoolVar(&autoMode, "auto", false, "Use legacy heuristic pattern detection (deprecated)")
	cmd.Flags().StringVar(&appsFile, "apps-file", "", "Path to JSON/YAML file with app definitions (used with --write)")

	return cmd
}

// runGuide outputs the structured LLM graph population guide as JSON.
func runGuide(cmd *cobra.Command) error {
	guide := GetGraphGuide()
	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent("", "  ")
	return enc.Encode(guide)
}

// runScan outputs raw structural facts as JSON (LLM-consumable).
func runScan(cmd *cobra.Command, repoDir string) error {
	result, err := ScanDir(repoDir)
	if err != nil {
		return fmt.Errorf("scanning directory: %w", err)
	}

	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent("", "  ")
	if err := enc.Encode(result); err != nil {
		return fmt.Errorf("encoding result: %w", err)
	}
	return nil
}

// runWriteFromFile writes .codebase.yml from an explicit app definitions file.
func runWriteFromFile(cmd *cobra.Command, repoDir, appsFilePath string) error {
	appsFile, err := parseAppsFile(appsFilePath)
	if err != nil {
		return fmt.Errorf("parsing apps file %q: %w", appsFilePath, err)
	}

	ymlPath := filepath.Join(repoDir, ".codebase.yml")
	if err := writeAppsYML(ymlPath, appsFile.Apps); err != nil {
		return fmt.Errorf("writing .codebase.yml: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Wrote %d app(s) to %s\n", len(appsFile.Apps), ymlPath)
	for _, app := range appsFile.Apps {
		fmt.Fprintf(cmd.OutOrStdout(), "  %s (%s) → %s [%s]\n", app.Key, app.Name, app.RootPath, app.AppType)
	}
	return nil
}

// parseAppsFile reads a JSON or YAML file with app definitions.
func parseAppsFile(path string) (*AppsFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var apps AppsFile

	// Try JSON first
	if err := json.Unmarshal(data, &apps); err == nil && len(apps.Apps) > 0 {
		return &apps, nil
	}

	// Try YAML
	if err := yaml.Unmarshal(data, &apps); err != nil {
		return nil, fmt.Errorf("file is neither valid JSON nor YAML: %w", err)
	}
	if len(apps.Apps) == 0 {
		return nil, fmt.Errorf("apps file must contain at least one app definition")
	}

	return &apps, nil
}

// writeAppsYML writes the apps section into .codebase.yml, preserving other fields.
func writeAppsYML(path string, apps []config.AppYML) error {
	var yml config.CodebaseYML
	if data, err := os.ReadFile(path); err == nil {
		if err := yaml.Unmarshal(data, &yml); err != nil {
			yml = config.CodebaseYML{}
		}
	}

	yml.Apps = apps

	data, err := yaml.Marshal(&yml)
	if err != nil {
		return fmt.Errorf("marshaling yaml: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("writing file: %w", err)
	}
	return nil
}

// runWritePrompt shows scan facts and guides the user on what to set.
func runWritePrompt(cmd *cobra.Command, repoDir string) error {
	result, err := ScanDir(repoDir)
	if err != nil {
		return fmt.Errorf("scanning directory: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Use --apps-file to write explicit app definitions.\n\n")
	fmt.Fprintf(cmd.OutOrStdout(), "Create a JSON or YAML file, e.g. /tmp/apps.json:\n\n")
	fmt.Fprintf(cmd.OutOrStdout(), `{
  "apps": [
    {"key": "api-server", "name": "API Server", "root_path": "%s", "app_type": "backend", "patterns": ["main_as_entry_point"]}
  ]
}
`, guessRootDir(result))

	fmt.Fprintf(cmd.OutOrStdout(), "\nThen run: codebase discover --write --apps-file /tmp/apps.json\n")
	fmt.Fprintf(cmd.OutOrStdout(), "\n---\n")
	printScanSummary(cmd, result)
	return nil
}

// runDefault shows scan facts and recommendations.
func runDefault(cmd *cobra.Command, repoDir string) error {
	result, err := ScanDir(repoDir)
	if err != nil {
		return fmt.Errorf("scanning directory: %w", err)
	}

	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Project: %s\n\n", result.Dir)

	printScanSummary(cmd, result)
	printRecommendations(cmd, result)
	return nil
}

// runAuto is the legacy heuristic pattern detection.
func runAuto(cmd *cobra.Command, repoDir string, write bool) error {
	patterns, err := LoadPatternDefs()
	if err != nil {
		return fmt.Errorf("loading pattern definitions: %w", err)
	}
	if len(patterns) == 0 {
		return fmt.Errorf("no pattern definitions found (embedded YAML may be missing)")
	}

	apps, err := DetectApps(repoDir, patterns)
	if err != nil {
		return fmt.Errorf("detection failed: %w", err)
	}

	if len(apps) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No known application patterns detected. Use `codebase discover` (without --auto) for scan-based recommendations.")
		return nil
	}

	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Legacy heuristic detection found %d app(s):\n\n", len(apps))
	for _, app := range apps {
		fmt.Fprintf(out, "  %s\n", app.Name)
		fmt.Fprintf(out, "    Type:     %s\n", app.AppType)
		fmt.Fprintf(out, "    Root:     %s\n", app.RootPath)
		fmt.Fprintf(out, "    Patterns:\n")
		for _, p := range app.PatternDefs {
			fmt.Fprintf(out, "      - %s (%s)\n", p.Label, p.Name)
		}
		fmt.Fprintf(out, "\n")
	}

	if write {
		ymlPath := filepath.Join(repoDir, ".codebase.yml")
		ymApps := make([]config.AppYML, 0, len(apps))
		for _, app := range apps {
			ymApps = append(ymApps, config.AppYML{
				Key:      config.MakeAppKey(app.Name),
				Name:     app.Name,
				RootPath: app.RootPath,
				AppType:  app.AppType,
				Patterns: app.PatternNames,
			})
		}
		if err := writeAppsYML(ymlPath, ymApps); err != nil {
			return fmt.Errorf("writing .codebase.yml: %w", err)
		}
		fmt.Fprintf(out, "Wrote apps section to %s (legacy heuristic mode)\n", ymlPath)
	}

	fmt.Fprintf(out, "\n⚠️  --auto is deprecated. Use `codebase discover --scan` for LLM-driven classification instead.\n")
	return nil
}

// --- Helpers ---

// printScanSummary prints a compact overview of scan results.
func printScanSummary(cmd *cobra.Command, result *ScanResult) {
	out := cmd.OutOrStdout()

	fmt.Fprintf(out, "Go files:     %d\n", result.GoFiles)
	fmt.Fprintf(out, "Go modules:   %s\n", joinOrNone(result.GoModules))
	fmt.Fprintf(out, "Entry points: %d\n", len(result.EntryPoints))
	fmt.Fprintf(out, "Dockerfiles:  %s\n", joinOrNone(result.Dockerfiles))
	fmt.Fprintf(out, "Makefile:     %v\n", result.HasMakefile)
	fmt.Fprintf(out, "Subdirs:      %s\n", strings.Join(result.Subdirs, ", "))
	fmt.Fprintf(out, "\n")

	if len(result.EntryPoints) > 0 {
		fmt.Fprintf(out, "Entry points:\n")
		for _, ep := range result.EntryPoints {
			fmt.Fprintf(out, "  %s", ep.Path)
			if ep.GoModDir != "." {
				fmt.Fprintf(out, " (module: %s)", ep.GoModDir)
			}
			fmt.Fprintf(out, "\n")
			for _, imp := range ep.Imports {
				fmt.Fprintf(out, "    ← %s\n", imp)
			}
		}
		fmt.Fprintf(out, "\n")
	}
}

// printRecommendations shows hints for the LLM/user.
func printRecommendations(cmd *cobra.Command, result *ScanResult) {
	out := cmd.OutOrStdout()

	fmt.Fprintf(out, "Recommendations:\n\n")

	if len(result.EntryPoints) == 0 {
		fmt.Fprintf(out, "  No Go entry points found. This may be a library or non-Go project.\n")
		fmt.Fprintf(out, "  Set app_type: library in your .codebase.yml apps section.\n")
		fmt.Fprintf(out, "\n")
		return
	}

	for _, ep := range result.EntryPoints {
		hasBackend := containsImport(ep.Imports, "echo", "gin", "chi", "net/http", "gorilla", "database/sql", "internal/server", "domain/")
		hasCLI := containsImport(ep.Imports, "cobra", "/cli")
		hasFramework := hasBackend || hasCLI

		if !hasFramework {
			fmt.Fprintf(out, "  %s — unknown structure (no detected frameworks)\n", ep.Path)
			fmt.Fprintf(out, "    Suggested app_type: library or check imports manually\n")
		} else if hasBackend && hasCLI {
			fmt.Fprintf(out, "  %s — CLI + HTTP framework detected\n", ep.Path)
			fmt.Fprintf(out, "    Suggested app_type: backend (higher priority than cli)\n")
		} else if hasBackend {
			fmt.Fprintf(out, "  %s — uses %s\n", ep.Path, firstFew(ep.Imports, 3))
			fmt.Fprintf(out, "    Suggested app_type: backend\n")
		} else if hasCLI {
			fmt.Fprintf(out, "  %s — uses %s\n", ep.Path, firstFew(ep.Imports, 3))
			fmt.Fprintf(out, "    Suggested app_type: cli\n")
		}

		suggestedKey := config.MakeAppKey(ep.Dir)
		if suggestedKey == "." || suggestedKey == "" {
			suggestedKey = "app"
		}
		fmt.Fprintf(out, "    Suggested key:    %s\n", suggestedKey)
		fmt.Fprintf(out, "    Root path:        %s\n", ep.Dir)
		fmt.Fprintf(out, "\n")
	}

	fmt.Fprintf(out, "To write these as apps to .codebase.yml:\n\n")
	fmt.Fprintf(out, "  1. Create a JSON file with your chosen apps (e.g. /tmp/apps.json)\n")
	fmt.Fprintf(out, "  2. Run: codebase discover --write --apps-file /tmp/apps.json\n")
	fmt.Fprintf(out, "\n")
	fmt.Fprintf(out, "Or run: codebase discover --scan > /tmp/facts.json\n")
	fmt.Fprintf(out, "  Then let an LLM interpret the facts and generate the apps file.\n")
	fmt.Fprintf(out, "\n")
}

// guessRootDir picks the most likely root path for a single-app project.
func guessRootDir(result *ScanResult) string {
	if len(result.EntryPoints) == 1 {
		return result.EntryPoints[0].Dir
	}
	for _, ep := range result.EntryPoints {
		if ep.Dir == "." {
			return "."
		}
	}
	if len(result.EntryPoints) > 0 {
		return result.EntryPoints[0].Dir
	}
	return "."
}

// containsImport checks if any of the given imports contain one of the prefixes.
func containsImport(imports []string, prefixes ...string) bool {
	for _, imp := range imports {
		impLower := strings.ToLower(imp)
		for _, prefix := range prefixes {
			if strings.Contains(impLower, prefix) {
				return true
			}
		}
	}
	return false
}

// firstFew returns a comma-separated string of the first n imports.
func firstFew(items []string, n int) string {
	if len(items) == 0 {
		return "(none)"
	}
	end := n
	if end > len(items) {
		end = len(items)
	}
	return strings.Join(items[:end], ", ")
}

// joinOrNone joins strings or returns "(none)".
func joinOrNone(items []string) string {
	if len(items) == 0 {
		return "(none)"
	}
	return strings.Join(items, ", ")
}
