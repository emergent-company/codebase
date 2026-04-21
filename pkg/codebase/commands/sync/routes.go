package sync

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	sdkgraph "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
	"github.com/emergent-company/codebase/config"
	"github.com/emergent-company/codebase/graph"
	"github.com/emergent-company/codebase/commands/sync/extractors"
	"github.com/olekukonko/tablewriter"
	"github.com/olekukonko/tablewriter/tw"
)

type RoutesOptions struct {
	Repo      string
	DryRun    bool
	RouteGlob string
	Defines   bool
}

// extractorRoute is the JSON contract emitted by extractor scripts (one per line).
type extractorRoute struct {
	Method       string   `json:"method"`
	Path         string   `json:"path"`
	Handler      string   `json:"handler"`
	Domain       string   `json:"domain"`
	File         string   `json:"file"`
	AuthRequired bool     `json:"auth_required"`
	Scopes       string   `json:"scopes"`
	Summary      string   `json:"summary"`
	Tags         []string `json:"tags"`
}

// runExtractorScript executes a custom or framework extractor and returns parsed routes.
func runExtractorScript(cfg config.SyncRoutesConfig, repoRoot string, stderr io.Writer) ([]CodeRoute, error) {
	var cmdParts []string

	switch {
	case cfg.Command != "":
		scriptPath := cfg.Command
		if !filepath.IsAbs(scriptPath) {
			scriptPath = filepath.Join(repoRoot, scriptPath)
		}
		if cfg.Runtime != "" {
			cmdParts = append(strings.Fields(cfg.Runtime), scriptPath)
		} else {
			cmdParts = []string{scriptPath}
		}
	case cfg.Framework != "":
		tmpPath, err := extractors.ExtractToTemp(cfg.Framework)
		if err != nil {
			return nil, err
		}
		defer os.Remove(tmpPath)
		runtime := cfg.Runtime
		if runtime == "" {
			runtime = "node"
		}
		cmdParts = append(strings.Fields(runtime), tmpPath)
	default:
		return nil, fmt.Errorf("no extractor configured (set sync.routes.command or sync.routes.framework in .codebase.yml)")
	}

	if cfg.Glob != "" {
		cmdParts = append(cmdParts, "--glob", cfg.Glob)
	}
	if cfg.DomainSegment != 0 {
		cmdParts = append(cmdParts, "--domain-segment", fmt.Sprint(cfg.DomainSegment))
	}

	fmt.Fprintf(stderr, "  Running extractor: %s\n", strings.Join(cmdParts, " "))

	cmd := exec.Command(cmdParts[0], cmdParts[1:]...) //nolint:gosec
	cmd.Dir = repoRoot
	cmd.Stderr = stderr

	stdout, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("extractor failed: %w", err)
	}

	var routes []CodeRoute
	scanner := bufio.NewScanner(strings.NewReader(string(stdout)))
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var er extractorRoute
		if err := json.Unmarshal([]byte(line), &er); err != nil {
			fmt.Fprintf(stderr, "  warn: extractor line %d: invalid JSON: %v\n", lineNum, err)
			continue
		}
		authReq := er.AuthRequired
		routes = append(routes, CodeRoute{
			Domain:       er.Domain,
			Method:       strings.ToUpper(er.Method),
			Path:         er.Path,
			Handler:      er.Handler,
			File:         er.File,
			AuthRequired: &authReq,
		})
	}
	return routes, nil
}

type CodeRoute struct {
	Domain       string
	Method       string
	Path         string
	Handler      string
	File         string
	Line         int
	AuthRequired *bool
}

type MatchResult struct {
	Route      CodeRoute
	GraphKey   string
	GraphID    string
	GraphEP    *sdkgraph.GraphObject
	MatchType  string
	Ambiguous  bool
	OldPath    string
	OldMethod  string
	OldHandler string
	OldFile    string
}

type UpdateRecord struct {
	GraphID string
	Key     string
	Props   map[string]any
}

type CreateRecord struct {
	Key   string
	Route CodeRoute
}

