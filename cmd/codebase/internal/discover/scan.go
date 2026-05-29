package discovercmd

import (
	"bufio"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ScanDir collects raw structural facts about a repo — no classification.
func ScanDir(repoDir string) (*ScanResult, error) {
	absDir, err := filepath.Abs(repoDir)
	if err != nil {
		return nil, err
	}

	result := &ScanResult{
		Dir: absDir,
	}

	goModDirs := make(map[string]bool)

	err = filepath.Walk(absDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip inaccessible
		}
		if info.IsDir() {
			base := info.Name()
			// Skip hidden dirs, vendor, node_modules
			if strings.HasPrefix(base, ".") && base != "." {
				return filepath.SkipDir
			}
			if base == "vendor" || base == "node_modules" || base == "dist" || base == "build" {
				return filepath.SkipDir
			}
			return nil
		}

		rel, err := filepath.Rel(absDir, path)
		if err != nil {
			return nil
		}

		base := info.Name()

		switch {
		case base == "go.mod":
			result.GoModules = append(result.GoModules, rel)
			goModDirs[filepath.Dir(path)] = true

		case base == "Dockerfile" || strings.HasPrefix(base, "Dockerfile."):
			result.Dockerfiles = append(result.Dockerfiles, rel)

		case base == "Makefile" || base == "makefile":
			result.HasMakefile = true

		case strings.HasSuffix(base, ".go") && !strings.HasSuffix(base, "_test.go"):
			result.GoFiles++

			// Check if this is an entry point (package main or has main())
			if isEntryPoint(path) {
				ep := EntryPoint{
					Path:    rel,
					Dir:     filepath.Dir(rel),
					HasMain: true,
				}
				// Find closest go.mod
				for modDir := range goModDirs {
					if strings.HasPrefix(filepath.Dir(path)+"/", modDir+"/") || filepath.Dir(path) == modDir {
						epDir, _ := filepath.Rel(absDir, modDir)
						ep.GoModDir = epDir
					}
				}
				if ep.GoModDir == "" {
					ep.GoModDir = "."
				}

				// Scan imports
				ep.Imports = scanGoImports(path)
				sort.Strings(ep.Imports)

				result.EntryPoints = append(result.EntryPoints, ep)
			}
		}
		return nil
	})

	if err != nil {
		return nil, err
	}

	// Collect top-level subdirectories (one level deep)
	entries, err := os.ReadDir(absDir)
	if err == nil {
		for _, e := range entries {
			if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
				result.Subdirs = append(result.Subdirs, e.Name())
			}
		}
		sort.Strings(result.Subdirs)
	}

	sort.Strings(result.GoModules)
	sort.Strings(result.Dockerfiles)

	return result, nil
}

// isEntryPoint checks if a Go file has "package main" or a main() function.
func isEntryPoint(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}
		if strings.HasPrefix(line, "package ") {
			pkgName := strings.TrimSpace(strings.TrimPrefix(line, "package"))
			if pkgName == "main" {
				return true
			}
		}
	}
	return false
}

// scanGoImports extracts import paths from a Go source file.
// Returns import paths (excluding stdlib) that look like known frameworks.
func scanGoImports(path string) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	knownPrefixes := []string{
		"github.com/spf13/cobra",
		"github.com/urfave/cli",
		"github.com/labstack/echo",
		"github.com/gin-gonic/gin",
		"github.com/go-chi/chi",
		"github.com/gorilla/mux",
		"net/http",
		"github.com/emergent-company",
		"github.com/mkucharz",
		"golang.org/x/",
		"github.com/jmoiron/sqlx",
		"database/sql",
		"github.com/redis/go-redis",
		"github.com/stretchr/testify",
	}

	var imports []string
	seen := make(map[string]bool)

	scanner := bufio.NewScanner(f)
	inImport := false
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
			imp := strings.Trim(line, "\" ")
			if imp == "" || strings.HasPrefix(imp, "_") || strings.HasPrefix(imp, ".") {
				continue
			}
			for _, prefix := range knownPrefixes {
				if strings.HasPrefix(imp, prefix) && !seen[imp] {
					seen[imp] = true
					imports = append(imports, imp)
				}
			}
		}
	}

	return imports
}
