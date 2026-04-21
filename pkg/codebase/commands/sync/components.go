package sync

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	sdkgraph "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
	"github.com/emergent-company/codebase/config"
	"github.com/emergent-company/codebase/graph"
)

type ComponentsOptions struct {
	Repo    string
	Glob    string
	Sync    bool
	Verbose bool
	Deps    bool
}

type componentInfo struct {
	name        string
	compType    string
	description string
	objectType  string // "UIComponent" or "Helper"
}

type componentRecord struct {
	rel  string
	key  string
	info *componentInfo
}

func RunComponents(ctx context.Context, g graph.GraphClient, opts *ComponentsOptions, yml *config.CodebaseYML, out io.Writer, stderr io.Writer) error {
	glob := opts.Glob
	if yml != nil && glob == "" {
		glob = yml.Sync.Components.Glob
	}
	if glob == "" {
		return fmt.Errorf("no glob pattern configured — set sync.components.glob in .codebase.yml or pass --glob")
	}

	absRoot, err := filepath.Abs(opts.Repo)
	if err != nil {
		return fmt.Errorf("resolving root: %w", err)
	}

	fmt.Fprintf(stderr, "→ Scanning components with glob: %s\n", glob)
	var diskComponents []componentRecord
	if err := walkGlob(absRoot, glob, func(rel string) {
		base := filepath.Base(rel)
		if strings.Contains(base, ".stories.") || strings.Contains(base, ".test.") ||
			strings.Contains(base, ".spec.") || base == "index.tsx" || base == "index.ts" {
			return
		}
		info := extractComponentInfo(rel)
		diskComponents = append(diskComponents, componentRecord{rel: rel, key: componentKey(rel), info: info})
	}); err != nil {
		return fmt.Errorf("walking components: %w", err)
	}
	sort.Slice(diskComponents, func(i, j int) bool { return diskComponents[i].rel < diskComponents[j].rel })
	fmt.Fprintf(stderr, "  %d component files found\n", len(diskComponents))

	fmt.Fprintln(stderr, "→ Fetching existing UIComponent objects from graph...")
	graphObjs, err := ListAllObjects(ctx, g, "UIComponent")
	if err != nil {
		return fmt.Errorf("fetching components: %w", err)
	}
	graphByKey := map[string]*sdkgraph.GraphObject{}
	for _, obj := range graphObjs {
		if DerefKey(obj.Key) != "" {
			graphByKey[DerefKey(obj.Key)] = obj
		}
	}

	helperObjs, err := ListAllObjects(ctx, g, "Helper")
	if err != nil {
		return fmt.Errorf("fetching helpers: %w", err)
	}
	for _, obj := range helperObjs {
		if DerefKey(obj.Key) != "" {
			graphByKey[DerefKey(obj.Key)] = obj
		}
	}

	var toCreate, upToDate []componentRecord
	for _, comp := range diskComponents {
		if _, exists := graphByKey[comp.key]; exists {
			upToDate = append(upToDate, comp)
		} else {
			toCreate = append(toCreate, comp)
		}
	}

	uiCount := 0
	helperCount := 0
	for _, comp := range diskComponents {
		if comp.info.objectType == "Helper" {
			helperCount++
		} else {
			uiCount++
		}
	}

	fmt.Fprintf(out, "┌─ COMPONENTS SYNC STATUS\n")
	fmt.Fprintf(out, "  Disk components : %d (%d UIComponent, %d Helper)\n", len(diskComponents), uiCount, helperCount)
	fmt.Fprintf(out, "  Graph objects   : %d\n", len(graphByKey))
	fmt.Fprintf(out, "  To create       : %d\n", len(toCreate))
	fmt.Fprintf(out, "  Up to date      : %d\n", len(upToDate))

	if opts.Verbose {
		fmt.Fprintln(out)
		fmt.Fprintf(out, "┌─ ALL COMPONENTS\n")
		fmt.Fprintf(out, "  %-50s  %-20s  %s\n", "File", "Key", "Type")
		fmt.Fprintf(out, "  %-50s  %-20s  %s\n", strings.Repeat("─", 50), strings.Repeat("─", 20), strings.Repeat("─", 15))
		for _, comp := range diskComponents {
			fmt.Fprintf(out, "  %-50s  %-20s  %s\n", truncate(comp.rel, 50), truncate(comp.key, 20), comp.info.compType)
		}
	}

	if len(toCreate) > 0 {
		fmt.Fprintln(out)
		fmt.Fprintf(out, "┌─ TO CREATE (%d)\n", len(toCreate))
		for _, comp := range toCreate {
			fmt.Fprintf(out, "  + %-50s  type=%s\n", comp.rel, comp.info.compType)
		}
	}

	if !opts.Sync && !opts.Deps {
		if len(toCreate) > 0 {
			fmt.Fprintln(out, "\nRun with --sync to apply changes.")
		}
		return nil
	}

	fmt.Fprintln(out)
	fmt.Fprintf(out, "┌─ SYNCING\n")

	if opts.Sync && len(toCreate) > 0 {
		fmt.Fprintf(out, "Creating %d UIComponent objects...\n", len(toCreate))
		created, failed := batchCreateComponents(ctx, g, toCreate)
		fmt.Fprintf(out, "  Created: %d  Failed: %d\n", created, failed)
	}

	if opts.Deps {
		fmt.Fprintln(out)
		fmt.Fprintf(out, "┌─ WIRING COMPONENT DEPENDENCIES\n")

		graphObjs2, err := ListAllObjects(ctx, g, "UIComponent")
		if err != nil {
			return fmt.Errorf("fetching components for dep wiring: %w", err)
		}
		graphByKey2 := map[string]*sdkgraph.GraphObject{}
		for _, obj := range graphObjs2 {
			if DerefKey(obj.Key) != "" {
				graphByKey2[DerefKey(obj.Key)] = obj
			}
		}

		relToKey := map[string]string{}
		for _, comp := range diskComponents {
			relToKey[comp.rel] = comp.key
		}

		wired, skipped, missing := wireComponentDeps(ctx, g, absRoot, diskComponents, relToKey, graphByKey2, opts.Verbose, yml, out)
		fmt.Fprintf(out, "  Wired: %d  Skipped (exists): %d  Unresolved imports: %d\n", wired, skipped, missing)
	}

	fmt.Fprintln(out, "\nSync complete.")
	return nil
}

