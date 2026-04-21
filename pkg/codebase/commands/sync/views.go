package sync

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	sdkgraph "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
	"github.com/emergent-company/codebase/config"
	"github.com/emergent-company/codebase/graph"
)

type ViewsOptions struct {
	Repo    string
	Glob    string
	Routes  string
	Sync    bool
	Verbose bool
	Deps    bool
}

type viewInfo struct {
	name        string
	route       string
	description string
}

type viewRecord struct {
	rel  string
	key  string
	info *viewInfo
}

func RunViews(ctx context.Context, g graph.GraphClient, opts *ViewsOptions, yml *config.CodebaseYML, out io.Writer, stderr io.Writer) error {
	glob := opts.Glob
	routesFile := opts.Routes
	if yml != nil {
		if glob == "" {
			glob = yml.Sync.Views.Glob
		}
		if routesFile == "" {
			routesFile = yml.Sync.Views.RoutesFile
		}
	}
	if glob == "" {
		return fmt.Errorf("no glob pattern configured — set sync.views.glob in .codebase.yml or pass --glob")
	}

	absRoot, err := filepath.Abs(opts.Repo)
	if err != nil {
		return fmt.Errorf("resolving root: %w", err)
	}

	routeMap := map[string]string{}
	if routesFile != "" {
		absRoutes := filepath.Join(absRoot, routesFile)
		routeMap = parseRoutesFile(absRoutes)
		fmt.Fprintf(stderr, "→ Loaded %d route patterns from %s\n", len(routeMap), routesFile)
	}

	fmt.Fprintf(stderr, "→ Scanning views with glob: %s\n", glob)
	var diskViews []viewRecord
	if err := walkGlob(absRoot, glob, func(rel string) {
		info := extractViewInfo(rel, routeMap)
		diskViews = append(diskViews, viewRecord{rel: rel, key: viewKey(rel), info: info})
	}); err != nil {
		return fmt.Errorf("walking views: %w", err)
	}
	sort.Slice(diskViews, func(i, j int) bool { return diskViews[i].rel < diskViews[j].rel })
	fmt.Fprintf(stderr, "  %d view files found\n", len(diskViews))

	fmt.Fprintln(stderr, "→ Fetching existing Context objects from graph...")
	graphObjs, err := ListAllObjects(ctx, g, "Context")
	if err != nil {
		return fmt.Errorf("fetching contexts: %w", err)
	}
	graphByKey := map[string]*sdkgraph.GraphObject{}
	for _, obj := range graphObjs {
		if DerefKey(obj.Key) != "" {
			graphByKey[DerefKey(obj.Key)] = obj
		}
	}

	var toCreate, toUpdate, upToDate []viewRecord
	for _, v := range diskViews {
		obj, exists := graphByKey[v.key]
		if !exists {
			toCreate = append(toCreate, v)
		} else if needsViewUpdate(obj, v.info) {
			toUpdate = append(toUpdate, v)
		} else {
			upToDate = append(upToDate, v)
		}
	}

	fmt.Fprintf(out, "┌─ VIEWS SYNC STATUS\n")
	fmt.Fprintf(out, "  Disk views      : %d\n", len(diskViews))
	fmt.Fprintf(out, "  Graph contexts  : %d\n", len(graphByKey))
	fmt.Fprintf(out, "  To create       : %d\n", len(toCreate))
	fmt.Fprintf(out, "  To update       : %d\n", len(toUpdate))
	fmt.Fprintf(out, "  Up to date      : %d\n", len(upToDate))

	if opts.Verbose {
		fmt.Fprintln(out)
		fmt.Fprintf(out, "┌─ ALL VIEWS\n")
		fmt.Fprintf(out, "  %-50s  %-30s  %s\n", "File", "Key", "Route")
		fmt.Fprintf(out, "  %-50s  %-30s  %s\n", strings.Repeat("─", 50), strings.Repeat("─", 30), strings.Repeat("─", 30))
		for _, v := range diskViews {
			fmt.Fprintf(out, "  %-50s  %-30s  %s\n", truncate(v.rel, 50), truncate(v.key, 30), v.info.route)
		}
	}

	if len(toCreate) > 0 {
		fmt.Fprintln(out)
		fmt.Fprintf(out, "┌─ TO CREATE (%d)\n", len(toCreate))
		for _, v := range toCreate {
			fmt.Fprintf(out, "  + %-50s  route=%s\n", v.rel, v.info.route)
		}
	}
	if len(toUpdate) > 0 {
		fmt.Fprintln(out)
		fmt.Fprintf(out, "┌─ TO UPDATE (%d)\n", len(toUpdate))
		for _, v := range toUpdate {
			fmt.Fprintf(out, "  ~ %-50s  route=%s\n", v.rel, v.info.route)
		}
	}

	if !opts.Sync && !opts.Deps {
		if len(toCreate) > 0 || len(toUpdate) > 0 {
			fmt.Fprintln(out, "\nRun with --sync to apply changes.")
		}
		return nil
	}

	fmt.Fprintln(out)
	fmt.Fprintf(out, "┌─ SYNCING\n")

	if opts.Sync {
		if len(toCreate) > 0 {
			fmt.Fprintf(out, "Creating %d Context objects...\n", len(toCreate))
			created, failed := batchCreateContexts(ctx, g, toCreate)
			fmt.Fprintf(out, "  Created: %d  Failed: %d\n", created, failed)
		}
		if len(toUpdate) > 0 {
			fmt.Fprintf(out, "Updating %d Context objects...\n", len(toUpdate))
			updated, failed := updateContexts(ctx, g, toUpdate, graphByKey)
			fmt.Fprintf(out, "  Updated: %d  Failed: %d\n", updated, failed)
		}
	}

	if opts.Deps {
		fmt.Fprintln(out)
		fmt.Fprintf(out, "┌─ WIRING CONTEXT → COMPONENT DEPENDENCIES\n")

		graphCtxObjs, err := ListAllObjects(ctx, g, "Context")
		if err != nil {
			return fmt.Errorf("fetching contexts for dep wiring: %w", err)
		}
		ctxByKey := make(map[string]*sdkgraph.GraphObject)
		for _, obj := range graphCtxObjs {
			if DerefKey(obj.Key) != "" {
				ctxByKey[DerefKey(obj.Key)] = obj
			}
		}

		uiObjs, err := ListAllObjects(ctx, g, "UIComponent")
		if err != nil {
			return fmt.Errorf("fetching UIComponents for dep wiring: %w", err)
		}
		uiByKey := make(map[string]*sdkgraph.GraphObject)
		for _, obj := range uiObjs {
			if DerefKey(obj.Key) != "" {
				uiByKey[DerefKey(obj.Key)] = obj
			}
		}

		helperObjs, err := ListAllObjects(ctx, g, "Helper")
		if err != nil {
			return fmt.Errorf("fetching Helpers for dep wiring: %w", err)
		}
		for _, obj := range helperObjs {
			if DerefKey(obj.Key) != "" {
				uiByKey[DerefKey(obj.Key)] = obj
			}
		}

		uiKeys := make([]string, 0, len(uiByKey))
		for k := range uiByKey {
			uiKeys = append(uiKeys, k)
		}

		wired, skipped, missing := wireViewComponentDeps(ctx, g, absRoot, diskViews, ctxByKey, uiByKey, uiKeys, opts.Verbose, out)
		fmt.Fprintf(out, "  Wired: %d  Skipped (exists): %d  Unresolved imports: %d\n", wired, skipped, missing)
	}

	fmt.Fprintln(out, "\nSync complete.")
	return nil
}

