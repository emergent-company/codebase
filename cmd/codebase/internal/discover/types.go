// Package discovercmd implements `codebase discover` — scans a local repo to
// detect project structure, applications, and patterns, then writes the results
// to .codebase.yml as the `apps` section.
//
// Pattern definitions are stored as embedded YAML so adding a new app type or
// detection rule is a data change, not a code change.
package discovercmd

import (
	"github.com/mkucharz/codebase/cmd/codebase/internal/config"
)

// DetectionMethod is how a detector checks for a pattern match.
type DetectionMethod string

const (
	MethodFileExists  DetectionMethod = "file_exists"
	MethodFileGlob    DetectionMethod = "file_glob"
	MethodImportCheck DetectionMethod = "import_search"
)

// DetectionRule defines a single detection criterion.
type DetectionRule struct {
	Method      DetectionMethod `yaml:"method"`
	Description string          `yaml:"description"`
	Params      RuleParams      `yaml:"params"`
}

// RuleParams holds the parameters for a detection rule.
type RuleParams struct {
	Patterns   []string `yaml:"patterns,omitempty"`
	Pattern    string   `yaml:"pattern,omitempty"`
	Imports    []string `yaml:"imports,omitempty"`
	Min        int      `yaml:"min,omitempty"`
	NoneMatch  bool     `yaml:"none_match,omitempty"`
	Exclude    []string `yaml:"exclude,omitempty"`
}

// ObservedPattern describes a structural pattern observed in this app type.
type ObservedPattern struct {
	Name        string `yaml:"name"`
	Label       string `yaml:"label"`
	Description string `yaml:"description"`
}

// PatternDef is a single pattern definition loaded from embedded YAML.
type PatternDef struct {
	Name          string            `yaml:"name"`
	Label         string            `yaml:"label"`
	AppType       string            `yaml:"app_type"`
	Detect        []DetectionRule   `yaml:"detect"`
	Observed      []ObservedPattern `yaml:"observed"`
	IgnoreChecks  []string          `yaml:"ignore_checks"`
	StarterRules  []string          `yaml:"starter_rules"`
}

// DetectedApp is the result of running detection against a directory.
type DetectedApp struct {
	Name         string            `yaml:"name"`
	RootPath     string            `yaml:"root_path"`
	AppType      string            `yaml:"app_type"`
	PatternNames []string          `yaml:"patterns"`
	PatternDefs  []ObservedPattern `yaml:"-"`
	MatchRules   []string          `yaml:"-"` // which detection rules matched
}

// DiscoverResult is the full output of a discover run.
type DiscoverResult struct {
	Apps []DetectedApp `yaml:"apps"`
}

// ScanResult is raw structural facts about a repo — no classification.
type ScanResult struct {
	Dir         string       `json:"dir"`
	EntryPoints []EntryPoint `json:"entry_points"`
	GoModules   []string     `json:"go_modules"`
	Dockerfiles []string     `json:"dockerfiles"`
	HasMakefile bool         `json:"has_makefile"`
	Subdirs     []string     `json:"subdirs"`
	GoFiles     int          `json:"go_files"`
}

// EntryPoint is a main package or CLI entry point with its imports.
type EntryPoint struct {
	Path       string   `json:"path"`
	Dir        string   `json:"dir"`
	Imports    []string `json:"imports"`
	HasMain    bool     `json:"has_main"`
	GoModDir   string   `json:"go_mod_dir"`
}

// AppsFile is the JSON/YAML structure for --apps-file.
type AppsFile struct {
	Apps []config.AppYML `yaml:"apps" json:"apps"`
}
