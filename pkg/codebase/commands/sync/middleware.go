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
	"github.com/emergent-company/codebase/graph"
	"github.com/olekukonko/tablewriter"
	"github.com/olekukonko/tablewriter/tw"
)

type MiddlewareOptions struct {
	Repo      string
	DryRun    bool
	RouteGlob string
}

type routeMiddleware struct {
	Domain     string
	Handler    string
	Middleware []string
	Scopes     []string
	IsPublic   bool
}

type relRecord struct {
	MiddlewareName string
	MiddlewareID   string
	EndpointKey    string
	EndpointID     string
}

type scopeUpdate struct {
	EndpointID  string
	EndpointKey string
	OldScopes   string
	NewScopes   string
}

type authUpdate struct {
	EndpointID  string
	EndpointKey string
	OldAuth     string
	NewAuth     string
}

func RunMiddleware(ctx context.Context, g graph.GraphClient, opts *MiddlewareOptions, out io.Writer, stderr io.Writer) error {
	fmt.Fprintln(stderr, "→ Fetching Middleware objects from graph...")
	middlewareObjs, err := ListAllObjects(ctx, g, "Middleware")
	if err != nil {
		return fmt.Errorf("listing Middleware: %w", err)
	}
	fmt.Fprintf(stderr, "  Found %d Middleware objects\n", len(middlewareObjs))

	mwByName := make(map[string]string)
	for _, m := range middlewareObjs {
		name := StrProp(m, "name")
		if name != "" {
			mwByName[name] = m.EntityID
		}
	}

	fmt.Fprintln(stderr, "→ Fetching APIEndpoint objects from graph...")
	epObjs, err := ListAllObjects(ctx, g, "APIEndpoint")
	if err != nil {
		return fmt.Errorf("listing APIEndpoints: %w", err)
	}
	fmt.Fprintf(stderr, "  Found %d APIEndpoint objects\n", len(epObjs))

	byKey := make(map[string]*sdkgraph.GraphObject)
	byDomainHandler := make(map[string][]*sdkgraph.GraphObject)
	for _, ep := range epObjs {
		if DerefKey(ep.Key) != "" {
			byKey[DerefKey(ep.Key)] = ep
		}
		domain := StrProp(ep, "domain")
		handler := strings.ToLower(StrProp(ep, "handler"))
		if domain != "" && handler != "" {
			k := domain + ":" + handler
			byDomainHandler[k] = append(byDomainHandler[k], ep)
		}
	}

	fmt.Fprintln(stderr, "→ Fetching existing applies_to relationships...")
	existingRels, err := ListAllRelationships(ctx, g, "applies_to")
	if err != nil {
		return fmt.Errorf("listing applies_to relationships: %w", err)
	}
	fmt.Fprintf(stderr, "  Found %d existing applies_to relationships\n", len(existingRels))

	existingPairs := make(map[string]bool)
	for _, r := range existingRels {
		existingPairs[r.SrcID+":"+r.DstID] = true
	}

	fmt.Fprintln(stderr, "→ Parsing route files...")
	var allRoutes []routeMiddleware
	for _, pattern := range strings.Split(opts.RouteGlob, ",") {
		pattern = strings.TrimSpace(pattern)
		matches, err := filepath.Glob(filepath.Join(opts.Repo, pattern))
		if err != nil {
			fmt.Fprintf(stderr, "  warn: glob %q: %v\n", pattern, err)
			continue
		}
		for _, f := range matches {
			domain := extractDomain(f)
			routes, err := parseRouteFileMiddleware(f, domain)
			if err != nil {
				fmt.Fprintf(stderr, "  warn: parsing %s: %v\n", f, err)
				continue
			}
			allRoutes = append(allRoutes, routes...)
		}
	}
	fmt.Fprintf(stderr, "  Parsed %d route-middleware entries\n", len(allRoutes))

	var toCreate []relRecord
	var scopeUpdates []scopeUpdate
	unmatched := 0
	matched := 0
	queued := make(map[string]bool)
	scopeQueued := make(map[string]bool)
	authQueued := make(map[string]bool)
	var authUpdates []authUpdate
	matchedEpIDs := make(map[string]bool)

	for _, route := range allRoutes {
		ep := resolveEndpoint(route.Domain, route.Handler, byKey, byDomainHandler)
		if ep == nil {
			unmatched++
			continue
		}
		matched++
		epID := ep.EntityID
		epKey := DerefKey(ep.Key)

		for _, mwName := range route.Middleware {
			mwID, ok := mwByName[mwName]
			if !ok {
				fmt.Fprintf(stderr, "  warn: middleware %q not found in graph (route %s.%s)\n", mwName, route.Domain, route.Handler)
				continue
			}
			pairKey := mwID + ":" + epID
			if existingPairs[pairKey] || queued[pairKey] {
				continue
			}
			queued[pairKey] = true
			toCreate = append(toCreate, relRecord{MiddlewareName: mwName, MiddlewareID: mwID, EndpointKey: epKey, EndpointID: epID})
		}

		if len(route.Scopes) > 0 && !scopeQueued[epID] {
			currentScopes := StrProp(ep, "scopes")
			newScopes := strings.Join(route.Scopes, ",")
			if currentScopes != newScopes {
				scopeQueued[epID] = true
				scopeUpdates = append(scopeUpdates, scopeUpdate{EndpointID: epID, EndpointKey: epKey, OldScopes: currentScopes, NewScopes: newScopes})
			}
		}

		matchedEpIDs[epID] = true

		if !authQueued[epID] {
			authQueued[epID] = true
			newAuth := "false"
			if !route.IsPublic {
				newAuth = "true"
			}
			oldAuth := StrProp(ep, "auth_required")
			if oldAuth != newAuth {
				authUpdates = append(authUpdates, authUpdate{
					EndpointID:  epID,
					EndpointKey: epKey,
					OldAuth:     oldAuth,
					NewAuth:     newAuth,
				})
			}
		}
	}

	sort.Slice(toCreate, func(i, j int) bool {
		if toCreate[i].MiddlewareName != toCreate[j].MiddlewareName {
			return toCreate[i].MiddlewareName < toCreate[j].MiddlewareName
		}
		return toCreate[i].EndpointKey < toCreate[j].EndpointKey
	})
	sort.Slice(scopeUpdates, func(i, j int) bool { return scopeUpdates[i].EndpointKey < scopeUpdates[j].EndpointKey })

	for _, ep := range epObjs {
		epID := ep.EntityID
		if matchedEpIDs[epID] || authQueued[epID] {
			continue
		}
		oldAuth := StrProp(ep, "auth_required")
		if oldAuth != "true" {
			authQueued[epID] = true
			authUpdates = append(authUpdates, authUpdate{
				EndpointID:  epID,
				EndpointKey: DerefKey(ep.Key),
				OldAuth:     oldAuth,
				NewAuth:     "true",
			})
		}
	}
	sort.Slice(authUpdates, func(i, j int) bool { return authUpdates[i].EndpointKey < authUpdates[j].EndpointKey })

	createdRels := 0
	failedRels := 0
	if !opts.DryRun && len(toCreate) > 0 {
		fmt.Fprintf(stderr, "→ Creating %d applies_to relationships...\n", len(toCreate))
		const batchSize = 100
		for start := 0; start < len(toCreate); start += batchSize {
			end := start + batchSize
			if end > len(toCreate) {
				end = len(toCreate)
			}
			batch := toCreate[start:end]
			items := make([]sdkgraph.CreateRelationshipRequest, 0, len(batch))
			for _, r := range batch {
				items = append(items, sdkgraph.CreateRelationshipRequest{Type: "applies_to", SrcID: r.MiddlewareID, DstID: r.EndpointID})
			}
			resp, err := g.BulkCreateRelationships(ctx, &sdkgraph.BulkCreateRelationshipsRequest{Items: items})
			if err != nil {
				fmt.Fprintf(stderr, "  error in bulk create batch %d-%d: %v\n", start, end, err)
				failedRels += len(batch)
				continue
			}
			createdRels += resp.Success
			failedRels += resp.Failed
		}
		fmt.Fprintf(stderr, "  Created: %d, Failed: %d\n", createdRels, failedRels)
	}

	appliedScopes := 0
	failedScopes := 0
	if !opts.DryRun && len(scopeUpdates) > 0 {
		fmt.Fprintf(stderr, "→ Updating scopes on %d endpoints...\n", len(scopeUpdates))
		const batchSize = 100
		for start := 0; start < len(scopeUpdates); start += batchSize {
			end := start + batchSize
			if end > len(scopeUpdates) {
				end = len(scopeUpdates)
			}
			batch := scopeUpdates[start:end]
			items := make([]sdkgraph.BulkUpdateObjectItem, 0, len(batch))
			for _, u := range batch {
				items = append(items, sdkgraph.BulkUpdateObjectItem{ID: u.EndpointID, Properties: map[string]any{"scopes": u.NewScopes}})
			}
			resp, err := g.BulkUpdateObjects(ctx, &sdkgraph.BulkUpdateObjectsRequest{Items: items})
			if err != nil {
				fmt.Fprintf(stderr, "  error in scope update batch %d-%d: %v\n", start, end, err)
				failedScopes += len(batch)
				continue
			}
			appliedScopes += resp.Success
			failedScopes += resp.Failed
		}
		fmt.Fprintf(stderr, "  Scope updates applied: %d, Failed: %d\n", appliedScopes, failedScopes)
	}

	appliedAuth := 0
	failedAuth := 0
	if !opts.DryRun && len(authUpdates) > 0 {
		fmt.Fprintf(stderr, "→ Updating auth_required on %d endpoints...\n", len(authUpdates))
		const batchSize = 100
		for start := 0; start < len(authUpdates); start += batchSize {
			end := start + batchSize
			if end > len(authUpdates) {
				end = len(authUpdates)
			}
			batch := authUpdates[start:end]
			items := make([]sdkgraph.BulkUpdateObjectItem, 0, len(batch))
			for _, u := range batch {
				items = append(items, sdkgraph.BulkUpdateObjectItem{ID: u.EndpointID, Properties: map[string]any{"auth_required": u.NewAuth}})
			}
			resp, err := g.BulkUpdateObjects(ctx, &sdkgraph.BulkUpdateObjectsRequest{Items: items})
			if err != nil {
				fmt.Fprintf(stderr, "  error in auth update batch %d-%d: %v\n", start, end, err)
				failedAuth += len(batch)
				continue
			}
			appliedAuth += resp.Success
			failedAuth += resp.Failed
		}
		fmt.Fprintf(stderr, "  Auth updates applied: %d, Failed: %d\n", appliedAuth, failedAuth)
	}

	return printMiddlewareTable(out, toCreate, scopeUpdates, authUpdates, createdRels, failedRels, appliedScopes, failedScopes, appliedAuth, failedAuth, opts.DryRun, len(middlewareObjs), len(epObjs), len(allRoutes), matched, unmatched, len(existingRels))
}