func RunRoutes(ctx context.Context, g graph.GraphClient, opts *RoutesOptions, yml *config.CodebaseYML, out io.Writer, stderr io.Writer) error {
	fmt.Fprintln(stderr, "→ Fetching APIEndpoint objects from graph...")
	graphEPs, err := ListAllObjects(ctx, g, "APIEndpoint")
	if err != nil {
		return fmt.Errorf("listing APIEndpoints: %w", err)
	}
	fmt.Fprintf(stderr, "  Found %d APIEndpoint objects\n", len(graphEPs))

	byKey := make(map[string]*sdkgraph.GraphObject)
	byDomainHandler := make(map[string][]*sdkgraph.GraphObject)
	byHandler := make(map[string][]*sdkgraph.GraphObject)

	for _, ep := range graphEPs {
		if DerefKey(ep.Key) != "" {
			byKey[DerefKey(ep.Key)] = ep
		}
		domain := StrProp(ep, "domain")
		handler := strings.ToLower(StrProp(ep, "handler"))
		if domain != "" && handler != "" {
			k := domain + ":" + handler
			byDomainHandler[k] = append(byDomainHandler[k], ep)
		}
		if handler != "" {
			byHandler[handler] = append(byHandler[handler], ep)
		}
	}

	fmt.Fprintln(stderr, "→ Scanning route files...")
	var codeRoutes []CodeRoute

	useExtractor := yml != nil && (yml.Sync.Routes.Command != "" || yml.Sync.Routes.Framework != "")

	if useExtractor {
		extracted, err := runExtractorScript(yml.Sync.Routes, opts.Repo, stderr)
		if err != nil {
			return fmt.Errorf("extractor: %w", err)
		}
		codeRoutes = extracted
	} else {
		for _, pattern := range strings.Split(opts.RouteGlob, ",") {
			pattern = strings.TrimSpace(pattern)
			matches, err := filepath.Glob(filepath.Join(opts.Repo, pattern))
			if err != nil {
				fmt.Fprintf(stderr, "  warn: glob %q: %v\n", pattern, err)
				continue
			}
			for _, f := range matches {
				domain := extractDomain(f)
				routes, err := parseRouteFile(f, domain, opts.Repo)
				if err != nil {
					fmt.Fprintf(stderr, "  warn: parsing %s: %v\n", f, err)
					continue
				}
				codeRoutes = append(codeRoutes, routes...)
			}
		}
	}
	fmt.Fprintf(stderr, "  Found %d code routes\n", len(codeRoutes))

	var canonicalRoutes []CodeRoute
	var aliasRoutes []CodeRoute
	groups := make(map[string][]CodeRoute)
	for _, r := range codeRoutes {
		key := r.Domain + ":" + strings.ToLower(r.Handler)
		groups[key] = append(groups[key], r)
	}

	for _, group := range groups {
		if len(group) == 1 {
			canonicalRoutes = append(canonicalRoutes, group[0])
			continue
		}
		sort.Slice(group, func(i, j int) bool {
			r1, r2 := group[i], group[j]
			p1Api := strings.HasPrefix(r1.Path, "/api/")
			p2Api := strings.HasPrefix(r2.Path, "/api/")
			if p1Api != p2Api {
				return p1Api
			}
			if len(r1.Path) != len(r2.Path) {
				return len(r1.Path) < len(r2.Path)
			}
			return r1.Path < r2.Path
		})
		canonicalRoutes = append(canonicalRoutes, group[0])
		aliasRoutes = append(aliasRoutes, group[1:]...)
	}

	var results []MatchResult
	matchedGraphIDs := make(map[string]bool)

	for _, r := range canonicalRoutes {
		res := MatchResult{Route: r}
		candidateKey := "ep-" + r.Domain + "-" + strings.ToLower(r.Handler)
		if ep, ok := byKey[candidateKey]; ok {
			res.MatchType = "key"
			res.GraphKey = DerefKey(ep.Key)
			res.GraphID = ep.EntityID
			res.GraphEP = ep
			res.OldPath = StrProp(ep, "path")
			res.OldMethod = StrProp(ep, "method")
			res.OldHandler = StrProp(ep, "handler")
			res.OldFile = StrProp(ep, "file")
			matchedGraphIDs[ep.EntityID] = true
			results = append(results, res)
			continue
		}

		dhKey := r.Domain + ":" + strings.ToLower(r.Handler)
		if eps, ok := byDomainHandler[dhKey]; ok {
			if len(eps) == 1 {
				ep := eps[0]
				res.MatchType = "handler+domain"
				res.GraphKey = DerefKey(ep.Key)
				res.GraphID = ep.EntityID
				res.GraphEP = ep
				res.OldPath = StrProp(ep, "path")
				res.OldMethod = StrProp(ep, "method")
				res.OldHandler = StrProp(ep, "handler")
				res.OldFile = StrProp(ep, "file")
				matchedGraphIDs[ep.EntityID] = true
			} else {
				res.MatchType = "handler+domain"
				res.Ambiguous = true
				res.GraphKey = DerefKey(eps[0].Key) + " (+" + fmt.Sprint(len(eps)-1) + " more)"
			}
			results = append(results, res)
			continue
		}

		hKey := strings.ToLower(r.Handler)
		if eps, ok := byHandler[hKey]; ok {
			ep := eps[0]
			res.MatchType = "handler-only"
			res.Ambiguous = len(eps) > 1
			res.GraphKey = DerefKey(ep.Key)
			res.GraphID = ep.EntityID
			res.GraphEP = ep
			res.OldPath = StrProp(ep, "path")
			res.OldMethod = StrProp(ep, "method")
			res.OldHandler = StrProp(ep, "handler")
			res.OldFile = StrProp(ep, "file")
			if !res.Ambiguous {
				matchedGraphIDs[ep.EntityID] = true
			}
			results = append(results, res)
			continue
		}
		res.MatchType = "unmatched"
		results = append(results, res)
	}

	var stale []*sdkgraph.GraphObject
	for _, ep := range graphEPs {
		if !matchedGraphIDs[ep.EntityID] {
			stale = append(stale, ep)
		}
	}

	var updates []UpdateRecord
	var creates []CreateRecord
	var skipped []MatchResult

	for _, res := range results {
		if res.Ambiguous {
			skipped = append(skipped, res)
			continue
		}
		if res.MatchType == "unmatched" || res.GraphID == "" {
			if useExtractor {
				key := "ep-" + res.Route.Domain + "-" + strings.ToLower(res.Route.Handler)
				creates = append(creates, CreateRecord{Key: key, Route: res.Route})
			} else {
				skipped = append(skipped, res)
			}
			continue
		}
		var oldAuth string
		if res.GraphEP != nil {
			if v, ok := res.GraphEP.Properties["auth_required"]; ok {
				switch val := v.(type) {
				case bool:
					if val {
						oldAuth = "true"
					} else {
						oldAuth = "false"
					}
				case string:
					oldAuth = val
				}
			}
		}
		var newAuthStr string
		if res.Route.AuthRequired != nil {
			if *res.Route.AuthRequired {
				newAuthStr = "true"
			} else {
				newAuthStr = "false"
			}
		}
		authNeedsUpdate := newAuthStr != "" && oldAuth != newAuthStr

		needsUpdate := res.OldPath != res.Route.Path ||
			!strings.EqualFold(res.OldMethod, res.Route.Method) ||
			res.OldFile != res.Route.File ||
			authNeedsUpdate
		if !needsUpdate {
			continue
		}
		props := map[string]any{
			"path":    res.Route.Path,
			"method":  res.Route.Method,
			"handler": res.Route.Handler,
			"file":    res.Route.File,
		}
		if authNeedsUpdate {
			props["auth_required"] = *res.Route.AuthRequired
		}
		updates = append(updates, UpdateRecord{
			GraphID: res.GraphID,
			Key:     res.GraphKey,
			Props:   props,
		})
	}

	sort.Slice(updates, func(i, j int) bool { return updates[i].Key < updates[j].Key })
	sort.Slice(creates, func(i, j int) bool { return creates[i].Key < creates[j].Key })

	applied := 0
	failed := 0
	created := 0
	createFailed := 0

	if !opts.DryRun && len(creates) > 0 {
		fmt.Fprintf(stderr, "→ Creating %d new APIEndpoint objects via bulk API...\n", len(creates))
		const batchSize = 100
		for start := 0; start < len(creates); start += batchSize {
			end := start + batchSize
			if end > len(creates) {
				end = len(creates)
			}
			batch := creates[start:end]
			items := make([]sdkgraph.CreateObjectRequest, 0, len(batch))
			for _, cr := range batch {
				key := cr.Key
				props := map[string]any{
					"path":    cr.Route.Path,
					"method":  cr.Route.Method,
					"handler": cr.Route.Handler,
					"domain":  cr.Route.Domain,
					"file":    cr.Route.File,
				}
				if cr.Route.AuthRequired != nil {
					props["auth_required"] = *cr.Route.AuthRequired
				}
				items = append(items, sdkgraph.CreateObjectRequest{
					Type:       "APIEndpoint",
					Key:        &key,
					Properties: props,
				})
			}
			resp, err := g.BulkCreateObjects(ctx, &sdkgraph.BulkCreateObjectsRequest{Items: items})
			if err != nil {
				fmt.Fprintf(stderr, "  error in bulk create batch %d-%d: %v\n", start, end, err)
				createFailed += len(batch)
				continue
			}
			created += resp.Success
			createFailed += resp.Failed
		}
		fmt.Fprintf(stderr, "  Created: %d, Failed: %d\n", created, createFailed)
	}

	if !opts.DryRun && len(updates) > 0 {
		fmt.Fprintf(stderr, "→ Applying %d updates via bulk API...\n", len(updates))
		const batchSize = 100
		for start := 0; start < len(updates); start += batchSize {
			end := start + batchSize
			if end > len(updates) {
				end = len(updates)
			}
			batch := updates[start:end]
			items := make([]sdkgraph.BulkUpdateObjectItem, 0, len(batch))
			for _, u := range batch {
				items = append(items, sdkgraph.BulkUpdateObjectItem{
					ID:         u.GraphID,
					Properties: u.Props,
				})
			}
			resp, err := g.BulkUpdateObjects(ctx, &sdkgraph.BulkUpdateObjectsRequest{Items: items})
			if err != nil {
				fmt.Fprintf(stderr, "  error in bulk update batch %d-%d: %v\n", start, end, err)
				failed += len(batch)
				continue
			}
			applied += resp.Success
			failed += resp.Failed
		}
		fmt.Fprintf(stderr, "  Applied: %d, Failed: %d\n", applied, failed)
	}

	if opts.Defines {
		fmt.Fprintln(stderr, "→ Wiring SourceFile → APIEndpoint defines relationships...")
		wired, skippedDef, missing := wireRouteDefines(ctx, g, codeRoutes, stderr)
		fmt.Fprintf(stderr, "  Wired: %d  Skipped (exists): %d  Unresolved: %d\n", wired, skippedDef, missing)
	}

	return printRoutesTable(out, updates, creates, skipped, stale, applied, failed, created, createFailed, opts.DryRun, len(graphEPs), len(codeRoutes), len(aliasRoutes))
}

