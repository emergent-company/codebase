package discovercmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DetectApps runs all pattern definitions against the given repo directory
// and returns the apps that were detected.
func DetectApps(repoDir string, patterns []PatternDef) ([]DetectedApp, error) {
	var apps []DetectedApp

	for _, def := range patterns {
		matched, err := runDetection(repoDir, def)
		if err != nil {
			continue // skip patterns that error
		}
		if matched {
			app := DetectedApp{
				Name:         def.Label,
				RootPath:     ".",
				AppType:      def.AppType,
				PatternNames: patternNames(def.Observed),
				PatternDefs:  def.Observed,
				MatchRules:   nil,
			}
			apps = append(apps, app)
		}
	}

	// Deduplicate by app_type — if multiple patterns match the same type,
	// merge them (prefer the most specific match).
	apps = deduplicateApps(apps)

	return apps, nil
}

// runDetection checks if a single pattern definition matches the repo.
func runDetection(repoDir string, def PatternDef) (bool, error) {
	totalRules := len(def.Detect)
	if totalRules == 0 {
		return false, nil
	}

	matches := 0
	for _, rule := range def.Detect {
		ok, err := checkRule(repoDir, rule)
		if err != nil {
			return false, err
		}
		if ok {
			matches++
		}
	}

	// Require at least half the rules to match (soft consensus)
	return matches >= (totalRules+1)/2, nil
}

// checkRule runs a single detection rule.
func checkRule(repoDir string, rule DetectionRule) (bool, error) {
	switch rule.Method {
	case MethodFileExists:
		return checkFileExists(repoDir, rule.Params)
	case MethodFileGlob:
		return checkFileGlob(repoDir, rule.Params)
	case MethodImportCheck:
		return checkImportSearch(repoDir, rule.Params)
	default:
		return false, fmt.Errorf("unknown detection method: %s", rule.Method)
	}
}

// checkFileExists checks if a single file exists.
func checkFileExists(repoDir string, params RuleParams) (bool, error) {
	path := filepath.Join(repoDir, params.Pattern)
	info, err := os.Stat(path)
	if err != nil {
		return false, nil
	}
	return !info.IsDir(), nil
}

// checkFileGlob checks if any files matching the glob patterns exist.
// Supports exclude patterns, recursive walking (patterns starting with **/),
// and min match threshold.
func checkFileGlob(repoDir string, params RuleParams) (bool, error) {
	if len(params.Patterns) == 0 {
		return false, nil
	}

	// Build exclusion set
	exclude := make(map[string]bool)
	for _, e := range params.Exclude {
		exclude[e] = true
	}

	matches := 0
	for _, pattern := range params.Patterns {
		if strings.HasPrefix(pattern, "**/") {
			// Recursive glob — walk the tree and match filenames
			suffix := pattern[3:] // strip "**/"
			filepath.Walk(repoDir, func(path string, info os.FileInfo, err error) error {
				if err != nil || info.IsDir() {
					return nil
				}
				rel, err := filepath.Rel(repoDir, path)
				if err != nil {
					return nil
				}
				if strings.Contains(filepath.ToSlash(rel), "/vendor/") {
					return nil
				}
				if matched, _ := filepath.Match(suffix, info.Name()); matched {
					base := filepath.Base(path)
					if exclude[base] || exclude[filepath.ToSlash(path)] {
						return nil
					}
					matches++
				}
				return nil
			})
		} else {
			fullPattern := filepath.Join(repoDir, pattern)
			globMatches, err := filepath.Glob(fullPattern)
			if err != nil {
				continue
			}
			for _, m := range globMatches {
				base := filepath.Base(m)
				if exclude[base] || exclude[filepath.ToSlash(m)] {
					continue
				}
				matches++
			}
		}
	}

	min := params.Min
	if min <= 0 {
		min = 1
	}

	if params.NoneMatch {
		return matches == 0, nil
	}
	return matches >= min, nil
}