func printMiddlewareTable(out io.Writer, rels []relRecord, scopes []scopeUpdate, authUpdates []authUpdate, createdRels, failedRels, appliedScopes, failedScopes, appliedAuth, failedAuth int, dryRun bool, mwCount, epCount, routesParsed, matched, unmatched, existingRels int) error {
	dryTag := ""
	if dryRun {
		dryTag = " [DRY RUN]"
	}
	fmt.Fprintf(out, "┌─ GRAPH SYNC MIDDLEWARE%s\n", dryTag)
	fmt.Fprintf(out, "┌─ SUMMARY\n")
	fmt.Fprintf(out, "  Middleware objects    : %d\n", mwCount)
	fmt.Fprintf(out, "  Endpoint objects     : %d\n", epCount)
	fmt.Fprintf(out, "  Routes parsed        : %d\n", routesParsed)
	fmt.Fprintf(out, "  Routes matched       : %d\n", matched)
	fmt.Fprintf(out, "  Routes unmatched     : %d\n", unmatched)
	fmt.Fprintf(out, "  Existing rels        : %d\n", existingRels)
	fmt.Fprintf(out, "  Relationships to add : %d\n", len(rels))
	if !dryRun {
		fmt.Fprintf(out, "  Rels created         : %d\n", createdRels)
		fmt.Fprintf(out, "  Rels failed          : %d\n", failedRels)
	}
	fmt.Fprintf(out, "  Scope updates        : %d\n", len(scopes))
	if !dryRun {
		fmt.Fprintf(out, "  Scopes applied       : %d\n", appliedScopes)
		fmt.Fprintf(out, "  Scopes failed        : %d\n", failedScopes)
	}
	fmt.Fprintf(out, "  Auth updates         : %d\n", len(authUpdates))
	if !dryRun {
		fmt.Fprintf(out, "  Auth applied         : %d\n", appliedAuth)
		fmt.Fprintf(out, "  Auth failed          : %d\n", failedAuth)
	}
	fmt.Fprintln(out)
	if len(rels) > 0 {
		fmt.Fprintf(out, "┌─ RELATIONSHIPS TO CREATE (applies_to: Middleware → APIEndpoint)\n")
		t := tablewriter.NewWriter(out)
		t.Header("MIDDLEWARE", "ENDPOINT KEY")
		t.Configure(func(cfg *tablewriter.Config) {
			cfg.Behavior.TrimSpace = tw.On
			cfg.Row.ColMaxWidths.PerColumn = map[int]int{1: 70}
		})
		for _, r := range rels {
			t.Append([]string{r.MiddlewareName, r.EndpointKey})
		}
		t.Render()
		fmt.Fprintln(out)
	}
	if len(scopes) > 0 {
		fmt.Fprintf(out, "┌─ SCOPE UPDATES\n")
		t := tablewriter.NewWriter(out)
		t.Header("ENDPOINT KEY", "OLD SCOPES", "NEW SCOPES")
		t.Configure(func(cfg *tablewriter.Config) {
			cfg.Behavior.TrimSpace = tw.On
			cfg.Row.ColMaxWidths.PerColumn = map[int]int{0: 50, 1: 30, 2: 30}
		})
		for _, s := range scopes {
			t.Append([]string{s.EndpointKey, s.OldScopes, s.NewScopes})
		}
		t.Render()
		fmt.Fprintln(out)
	}
	if len(authUpdates) > 0 {
		fmt.Fprintf(out, "┌─ AUTH UPDATES\n")
		t := tablewriter.NewWriter(out)
		t.Header("ENDPOINT KEY", "OLD AUTH", "NEW AUTH")
		t.Configure(func(cfg *tablewriter.Config) {
			cfg.Behavior.TrimSpace = tw.On
			cfg.Row.ColMaxWidths.PerColumn = map[int]int{0: 50, 1: 15, 2: 15}
		})
		for _, a := range authUpdates {
			t.Append([]string{a.EndpointKey, a.OldAuth, a.NewAuth})
		}
		t.Render()
	}
	return nil
}