func extractComponentInfo(rel string) *componentInfo {
	base := filepath.Base(rel)
	base = strings.TrimSuffix(base, filepath.Ext(base))
	for _, suffix := range []string{".component", ".widget", ".container"} {
		base = strings.TrimSuffix(base, suffix)
	}

	name := toTitle(base)
	lower := strings.ToLower(filepath.ToSlash(rel))
	isHook := strings.Contains(lower, "/hooks/") ||
		strings.HasPrefix(strings.ToLower(base), "use-") ||
		strings.HasPrefix(strings.ToLower(base), "use_")

	if isHook {
		return &componentInfo{
			name:        name,
			compType:    "hook",
			description: fmt.Sprintf("React hook: %s", name),
			objectType:  "Helper",
		}
	}

	compType := inferComponentType(rel)
	desc := fmt.Sprintf("%s UI component", name)
	if compType != "" {
		desc = fmt.Sprintf("%s UI component (%s)", name, compType)
	}

	return &componentInfo{name: name, compType: compType, description: desc, objectType: "UIComponent"}
}

func inferComponentType(rel string) string {
	lower := strings.ToLower(filepath.ToSlash(rel))
	switch {
	case strings.Contains(lower, "/atoms/") || strings.Contains(lower, "/atom/"):
		return "primitive"
	case strings.Contains(lower, "/molecules/") || strings.Contains(lower, "/molecule/"):
		return "composite"
	case strings.Contains(lower, "/organisms/") || strings.Contains(lower, "/organism/"):
		return "composite"
	case strings.Contains(lower, "/templates/") || strings.Contains(lower, "/template/"):
		return "layout"
	case strings.Contains(lower, "/layouts/") || strings.Contains(lower, "/layout/"):
		return "layout"
	case strings.Contains(lower, "/containers/") || strings.Contains(lower, "/container/"):
		return "container"
	case strings.Contains(lower, "/pages/") || strings.Contains(lower, "/page/"):
		return "container"
	case strings.Contains(lower, "/forms/") || strings.Contains(lower, "/form/"):
		return "composite"
	case strings.Contains(lower, "/table/") || strings.Contains(lower, "/tables/"):
		return "composite"
	case strings.Contains(lower, "/modal/") || strings.Contains(lower, "/modals/"):
		return "composite"
	default:
		return "composite"
	}
}