func extractViewInfo(rel string, routeMap map[string]string) *viewInfo {
	base := filepath.Base(rel)
	base = strings.TrimSuffix(base, filepath.Ext(base))
	for _, suffix := range []string{"-view", "-page", "-screen"} {
		base = strings.TrimSuffix(base, suffix)
	}

	name := toTitle(base)
	route := matchRoute(rel, routeMap)

	parts := strings.Split(filepath.ToSlash(rel), "/")
	domain := ""
	for i, p := range parts {
		if p == "views" && i+1 < len(parts) {
			domain = parts[i+1]
			break
		}
	}
	desc := fmt.Sprintf("View: %s", name)
	if domain != "" && domain != base {
		desc = fmt.Sprintf("%s view in %s domain", name, toTitle(domain))
	}

	return &viewInfo{name: name, route: route, description: desc}
}

func viewKey(rel string) string {
	key := strings.NewReplacer("/", "-", ".", "-", "_", "-").Replace(rel)
	key = strings.ToLower(key)
	for _, prefix := range []string{"apps-web-src-views-", "apps-web-src-", "src-views-"} {
		if strings.HasPrefix(key, prefix) {
			key = strings.TrimPrefix(key, prefix)
			break
		}
	}
	return "ctx-" + key
}

func needsViewUpdate(obj *sdkgraph.GraphObject, info *viewInfo) bool {
	return info.route != "" && StrProp(obj, "route") != info.route
}