func printRoutesTable(out io.Writer, updates []UpdateRecord, creates []CreateRecord, skipped []MatchResult, stale []*sdkgraph.GraphObject, applied, failed, created, createFailed int, dryRun bool, graphTotal, codeTotal, aliasTotal int) error {
	dryTag := ""
	if dryRun {
		dryTag = " [DRY RUN]"
	}
	fmt.Fprintf(out, "┌─ GRAPH SYNC ROUTES%s\n", dryTag)
	fmt.Fprintf(out, "┌─ SUMMARY\n")
	fmt.Fprintf(out, "  Graph endpoints : %d\n", graphTotal)
	fmt.Fprintf(out, "  Code routes     : %d\n", codeTotal)
	fmt.Fprintf(out, "  Aliases skipped : %d  (same handler, multiple paths)\n", aliasTotal)
	fmt.Fprintf(out, "  New to create   : %d\n", len(creates))
	fmt.Fprintf(out, "  Updates needed  : %d\n", len(updates))
	if !dryRun {
		fmt.Fprintf(out, "  Created         : %d\n", created)
		fmt.Fprintf(out, "  Create failed   : %d\n", createFailed)
		fmt.Fprintf(out, "  Applied         : %d\n", applied)
		fmt.Fprintf(out, "  Failed          : %d\n", failed)
	}
	fmt.Fprintf(out, "  Skipped         : %d  (ambiguous)\n", len(skipped))
	fmt.Fprintf(out, "  Stale in graph  : %d  (graph endpoint, no code route found)\n", len(stale))
	fmt.Fprintln(out)

	if len(creates) > 0 {
		label := "┌─ NEW ENDPOINTS (will be created)"
		if dryRun {
			label = "┌─ NEW ENDPOINTS (dry run — would create)"
		}
		fmt.Fprintf(out, "%s\n", label)
		t := tablewriter.NewWriter(out)
		t.Header("KEY", "DOMAIN", "METHOD", "PATH", "HANDLER", "FILE")
		t.Configure(func(cfg *tablewriter.Config) {
			cfg.Behavior.TrimSpace = tw.On
			cfg.Row.ColMaxWidths.PerColumn = map[int]int{3: 55, 5: 45}
		})
		for _, cr := range creates {
			t.Append([]string{cr.Key, cr.Route.Domain, cr.Route.Method, cr.Route.Path, cr.Route.Handler, cr.Route.File})
		}
		t.Render()
		fmt.Fprintln(out)
	}

	if len(updates) > 0 {
		fmt.Fprintf(out, "┌─ UPDATES\n")
		t := tablewriter.NewWriter(out)
		t.Header("KEY", "PATH", "METHOD", "FILE")
		t.Configure(func(cfg *tablewriter.Config) {
			cfg.Behavior.TrimSpace = tw.On
			cfg.Row.Alignment.PerColumn = []tw.Align{tw.AlignLeft, tw.AlignLeft, tw.AlignCenter, tw.AlignLeft}
			cfg.Row.ColMaxWidths.PerColumn = map[int]int{1: 60, 3: 55}
		})
		for _, u := range updates {
			t.Append([]string{u.Key, fmt.Sprint(u.Props["path"]), fmt.Sprint(u.Props["method"]), fmt.Sprint(u.Props["file"])})
		}
		t.Render()
		fmt.Fprintln(out)
	}

	if len(skipped) > 0 {
		fmt.Fprintf(out, "┌─ SKIPPED (ambiguous — manual review needed)\n")
		t := tablewriter.NewWriter(out)
		t.Header("MATCH", "DOMAIN", "METHOD", "PATH", "HANDLER", "NOTE")
		t.Configure(func(cfg *tablewriter.Config) {
			cfg.Behavior.TrimSpace = tw.On
			cfg.Row.ColMaxWidths.PerColumn = map[int]int{3: 55, 5: 40}
		})
		for _, s := range skipped {
			note := s.MatchType
			if s.Ambiguous {
				note += " (ambiguous: " + s.GraphKey + ")"
			}
			t.Append([]string{s.MatchType, s.Route.Domain, s.Route.Method, s.Route.Path, s.Route.Handler, note})
		}
		t.Render()
		fmt.Fprintln(out)
	}

	if len(stale) > 0 {
		fmt.Fprintf(out, "┌─ STALE GRAPH ENDPOINTS (no matching code route)\n")
		t := tablewriter.NewWriter(out)
		t.Header("KEY", "DOMAIN", "METHOD", "PATH", "HANDLER")
		t.Configure(func(cfg *tablewriter.Config) {
			cfg.Behavior.TrimSpace = tw.On
			cfg.Row.ColMaxWidths.PerColumn = map[int]int{3: 60}
		})
		for _, ep := range stale {
			t.Append([]string{DerefKey(ep.Key), StrProp(ep, "domain"), StrProp(ep, "method"), StrProp(ep, "path"), StrProp(ep, "handler")})
		}
		t.Render()
	}
	return nil
}