type groupState struct {
	prefix     string
	middleware []string
	scopes     []string
}

func parseRouteFileMiddleware(path, domain string) ([]routeMiddleware, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	rootVars := map[string]bool{"e": true, "r": true, "router": true}
	groupRe := regexp.MustCompile(`(\w+)\s*:?=\s*(\w+)\.Group\s*\(\s*"([^"]*)"`)
	useRe := regexp.MustCompile(`(\w+)\.Use\(`)
	routeRe := regexp.MustCompile(`(\w+)\.(GET|POST|PUT|DELETE|PATCH)\s*\(\s*"([^"]*)"[^,]*,\s*\w+\.(\w+)`)
	matchRe := regexp.MustCompile(`(\w+)\.Match\s*\(`)
	requireAuthRe := regexp.MustCompile(`RequireAuth\(\)`)
	requireAPITokenRe := regexp.MustCompile(`RequireAPITokenScopes\("([^"]+)"\)`)
	requireProjectScopeRe := regexp.MustCompile(`RequireProjectScope\(\)`)
	requireProjectIDRe := regexp.MustCompile(`RequireProjectID\(\)`)
	requireScopesRe := regexp.MustCompile(`RequireScopes\(`)
	toolAuditRe := regexp.MustCompile(`ToolAuditMiddleware\(`)
	toolRestrictionRe := regexp.MustCompile(`ToolRestrictionMiddleware\(`)
	groupPrefixes := make(map[string]string)
	for _, line := range lines {
		if m := groupRe.FindStringSubmatch(line); m != nil {
			varName, parentVar, groupPath := m[1], m[2], m[3]
			if rootVars[parentVar] {
				groupPrefixes[varName] = groupPath
			} else {
				groupPrefixes[varName] = groupPrefixes[parentVar] + groupPath
			}
		}
	}
	groups := make(map[string]*groupState)
	for varName, prefix := range groupPrefixes {
		groups[varName] = &groupState{prefix: prefix}
	}
	parentOf := make(map[string]string)
	for _, line := range lines {
		if m := groupRe.FindStringSubmatch(line); m != nil {
			varName, parentVar := m[1], m[2]
			if !rootVars[parentVar] {
				parentOf[varName] = parentVar
			}
		}
	}
	var inheritedMiddleware func(varName string) ([]string, []string)
	inheritedMiddleware = func(varName string) ([]string, []string) {
		parent, ok := parentOf[varName]
		if !ok {
			return nil, nil
		}
		parentMW, parentScopes := inheritedMiddleware(parent)
		g := groups[parent]
		if g == nil {
			return parentMW, parentScopes
		}
		mw := append(parentMW, g.middleware...)
		sc := append(parentScopes, g.scopes...)
		return mw, sc
	}
	extractMW := func(line string) (names []string, scopes []string) {
		if requireAuthRe.MatchString(line) {
			names = append(names, "RequireAuth")
		}
		if m := requireAPITokenRe.FindStringSubmatch(line); m != nil {
			names = append(names, "RequireAPITokenScopes")
			scopes = append(scopes, m[1])
		}
		if requireProjectScopeRe.MatchString(line) {
			names = append(names, "RequireProjectScope")
		}
		if requireProjectIDRe.MatchString(line) {
			names = append(names, "RequireProjectID")
		}
		if requireScopesRe.MatchString(line) {
			names = append(names, "RequireScopes")
		}
		if toolAuditRe.MatchString(line) {
			names = append(names, "ToolAuditMiddleware")
		}
		if toolRestrictionRe.MatchString(line) {
			names = append(names, "ToolRestrictionMiddleware")
		}
		return
	}
	for _, line := range lines {
		if m := groupRe.FindStringSubmatch(line); m != nil {
			varName := m[1]
			if groups[varName] == nil {
				groups[varName] = &groupState{prefix: groupPrefixes[varName]}
			}
		}
		if m := useRe.FindStringSubmatch(line); m != nil {
			varName := m[1]
			if rootVars[varName] {
				continue
			}
			g := groups[varName]
			if g == nil {
				g = &groupState{prefix: groupPrefixes[varName]}
				groups[varName] = g
			}
			names, scopes := extractMW(line)
			g.middleware = append(g.middleware, names...)
			g.scopes = append(g.scopes, scopes...)
		}
	}
	var results []routeMiddleware
	seen := make(map[string]bool)
	processRoute := func(varName, handler string) {
		key := domain + ":" + strings.ToLower(handler)
		if seen[key] {
			return
		}
		seen[key] = true
		inherited, inheritedScopes := inheritedMiddleware(varName)
		var ownMW []string
		var ownScopes []string
		if g := groups[varName]; g != nil {
			ownMW = g.middleware
			ownScopes = g.scopes
		}
		allMW := append(inherited, ownMW...)
		allScopes := append(inheritedScopes, ownScopes...)
		seen2 := make(map[string]bool)
		var dedupMW []string
		for _, m := range allMW {
			if !seen2[m] {
				seen2[m] = true
				dedupMW = append(dedupMW, m)
			}
		}
		seenScope := make(map[string]bool)
		var dedupScopes []string
		for _, s := range allScopes {
			if !seenScope[s] {
				seenScope[s] = true
				dedupScopes = append(dedupScopes, s)
			}
		}
		isPublic := !seen2["RequireAuth"]
		results = append(results, routeMiddleware{Domain: domain, Handler: handler, Middleware: dedupMW, Scopes: dedupScopes, IsPublic: isPublic})
	}
	for _, line := range lines {
		if m := routeRe.FindStringSubmatch(line); m != nil {
			varName, handler := m[1], m[4]
			processRoute(varName, handler)
		}
		if matchRe.MatchString(line) {
			matchHandlerRe := regexp.MustCompile(`Match\s*\([^,]+,\s*"[^"]*"\s*,\s*\w+\.(\w+)`)
			if m := matchHandlerRe.FindStringSubmatch(line); m != nil {
				varName := matchRe.FindStringSubmatch(line)[1]
				handler := m[1]
				processRoute(varName, handler)
			}
		}
	}
	return results, nil
}

func resolveEndpoint(domain, handler string, byKey map[string]*sdkgraph.GraphObject, byDomainHandler map[string][]*sdkgraph.GraphObject) *sdkgraph.GraphObject {
	candidateKey := "ep-" + domain + "-" + strings.ToLower(handler)
	if ep, ok := byKey[candidateKey]; ok {
		return ep
	}
	dhKey := domain + ":" + strings.ToLower(handler)
	if eps, ok := byDomainHandler[dhKey]; ok && len(eps) == 1 {
		return eps[0]
	}
	return nil
}