func batchCreateContexts(ctx context.Context, g graph.GraphClient, views []viewRecord) (int, int) {
	const batchSize = 100
	created, failed := 0, 0
	for i := 0; i < len(views); i += batchSize {
		end := i + batchSize
		if end > len(views) {
			end = len(views)
		}
		batch := views[i:end]
		items := make([]sdkgraph.CreateObjectRequest, 0, len(batch))
		for _, v := range batch {
			key := v.key
			props := map[string]any{
				"context_type": "web-view",
				"type":         "screen",
				"description":  v.info.description,
			}
			if v.info.route != "" {
				props["route"] = v.info.route
			}
			props["name"] = v.info.name
			items = append(items, sdkgraph.CreateObjectRequest{
				Type:       "Context",
				Key:        &key,
				Properties: props,
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

func updateContexts(ctx context.Context, g graph.GraphClient, views []viewRecord, graphByKey map[string]*sdkgraph.GraphObject) (int, int) {
	updated, failed := 0, 0
	for _, v := range views {
		obj, ok := graphByKey[v.key]
		if !ok {
			failed++
			continue
		}
		props := map[string]any{}
		if v.info.route != "" {
			props["route"] = v.info.route
		}
		if len(props) == 0 {
			continue
		}
		if _, err := g.UpdateObject(ctx, obj.EntityID, &sdkgraph.UpdateObjectRequest{Properties: props}); err != nil {
			failed++
		} else {
			updated++
		}
	}
	return updated, failed
}

func parseRoutesFile(path string) map[string]string {
	f, err := os.Open(path)
	if err != nil {
		return map[string]string{}
	}
	defer f.Close()

	routeRe := regexp.MustCompile(`['"]([/][^'"]{2,})['"]`)
	result := map[string]string{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		matches := routeRe.FindAllStringSubmatch(line, -1)
		for _, m := range matches {
			route := m[1]
			if !strings.HasPrefix(route, "/") || len(route) <= 1 {
				continue
			}
			parts := strings.Split(strings.Trim(route, "/"), "/")
			for i := len(parts) - 1; i >= 0; i-- {
				seg := parts[i]
				if seg != "" && !strings.HasPrefix(seg, ":") {
					if _, exists := result[seg]; !exists {
						result[seg] = route
					}
					break
				}
			}
			if len(parts) > 0 && parts[0] != "" && !strings.HasPrefix(parts[0], ":") {
				if _, exists := result[parts[0]]; !exists {
					result[parts[0]] = route
				}
			}
		}
	}
	return result
}

func matchRoute(rel string, routeMap map[string]string) string {
	if len(routeMap) == 0 {
		return ""
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	for i := len(parts) - 1; i >= 0; i-- {
		seg := strings.ToLower(parts[i])
		for _, ext := range []string{".tsx", ".ts", ".jsx", ".js"} {
			seg = strings.TrimSuffix(seg, ext)
		}
		if route, ok := routeMap[seg]; ok {
			return route
		}
		seg2 := strings.ReplaceAll(seg, "-", "")
		if route, ok := routeMap[seg2]; ok {
			return route
		}
	}
	return ""
}

func walkGlob(root, pattern string, fn func(rel string)) error {
	absPattern := filepath.Join(root, pattern)
	prefix := globPrefix(absPattern)

	return filepath.WalkDir(prefix, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if strings.HasPrefix(name, ".") || name == "node_modules" || name == "dist" || name == "__tests__" || name == "stories" {
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if matchesGlob(pattern, rel) {
			fn(rel)
		}
		return nil
	})
}

func globPrefix(pattern string) string {
	sep := string(filepath.Separator)
	parts := strings.Split(pattern, sep)
	var prefix []string
	for _, p := range parts {
		if strings.ContainsAny(p, "*?[") {
			break
		}
		prefix = append(prefix, p)
	}
	if len(prefix) == 0 {
		return "."
	}
	return strings.Join(prefix, sep)
}

func matchesGlob(pattern, rel string) bool {
	pattern = filepath.ToSlash(pattern)
	rel = filepath.ToSlash(rel)
	if !strings.Contains(pattern, "**") {
		m, _ := filepath.Match(pattern, rel)
		return m
	}
	return matchDoubleStarGlob(pattern, rel)
}

func matchDoubleStarGlob(pattern, path string) bool {
	patParts := strings.Split(pattern, "/")
	pathParts := strings.Split(path, "/")
	return matchParts(patParts, pathParts)
}

func matchParts(patParts, pathParts []string) bool {
	for len(patParts) > 0 {
		p := patParts[0]
		if p == "**" {
			rest := patParts[1:]
			for i := 0; i <= len(pathParts); i++ {
				if matchParts(rest, pathParts[i:]) {
					return true
				}
			}
			return false
		}
		if len(pathParts) == 0 {
			return false
		}
		m, _ := filepath.Match(p, pathParts[0])
		if !m {
			return false
		}
		patParts = patParts[1:]
		pathParts = pathParts[1:]
	}
	return len(pathParts) == 0
}

func toTitle(s string) string {
	s = strings.ReplaceAll(s, "-", " ")
	s = strings.ReplaceAll(s, "_", " ")
	words := strings.Fields(s)
	for i, w := range words {
		if len(w) > 0 {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(words, " ")
}

func importToUIKey(importPath string, sourceRel string) string {
	importPath = filepath.ToSlash(importPath)
	var resolvedBase string
	if strings.HasPrefix(importPath, "shared-web/components/") {
		resolvedBase = "libs/shared-web/src/" + strings.TrimPrefix(importPath, "shared-web/")
	} else if strings.HasPrefix(importPath, ".") {
		sourceDir := filepath.ToSlash(filepath.Dir(sourceRel))
		resolvedBase = filepath.ToSlash(filepath.Join(sourceDir, importPath))
	} else {
		return ""
	}
	for _, ext := range []string{".tsx", ".ts"} {
		candidate := resolvedBase + ext
		return componentKey(candidate)
	}
	return ""
}

func wireViewComponentDeps(
	ctx context.Context,
	g graph.GraphClient,
	absRoot string,
	diskViews []viewRecord,
	ctxByKey map[string]*sdkgraph.GraphObject,
	uiByKey map[string]*sdkgraph.GraphObject,
	uiKeys []string,
	verbose bool,
	out io.Writer,
) (int, int, int) {
	existingRels := map[string]bool{}
	rels, _ := ListAllRelationships(ctx, g, "contains")
	for _, r := range rels {
		existingRels[r.SrcID+":"+r.DstID] = true
	}

	var toWire []sdkgraph.CreateRelationshipRequest
	unresolved := 0

	for _, view := range diskViews {
		ctxObj, ok := ctxByKey[view.key]
		if !ok {
			continue
		}

		absPath := filepath.Join(absRoot, view.rel)
		imports := parseComponentImports(absPath)

		for _, imp := range imports {
			if !strings.HasPrefix(imp, ".") && !strings.HasPrefix(imp, "shared-web/components") {
				continue
			}

			uiKey := importToUIKey(imp, view.rel)
			if uiKey == "" {
				unresolved++
				continue
			}

			dstObj, ok := uiByKey[uiKey]
			if !ok {
				uiKeyIdx := strings.TrimSuffix(uiKey, "-tsx") + "-index-tsx"
				dstObj, ok = uiByKey[uiKeyIdx]
				if !ok {
					unresolved++
					continue
				}
			}

			relKey := ctxObj.EntityID + ":" + dstObj.EntityID
			if existingRels[relKey] {
				continue
			}
			existingRels[relKey] = true
			if verbose {
				fmt.Fprintf(out, "  %s → %s\n", view.key, uiKey)
			}
			toWire = append(toWire, sdkgraph.CreateRelationshipRequest{
				Type:   "contains",
				SrcID:  ctxObj.EntityID,
				DstID:  dstObj.EntityID,
				Upsert: true,
			})
		}
	}

	fmt.Fprintf(out, "  Found %d context→component dependency edges to wire\n", len(toWire))

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

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "…" + s[len(s)-(n-1):]
}