func extractDomain(filePath string) string {
	const marker = "/domain/"
	idx := strings.Index(filePath, marker)
	if idx < 0 {
		return ""
	}
	rest := filePath[idx+len(marker):]
	parts := strings.SplitN(rest, "/", 2)
	if len(parts) == 0 {
		return ""
	}
	return parts[0]
}

func parseRouteFile(path, domain, repoRoot string) ([]CodeRoute, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	relPath, _ := filepath.Rel(repoRoot, path)
	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	rootVars := map[string]bool{"e": true, "r": true, "router": true}
	groupPrefixes := make(map[string]string)
	basePrefix := ""
	groupRe := regexp.MustCompile(`(\w+)\s*:?=\s*(\w+)\.Group\s*\(\s*"([^"]*)"`)
	for _, line := range lines {
		if m := groupRe.FindStringSubmatch(line); m != nil {
			varName, parentVar, groupPath := m[1], m[2], m[3]
			if rootVars[parentVar] {
				groupPrefixes[varName] = groupPath
				if basePrefix == "" {
					basePrefix = groupPath
				}
			} else {
				groupPrefixes[varName] = groupPrefixes[parentVar] + groupPath
			}
		}
	}
	routeRe := regexp.MustCompile(`(\w+)\.(GET|POST|PUT|DELETE|PATCH)\s*\(\s*"([^"]*)"[^,]*,\s*\w+\.(\w+)`)
	var routes []CodeRoute
	for i, line := range lines {
		if m := routeRe.FindStringSubmatch(line); m != nil {
			varName, method, subPath, handler := m[1], m[2], m[3], m[4]
			var prefix string
			if rootVars[varName] {
				prefix = ""
			} else {
				prefix = groupPrefixes[varName]
				if prefix == "" {
					prefix = basePrefix
				}
			}
			fullPath := prefix + subPath
			fullPath = strings.ReplaceAll(fullPath, "//", "/")
			fullPath = strings.TrimRight(fullPath, "/")
			if fullPath == "" {
				fullPath = "/"
			}
			routes = append(routes, CodeRoute{Domain: domain, Method: strings.ToUpper(method), Path: fullPath, Handler: handler, File: relPath, Line: i + 1})
		}
	}
	return routes, nil
}