// checkImportSearch greps Go source files for import statements matching
// any of the given import paths.
func checkImportSearch(repoDir string, params RuleParams) (bool, error) {
	if len(params.Imports) == 0 {
		return false, nil
	}

	// Build grep-like patterns by scanning .go files for import blocks
	importHits := 0
	seen := make(map[string]bool)

	err := filepath.Walk(repoDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		if strings.Contains(filepath.ToSlash(path), "/vendor/") {
			return nil
		}

		f, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer f.Close()

		inImport := false
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())

			if line == "import (" {
				inImport = true
				continue
			}
			if inImport && line == ")" {
				inImport = false
				continue
			}
			if inImport {
				// Strip quotes
				imp := strings.Trim(line, "\" ")
				if imp == "" || strings.HasPrefix(imp, "_") || strings.HasPrefix(imp, ".") {
					continue
				}
				for _, target := range params.Imports {
					if imp == target || strings.HasPrefix(imp, target+"/") {
						if !seen[target] {
							seen[target] = true
							importHits++
						}
					}
				}
			}
		}
		return nil
	})

	if err != nil {
		return false, err
	}

	min := params.Min
	if min <= 0 {
		min = 1
	}

	if params.NoneMatch {
		return importHits == 0, nil
	}
	return importHits >= min, nil
}

// patternNames extracts just the names from a list of ObservedPattern.
func patternNames(patterns []ObservedPattern) []string {
	names := make([]string, len(patterns))
	for i, p := range patterns {
		names[i] = p.Name
	}
	return names
}

// deduplicateApps merges apps with the same app_type and resolves conflicts.
// Priority: backend > cli > library > frontend.
// If a higher-priority app type is detected, lower-priority ones in the same
// root path are removed (they're likely internal packages, not separate apps).
func deduplicateApps(apps []DetectedApp) []DetectedApp {
	priority := map[string]int{
		"backend":  4,
		"cli":      3,
		"library":  2,
		"frontend": 1,
	}

	// Group by root_path
	type appGroup struct {
		maxPriority int
		app         *DetectedApp
	}
	groups := make(map[string]*appGroup)

	for _, app := range apps {
		group, ok := groups[app.RootPath]
		if !ok {
			dup := app
			dup.PatternNames = append([]string{}, app.PatternNames...)
			dup.PatternDefs = append([]ObservedPattern{}, app.PatternDefs...)
			groups[app.RootPath] = &appGroup{
				maxPriority: priority[app.AppType],
				app:         &dup,
			}
			continue
		}
		thisPri := priority[app.AppType]
		if thisPri > group.maxPriority {
			// Higher priority app replaces the lower one
			dup := app
			dup.PatternNames = append([]string{}, app.PatternNames...)
			dup.PatternDefs = append([]ObservedPattern{}, app.PatternDefs...)
			group.maxPriority = thisPri
			group.app = &dup
		} else if thisPri == group.maxPriority {
			// Same priority — merge patterns
			existingPatterns := make(map[string]bool)
			for _, n := range group.app.PatternNames {
				existingPatterns[n] = true
			}
			for _, n := range app.PatternNames {
				if !existingPatterns[n] {
					group.app.PatternNames = append(group.app.PatternNames, n)
				}
			}
			existingDefs := make(map[string]bool)
			for _, p := range group.app.PatternDefs {
				existingDefs[p.Name] = true
			}
			for _, p := range app.PatternDefs {
				if !existingDefs[p.Name] {
					group.app.PatternDefs = append(group.app.PatternDefs, p)
				}
			}
		}
		// Lower priority — skip
	}

	result := make([]DetectedApp, 0, len(groups))
	for _, g := range groups {
		result = append(result, *g.app)
	}
	return result
}

// FindPatternDef finds a pattern definition by app_type.
func FindPatternDef(patterns []PatternDef, appType string) *PatternDef {
	for _, p := range patterns {
		if p.AppType == appType {
			return &p
		}
	}
	return nil
}
