// Package config handles .codebase.yml parsing and SDK client construction.
package config

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	sdk "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk"
	sdkgraph "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
	sdkregistry "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/schemaregistry"
	"gopkg.in/yaml.v3"
)

// SyncRoutesConfig configures the route extractor for `codebase sync routes`.
type SyncRoutesConfig struct {
	// Framework selects a bundled extractor (e.g. "nestjs", "express", "fastapi").
	// Mutually exclusive with Command.
	Framework string `yaml:"framework"`
	// Command is a custom extractor script path (relative to repo root).
	// The CLI runs it and reads newline-delimited JSON from stdout.
	Command string `yaml:"command"`
	// Runtime is the interpreter for Command (e.g. "npx ts-node", "python3").
	// Defaults to direct execution if empty.
	Runtime string `yaml:"runtime"`
	// Glob is passed to the extractor as --glob (overrides extractor default).
	Glob string `yaml:"glob"`
	// DomainSegment is the 0-based path segment index used to derive domain name.
	// e.g. "apps/api/src/<domain>/foo.controller.ts" → segment 3
	DomainSegment int `yaml:"domain_segment"`
}

// SyncViewsConfig configures `codebase sync views` — React/frontend view extraction.
type SyncViewsConfig struct {
	// Glob pattern for view files relative to repo root. E.g. "apps/web/src/views/**/*.tsx"
	Glob string `yaml:"glob"`
	// RoutesFile is the path to the routes definition file (TypeScript/JS).
	// The CLI parses string literals from it to map view names to routes.
	RoutesFile string `yaml:"routes_file"`
	// Platform overrides the default platform tag. Defaults to ["web"].
	Platform []string `yaml:"platform"`
}

// SyncComponentsConfig configures `codebase sync components` — UI component extraction.
type SyncComponentsConfig struct {
	// Glob pattern for component files relative to repo root. E.g. "libs/shared-web/src/components/**/*.tsx"
	Glob string `yaml:"glob"`
}

// SyncActionsConfig configures `codebase sync actions` — store/action extraction.
type SyncActionsConfig struct {
	// Glob pattern for store files relative to repo root. E.g. "apps/web/src/store/**/*.ts"
	Glob string `yaml:"glob"`
	// Pattern selects the store framework. E.g. "mobx", "redux", "zustand". Defaults to "mobx".
	Pattern string `yaml:"pattern"`
}

// SyncScenariosConfig configures `codebase sync scenarios`.
type SyncScenariosConfig struct {
	// File is the path to the scenarios definition YAML (relative to repo root).
	// E.g. ".codebase/scenarios.yml"
	File string `yaml:"file"`
	// RouterFile is the path to the main React Router file for --discover mode.
	// E.g. "apps/web/src/App.tsx"
	RouterFile string `yaml:"router_file"`
	// StoreGlob is the glob for store files used in --discover mode.
	// E.g. "apps/web/src/store/**/*.ts"
	StoreGlob string `yaml:"store_glob"`
}

// SyncConfig groups all sync sub-command configuration.
type SyncConfig struct {
	Routes     SyncRoutesConfig     `yaml:"routes"`
	Views      SyncViewsConfig      `yaml:"views"`
	Components SyncComponentsConfig `yaml:"components"`
	Actions    SyncActionsConfig    `yaml:"actions"`
	Scenarios  SyncScenariosConfig  `yaml:"scenarios"`
}

// CodebaseYML is the structure of .codebase.yml.
type CodebaseYML struct {
	Project   string     `yaml:"project"`    // project name (resolved to ID via API)
	ProjectID string     `yaml:"project_id"` // explicit project ID (skips name lookup)
	Server    string     `yaml:"server"`     // optional server URL override
	APIKey    string     `yaml:"api_key"`    // optional API key (set as MEMORY_API_KEY)
	Sync      SyncConfig `yaml:"sync"`       // sync sub-command configuration
	Apps      []AppYML   `yaml:"apps,omitempty"` // discovered applications (written by `codebase discover`)
}

