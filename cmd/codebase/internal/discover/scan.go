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

				// Scan body for behavior patterns (server vs CLI)
				ep.ServerPatterns, ep.CLIPatterns = scanMainBehavior(path)

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

// isEntryPoint checks if a Go file is a real entry point: package main + func main().
func isEntryPoint(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	hasPkgMain := false
	hasFuncMain := false

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}

		// Check for "package main"
		if strings.HasPrefix(line, "package ") {
			pkgName := strings.TrimSpace(strings.TrimPrefix(line, "package"))
			if pkgName == "main" {
				hasPkgMain = true
			}
			continue
		}

		// Check for "func main()" or "func main(" (with possible receiver or params)
		if strings.HasPrefix(line, "func main(") || strings.HasPrefix(line, "func main()") {
			hasFuncMain = true
		}
	}

	return hasPkgMain && hasFuncMain
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
		"github.com/joho/godotenv",   // CLI .env loading
		"github.com/charmbracelet/bubbletea", // TUI framework
		"github.com/charmbracelet/bubbles",   // TUI components
		"github.com/charmbracelet/lipgloss",  // TUI styling
		"github.com/gdamore/tcell/v2",       // TUI terminal
		"github.com/rivo/tview",             // TUI framework
		"github.com/pterm/pterm",             // TUI output
		"github.com/fatih/color",            // TUI coloring
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

// scanMainBehavior reads the file body to detect server vs CLI usage patterns.
// Returns (serverPatterns, cliPatterns) — slices of matched pattern descriptions.
func scanMainBehavior(path string) (serverPatterns, cliPatterns []string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil
	}
	content := string(data)

	// Server indicators — things that start an HTTP server
	serverChecks := []struct {
		pattern     string
		description string
	}{
		{"http.ListenAndServe", "calls http.ListenAndServe"},
		{"http.Server{", "creates http.Server struct"},
		{"http.ServeMux", "uses http.ServeMux"},
		{"http.Serve(", "calls http.Serve"},
		{"echo.New()", "uses Echo framework"},
		{"gin.Default()", "uses Gin framework"},
		{"chi.NewRouter()", "uses Chi router"},
		{"mux.NewRouter()", "uses gorilla/mux"},
		{".Use(middleware.", "applies middleware"},
		{"e.GET(", "Echo GET route"},
		{"e.POST(", "Echo POST route"},
		{"e.PUT(", "Echo PUT route"},
		{"e.DELETE(", "Echo DELETE route"},
		{"e.PATCH(", "Echo PATCH route"},
		{"r.GET(", "Router GET route"},
		{"r.POST(", "Router POST route"},
		{"r.Use(", "Router middleware"},
		{"router.GET(", "Router GET route"},
		{"router.POST(", "Router POST route"},
		{"router.Use(", "Router middleware"},
		{"fx.New(", "uses fx DI framework"},
		{"fx.Invoke(", "uses fx DI framework"},
		{"app.Start(", "starts app via fx"},
	}

	// CLI indicators — things that suggest a command-line tool
	cliChecks := []struct {
		pattern     string
		description string
	}{
		{"os.Args", "reads os.Args"},
		{"flag.Parse(", "parses flags"},
		{"flag.String(", "defines string flag"},
		{"flag.Bool(", "defines bool flag"},
		{"flag.Int(", "defines int flag"},
		{"flag.Duration(", "defines duration flag"},
		{"flag.Float64(", "defines float flag"},
		{"flag.Uint(", "defines uint flag"},
		{"bufio.NewReader(os.Stdin)", "reads from stdin"},
		{"bufio.NewScanner(os.Stdin)", "scans stdin"},
		{"os.Stdin", "accesses stdin"},
		{"os.Exit(", "calls os.Exit"},
		{"cobra.Command", "uses Cobra commands"},
		{"cobra.RootCommand", "uses Cobra root command"},
		{"godotenv.Load(", "loads .env file"},
		{"fmt.Fprintf(os.Stderr", "writes to stderr"},
		{"fmt.Fprintln(os.Stderr", "writes to stderr"},
		{"fmt.Fprint(os.Stderr", "writes to stderr"},
	}

	for _, check := range serverChecks {
		if strings.Contains(content, check.pattern) {
			serverPatterns = append(serverPatterns, check.description)
		}
	}

	for _, check := range cliChecks {
		if strings.Contains(content, check.pattern) {
			cliPatterns = append(cliPatterns, check.description)
		}
	}

	return serverPatterns, cliPatterns
}