func componentKey(rel string) string {
	return componentKeyWithSiblings(rel, nil)
}

func componentKeyWithSiblings(rel string, siblings []string) string {
	rel = filepath.ToSlash(rel)
	lower := strings.ToLower(rel)
	isHook := strings.Contains(lower, "/hooks/")

	base := filepath.Base(rel)
	for _, ext := range []string{".tsx", ".ts", ".jsx", ".js"} {
		base = strings.TrimSuffix(base, ext)
	}
	slug := stemSlug(base)
	prefix := "ui-"
	if isHook {
		prefix = "hook-"
	}
	key := prefix + slug

	for _, sib := range siblings {
		if sib == rel {
			continue
		}
		sibBase := filepath.Base(sib)
		for _, ext := range []string{".tsx", ".ts", ".jsx", ".js"} {
			sibBase = strings.TrimSuffix(sibBase, ext)
		}
		if stemSlug(sibBase) == slug {
			parent := parentSlug(rel)
			if parent != "" {
				key = prefix + slug + "-" + parent
			}
			break
		}
	}
	return key
}

func stemSlug(s string) string {
	s = strings.NewReplacer("_", "-", ".", "-").Replace(s)
	s = strings.ToLower(s)
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	s = strings.Trim(s, "-")
	parts := strings.Split(s, "-")
	deduped := make([]string, 0, len(parts))
	for i, p := range parts {
		if i > 0 && p == parts[i-1] {
			continue
		}
		deduped = append(deduped, p)
	}
	return strings.Join(deduped, "-")
}

func parentSlug(rel string) string {
	rel = filepath.ToSlash(rel)
	parts := strings.Split(rel, "/")
	if len(parts) > 0 {
		parts = parts[:len(parts)-1]
	}
	for len(parts) > 0 && (parts[len(parts)-1] == "components" || parts[len(parts)-1] == "hooks") {
		parts = parts[:len(parts)-1]
	}
	if len(parts) == 0 {
		return ""
	}
	return stemSlug(parts[len(parts)-1])
}

var importRe = regexp.MustCompile(`(?m)from\s+['"]([^'"]+)['"]`)

func parseComponentImports(absPath string) []string {
	f, err := os.Open(absPath)
	if err != nil {
		return nil
	}
	defer f.Close()

	var imports []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		matches := importRe.FindAllStringSubmatch(line, -1)
		for _, m := range matches {
			if len(m) > 1 {
				imports = append(imports, m[1])
			}
		}
	}
	return imports
}

func resolveImportToKey(importPath string, sourceRel string, relToKey map[string]string, yml *config.CodebaseYML) string {
	if strings.HasPrefix(importPath, "shared-web/components/") {
		rel := "libs/shared-web/src/" + strings.TrimPrefix(importPath, "shared-web/")
		for _, ext := range []string{".tsx", ".ts", "/index.tsx", "/index.ts"} {
			candidate := rel + ext
			if key, ok := relToKey[candidate]; ok {
				return key
			}
		}
		return ""
	}

	if strings.HasPrefix(importPath, ".") {
		sourceDir := filepath.Dir(sourceRel)
		resolved := filepath.Join(sourceDir, importPath)
		resolved = filepath.ToSlash(resolved)
		for _, ext := range []string{".tsx", ".ts", "/index.tsx", "/index.ts"} {
			candidate := resolved + ext
			if key, ok := relToKey[candidate]; ok {
				return key
			}
		}
		return ""
	}

	return ""
}