// AppYML represents a detected application in .codebase.yml.
type AppYML struct {
	Name         string   `yaml:"name"`
	RootPath     string   `yaml:"root_path"`
	AppType      string   `yaml:"app_type"`
	Patterns     []string `yaml:"patterns,omitempty"`
}

// AppConfig returns the full app config for a given app name from .codebase.yml.
// Returns nil if no matching app is found.
func FindApp(name string) *AppYML {
	yml := LoadYML()
	if yml == nil {
		return nil
	}
	for _, app := range yml.Apps {
		if app.Name == name {
			return &app
		}
	}
	return nil
}

// AppTypes returns the set of unique app_types in .codebase.yml.
func AppTypes() []string {
	yml := LoadYML()
	if yml == nil || len(yml.Apps) == 0 {
		return nil
	}
	seen := make(map[string]bool)
	var types []string
	for _, app := range yml.Apps {
		if app.AppType != "" && !seen[app.AppType] {
			seen[app.AppType] = true
			types = append(types, app.AppType)
		}
	}
	return types
}

// HasAppType checks if a specific app_type exists in .codebase.yml.
func HasAppType(appType string) bool {
	for _, t := range AppTypes() {
		if t == appType {
			return true
		}
	}
	return false
}

// IgnoredChecks returns the set of check names to suppress.
// Only suppresses checks when there are NO backend apps in the project.
// A monorepo with both backend + cli apps keeps all checks active.
func IgnoredChecks() map[string]bool {
	yml := LoadYML()
	if yml == nil {
		return nil
	}
	hasBackend := false
	for _, app := range yml.Apps {
		switch app.AppType {
		case "backend":
			hasBackend = true
		case "cli", "library":
			// non-backend type present
		default:
			// unknown type — treat conservatively
		}
	}
	// If backend is present, don't suppress anything
	if hasBackend || len(yml.Apps) == 0 {
		return nil
	}
	ignored := make(map[string]bool)
	for _, app := range yml.Apps {
		switch app.AppType {
		case "cli":
			ignored["DOMAIN_NO_ENDPOINTS"] = true
		case "library":
			ignored["DOMAIN_NO_ENDPOINTS"] = true
		}
	}
	if len(ignored) == 0 {
		return nil
	}
	return ignored
}

// Client wraps the SDK client with resolved project context.
type Client struct {
	SDK            *sdk.Client
	Graph          *sdkgraph.Client
	SchemaRegistry *sdkregistry.Client
	ProjectID      string
	ProjectName    string // resolved project name (may be empty if only ID available)
	Branch         string
}

// loadEnvFiles walks up from cwd to find .env then .env.local, parses them,
// and returns the merged config (.env base, .env.local overrides).
// Actual process env vars still take highest priority — callers should
// check os.Getenv first before falling back to these values.
func loadEnvFiles() (apiKey, projectID string) {
	merged := make(map[string]string)
	// .env first (base config)
	if p := walkUpFind(".env"); p != "" {
		if raw, err := parseDotenv(p); err == nil {
			for k, v := range raw {
				merged[k] = v
			}
		}
	}
	// .env.local overrides .env
	if p := walkUpFind(".env.local"); p != "" {
		if raw, err := parseDotenv(p); err == nil {
			for k, v := range raw {
				merged[k] = v
			}
		}
	}
	return merged["CODEBASE_API_KEY"], merged["CODEBASE_PROJECT_ID"]
}

// walkUpFind searches for filename starting from cwd, walking up to root.
func walkUpFind(filename string) string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	for {
		candidate := filepath.Join(dir, filename)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// parseDotenv parses a simple KEY=VALUE file, stripping surrounding quotes.
func parseDotenv(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	result := make(map[string]string)
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || !strings.Contains(line, "=") {
			continue
		}
		idx := strings.Index(line, "=")
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		if len(val) >= 2 && ((val[0] == '"' && val[len(val)-1] == '"') || (val[0] == '\'' && val[len(val)-1] == '\'')) {
			val = val[1 : len(val)-1]
		}
		result[key] = val
	}
	return result, nil
}

