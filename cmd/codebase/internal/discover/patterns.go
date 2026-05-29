package discovercmd

import (
	"embed"
	"fmt"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed patterns/*.yaml
var patternsFS embed.FS

// LoadPatternDefs reads all embedded pattern definition YAML files.
func LoadPatternDefs() ([]PatternDef, error) {
	entries, err := patternsFS.ReadDir("patterns")
	if err != nil {
		return nil, fmt.Errorf("reading patterns directory: %w", err)
	}

	var defs []PatternDef
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		data, err := patternsFS.ReadFile(filepath.Join("patterns", entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("reading pattern %s: %w", entry.Name(), err)
		}
		var def PatternDef
		if err := yaml.Unmarshal(data, &def); err != nil {
			return nil, fmt.Errorf("parsing pattern %s: %w", entry.Name(), err)
		}
		if def.Name == "" {
			continue
		}
		defs = append(defs, def)
	}
	return defs, nil
}