func wireComponentDeps(
	ctx context.Context,
	g graph.GraphClient,
	absRoot string,
	diskComponents []componentRecord,
	relToKey map[string]string,
	graphByKey map[string]*sdkgraph.GraphObject,
	verbose bool,
	yml *config.CodebaseYML,
	out io.Writer,
) (int, int, int) {
	existingRels := map[string]bool{}
	rels, _ := ListAllRelationships(ctx, g, "contains")
	for _, r := range rels {
		existingRels[r.SrcID+":"+r.DstID] = true
	}

	var toWire []sdkgraph.CreateRelationshipRequest
	unresolved := 0

	for _, comp := range diskComponents {
		srcObj, ok := graphByKey[comp.key]
		if !ok {
			continue
		}

		absPath := filepath.Join(absRoot, comp.rel)
		imports := parseComponentImports(absPath)

		for _, imp := range imports {
			dstKey := resolveImportToKey(imp, comp.rel, relToKey, yml)
			if dstKey == "" {
				if strings.HasPrefix(imp, ".") || strings.HasPrefix(imp, "shared-web/components") {
					unresolved++
				}
				continue
			}
			dstObj, ok := graphByKey[dstKey]
			if !ok {
				unresolved++
				continue
			}
			if srcObj.EntityID == dstObj.EntityID {
				continue
			}
			relKey := srcObj.EntityID + ":" + dstObj.EntityID
			if existingRels[relKey] {
				continue
			}
			existingRels[relKey] = true
			if verbose {
				fmt.Fprintf(out, "  %s → %s\n", comp.key, dstKey)
			}
			toWire = append(toWire, sdkgraph.CreateRelationshipRequest{
				Type:   "contains",
				SrcID:  srcObj.EntityID,
				DstID:  dstObj.EntityID,
				Upsert: true,
			})
		}
	}

	fmt.Fprintf(out, "  Found %d component dependency edges to wire\n", len(toWire))

	const batchSize = 100
	wired := 0
	skipped := 0
	for i := 0; i < len(toWire); i += batchSize {
		end := i + batchSize
		if end > len(toWire) {
			end = len(toWire)
		}
		resp, err := g.BulkCreateRelationships(ctx, &sdkgraph.BulkCreateRelationshipsRequest{Items: toWire[i:end]})
		if err != nil {
			skipped += end - i
			continue
		}
		for _, r := range resp.Results {
			if r.Error != nil {
				skipped++
			} else {
				wired++
			}
		}
	}
	return wired, skipped, unresolved
}

func batchCreateComponents(ctx context.Context, g graph.GraphClient, comps []componentRecord) (int, int) {
	const batchSize = 100
	created, failed := 0, 0
	for i := 0; i < len(comps); i += batchSize {
		end := i + batchSize
		if end > len(comps) {
			end = len(comps)
		}
		batch := comps[i:end]
		items := make([]sdkgraph.CreateObjectRequest, 0, len(batch))
		for _, comp := range batch {
			key := comp.key
			objType := comp.info.objectType
			if objType == "" {
				objType = "UIComponent"
			}
			items = append(items, sdkgraph.CreateObjectRequest{
				Type: objType,
				Key:  &key,
				Properties: map[string]any{
					"name":        comp.info.name,
					"type":        comp.info.compType,
					"description": comp.info.description,
				},
			})
		}
		resp, err := g.BulkCreateObjects(ctx, &sdkgraph.BulkCreateObjectsRequest{Items: items})
		if err != nil {
			failed += len(batch)
			continue
		}
		created += resp.Success
		failed += resp.Failed
	}
	return created, failed
}