// New creates a configured Client. flagProjectID overrides all other sources.
func New(flagProjectID, flagBranch string) (*Client, error) {
	// Load .env → .env.local (in that order, so .env.local overrides)
	envAK, envPID := loadEnvFiles()

	yml, _ := findAndParseYML()

	// Auth priority: actual env var > .env.local > .env > .codebase.yml > ~/.memory/config.yaml.
	if ak := os.Getenv("CODEBASE_API_KEY"); ak != "" {
		os.Setenv("MEMORY_API_KEY", ak)
	} else if envAK != "" {
		os.Setenv("MEMORY_API_KEY", envAK)
	} else if yml != nil && yml.APIKey != "" {
		if err := os.Setenv("MEMORY_API_KEY", yml.APIKey); err != nil {
			return nil, fmt.Errorf("exporting api_key from .codebase.yml: %w", err)
		}
	}

	sdkClient, err := sdk.NewFromEnv()
	if err != nil {
		if yml != nil && yml.Server != "" {
			os.Setenv("MEMORY_SERVER_URL", yml.Server)
			sdkClient, err = sdk.NewFromEnv()
		}
		if err != nil {
			return nil, fmt.Errorf("memory auth: %w", err)
		}
	}

	// Project ID priority: --flag > actual env var > .env.local > .env > .codebase.yml.
	projectID := flagProjectID
	var projectName string
	if projectID == "" {
		projectID = os.Getenv("CODEBASE_PROJECT_ID")
	}
	if projectID == "" {
		projectID = envPID
	}

	if projectID == "" {
		if yml != nil {
			if yml.ProjectID != "" {
				projectID = yml.ProjectID
			} else if yml.Project != "" {
				if looksLikeUUID(yml.Project) {
					// project: value is a UUID — use as ID directly
					projectID = yml.Project
				} else {
					// Treat as a human-readable project name — resolve to ID
					projectName = yml.Project
					id, err := resolveProjectName(sdkClient, yml.Project)
					if err != nil {
						return nil, err
					}
					projectID = id
				}
			}
		}
	}

	// Capture display name from .codebase.yml even when ID came from env var.
	if projectName == "" && yml != nil {
		if yml.Project != "" && !looksLikeUUID(yml.Project) {
			projectName = yml.Project
		}
	}

	if projectID == "" {
		return nil, fmt.Errorf("project not found")
	}

	sdkClient.SetContext("", projectID)

	return &Client{
		SDK:            sdkClient,
		Graph:          sdkClient.Graph,
		SchemaRegistry: sdkClient.SchemaRegistry,
		ProjectID:      projectID,
		ProjectName:    projectName,
		Branch:         flagBranch,
	}, nil
}

// LoadYML returns the parsed .codebase.yml walked up from cwd, or nil if not found.
func LoadYML() *CodebaseYML {
	yml, _ := findAndParseYML()
	return yml
}

func findAndParseYML() (*CodebaseYML, error) {
	dir, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	for {
		candidate := filepath.Join(dir, ".codebase.yml")
		if data, err := os.ReadFile(candidate); err == nil {
			var yml CodebaseYML
			if err := yaml.Unmarshal(data, &yml); err != nil {
				return nil, err
			}
			return &yml, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return nil, os.ErrNotExist
}

func resolveProjectName(client *sdk.Client, name string) (string, error) {
	ctx := context.Background()
	projects, err := client.Projects.List(ctx, nil)
	if err != nil {
		return "", err
	}
	nameLower := strings.ToLower(name)
	for _, p := range projects {
		if strings.ToLower(p.Name) == nameLower {
			return p.ID, nil
		}
	}
	return "", fmt.Errorf("no project named %q found", name)
}

// looksLikeUUID checks if a string is a UUID (8-4-4-4-12 hex format).
func looksLikeUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, c := range s {
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
				return false
			}
		}
	}
	return true
}