func wireRouteDefines(
	ctx context.Context,
	g graph.GraphClient,
	codeRoutes []CodeRoute,
	stderr io.Writer,
) (int, int, int) {
	sfObjs, err := ListAllObjects(ctx, g, "SourceFile")
	if err != nil {
		fmt.Fprintf(stderr, "  error fetching SourceFile objects: %v\n", err)
		return 0, 0, 0
	}
	sfByKey := map[string]*sdkgraph.GraphObject{}
	for _, obj := range sfObjs {
		if DerefKey(obj.Key) != "" {
			sfByKey[DerefKey(obj.Key)] = obj
		}
	}

	epObjs, err := ListAllObjects(ctx, g, "APIEndpoint")
	if err != nil {
		fmt.Fprintf(stderr, "  error fetching APIEndpoint objects: %v\n", err)
		return 0, 0, 0
	}
	epByKey := map[string]*sdkgraph.GraphObject{}
	for _, obj := range epObjs {
		if DerefKey(obj.Key) != "" {
			epByKey[DerefKey(obj.Key)] = obj
		}
	}

	existingRels := map[string]bool{}
	rels, _ := ListAllRelationships(ctx, g, "defines")
	for _, r := range rels {
		existingRels[r.SrcID+":"+r.DstID] = true
	}

	var toWire []sdkgraph.CreateRelationshipRequest
	unresolved := 0

	for _, route := range codeRoutes {
		if route.File == "" {
			unresolved++
			continue
		}
		sfKey := relToSourceFileKey(route.File)
		sfObj, ok := sfByKey[sfKey]
		if !ok {
			unresolved++
			continue
		}

		epKey := "ep-" + route.Domain + "-" + strings.ToLower(route.Handler)
		epObj, ok := epByKey[epKey]
		if !ok {
			unresolved++
			continue
		}

		relKey := sfObj.EntityID + ":" + epObj.EntityID
		if existingRels[relKey] {
			continue
		}
		existingRels[relKey] = true
		toWire = append(toWire, sdkgraph.CreateRelationshipRequest{
			Type:   "defines",
			SrcID:  sfObj.EntityID,
			DstID:  epObj.EntityID,
			Upsert: true,
		})
	}

	fmt.Fprintf(stderr, "  Found %d SourceFile→APIEndpoint defines edges to wire\n", len(toWire))

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

func relToSourceFileKey(rel string) string {
	key := strings.NewReplacer("/", "-", ".", "-", "_", "-").Replace(rel)
	key = strings.ToLower(key)
	for strings.Contains(key, "--") {
		key = strings.ReplaceAll(key, "--", "-")
	}
	key = strings.Trim(key, "-")
	return "sf-" + key
}
