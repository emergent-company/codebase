package analyze

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	sdkgraph "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
	"github.com/emergent-company/codebase/graph"
	"github.com/emergent-company/codebase/schema"
)

func RunTree(ctx context.Context, gc graph.GraphClient, w io.Writer, domainFilter string, showScenarios, showEndpoints bool, minScenarios int, format string) error {
	fmt.Fprintln(w, "→ Fetching graph data...")

	var (
		domains     []*sdkgraph.GraphObject
		endpoints   []*sdkgraph.GraphObject
		services    []*sdkgraph.GraphObject
		scenarios   []*sdkgraph.GraphObject
		btRels      []*sdkgraph.GraphRelationship
		exposesRels []*sdkgraph.GraphRelationship
		occursRels  []*sdkgraph.GraphRelationship
		mu          sync.Mutex
		wg          sync.WaitGroup
		fetchErr    error
	)

	fetch := func(fn func() error) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if e := fn(); e != nil {
				mu.Lock()
				fetchErr = e
				mu.Unlock()
			}
		}()
	}

	fetch(func() error { r, e := graph.ListAll(ctx, gc, schema.TypeDomain); domains = r; return e })
	fetch(func() error { r, e := graph.ListAll(ctx, gc, schema.TypeAPIEndpoint); endpoints = r; return e })
	fetch(func() error { r, e := graph.ListAll(ctx, gc, schema.TypeService); services = r; return e })
	fetch(func() error { r, e := graph.ListAll(ctx, gc, schema.TypeScenario); scenarios = r; return e })
	fetch(func() error { r, e := graph.ListAllRels(ctx, gc, schema.RelBelongsTo); btRels = r; return e })
	fetch(func() error { r, e := graph.ListAllRels(ctx, gc, schema.RelExposes); exposesRels = r; return e })
	fetch(func() error { r, e := graph.ListAllRels(ctx, gc, schema.RelOccursIn); occursRels = r; return e })

	wg.Wait()
	if fetchErr != nil {
		return fetchErr
	}

	fmt.Fprintf(w, "  Domains:%d Endpoints:%d Services:%d Scenarios:%d belongs_to:%d exposes:%d occurs_in:%d\n",
		len(domains), len(endpoints), len(services), len(scenarios), len(btRels), len(exposesRels), len(occursRels))

	domainByID := map[string]*sdkgraph.GraphObject{}
	domainSlugs := []string{}
	for _, d := range domains {
		domainByID[d.EntityID] = d
		domainSlugs = append(domainSlugs, domainKeySlug(graph.DerefKey(d.Key)))
	}
	sort.Slice(domainSlugs, func(i, j int) bool { return len(domainSlugs[i]) > len(domainSlugs[j]) })

	svcByID := map[string]*sdkgraph.GraphObject{}
	for _, s := range services {
		svcByID[s.EntityID] = s
	}

	domainToSvcs := map[string][]string{}
	svcToDomains := map[string][]string{}
	for _, r := range btRels {
		if _, isDomain := domainByID[r.DstID]; isDomain {
			if _, isSvc := svcByID[r.SrcID]; isSvc {
				domainToSvcs[r.DstID] = append(domainToSvcs[r.DstID], r.SrcID)
				svcToDomains[r.SrcID] = append(svcToDomains[r.SrcID], r.DstID)
			}
		}
	}

	epByID := map[string]*sdkgraph.GraphObject{}
	for _, ep := range endpoints {
		epByID[ep.EntityID] = ep
	}

	epBySlug := map[string][]EndpointInfo{}
	for _, r := range exposesRels {
		domainIDs, ok := svcToDomains[r.SrcID]
		if !ok {
			continue
		}
		ep, ok := epByID[r.DstID]
		if !ok {
			continue
		}
		info := EndpointInfo{
			Method: graph.StrProp(ep, "method"),
			Path:   graph.StrProp(ep, "path"),
		}
		for _, domainID := range domainIDs {
			domain, ok := domainByID[domainID]
			if !ok {
				continue
			}
			slug := domainKeySlug(graph.DerefKey(domain.Key))
			epBySlug[slug] = append(epBySlug[slug], info)
		}
	}

	// Also check for direct belongs_to from APIEndpoint to Domain.
	// This is the simpler and more common pattern — endpoints don't
	// always have a Service in the middle.
	for _, r := range btRels {
		ep, ok := epByID[r.SrcID]
		if !ok {
			continue
		}
		domain, ok := domainByID[r.DstID]
		if !ok {
			continue
		}
		slug := domainKeySlug(graph.DerefKey(domain.Key))
		// Skip if already added via exposes chain (dedup)
		alreadyAdded := false
		for _, existing := range epBySlug[slug] {
			if existing.Method == graph.StrProp(ep, "method") && existing.Path == graph.StrProp(ep, "path") {
				alreadyAdded = true
				break
			}
		}
		if !alreadyAdded {
			epBySlug[slug] = append(epBySlug[slug], EndpointInfo{
				Method: graph.StrProp(ep, "method"),
				Path:   graph.StrProp(ep, "path"),
			})
		}
	}

	scenBySlug := map[string][]ScenarioInfo{}
	scenAssigned := map[string]bool{}

	// First, assign scenarios via occurs_in relationships (most authoritative)
	scnByID := map[string]*sdkgraph.GraphObject{}
	for _, sc := range scenarios {
		scnByID[sc.EntityID] = sc
	}
	for _, r := range occursRels {
		sc, ok := scnByID[r.SrcID]
		if !ok {
			continue
		}
		domain, ok := domainByID[r.DstID]
		if !ok {
			continue
		}
		slug := domainKeySlug(graph.DerefKey(domain.Key))
		scKey := graph.DerefKey(sc.Key)
		statusStr := graph.DerefKey(sc.Status)
		if statusStr == "" {
			statusStr = graph.StrProp(sc, "status")
		}
		switch statusStr {
		case "planned":
			statusStr = "planned"
		case "":
			statusStr = "implemented"
		}
		scenBySlug[slug] = append(scenBySlug[slug], ScenarioInfo{
			Name:   graph.StrProp(sc, "name"),
			Status: statusStr,
			Key:    scKey,
		})
		scenAssigned[sc.EntityID] = true
	}

	// Fall back to key-prefix / domain-property matching for unassigned scenarios
	for _, sc := range scenarios {
		if scenAssigned[sc.EntityID] {
			continue
		}
		scKey := graph.DerefKey(sc.Key)
		slug := scenarioDomainSlug(scKey, domainSlugs)
		if slug == "" {
			slug = strings.ToLower(graph.StrProp(sc, "domain"))
		}
		if slug == "" {
			slug = "(unassigned)"
		}
		statusStr := graph.DerefKey(sc.Status)
		if statusStr == "" {
			statusStr = graph.StrProp(sc, "status")
		}
		switch statusStr {
		case "planned":
			statusStr = "planned"
		case "":
			statusStr = "implemented"
		}
		scenBySlug[slug] = append(scenBySlug[slug], ScenarioInfo{
			Name:   graph.StrProp(sc, "name"),
			Status: statusStr,
			Key:    scKey,
		})
	}

	var nodes []DomainNode
	for _, d := range domains {
		slug := domainKeySlug(graph.DerefKey(d.Key))
		if domainFilter != "" && slug != domainFilter {
			continue
		}

		var svcNames []string
		for _, svcID := range domainToSvcs[d.EntityID] {
			if svc, ok := svcByID[svcID]; ok {
				svcNames = append(svcNames, graph.StrProp(svc, "name"))
			}
		}
		sort.Strings(svcNames)

		eps := epBySlug[slug]
		sort.Slice(eps, func(i, j int) bool {
			if eps[i].Method != eps[j].Method {
				return eps[i].Method < eps[j].Method
			}
			return eps[i].Path < eps[j].Path
		})

		scens := scenBySlug[slug]
		sort.Slice(scens, func(i, j int) bool {
			return scens[i].Name < scens[j].Name
		})

		if minScenarios > 0 && len(scens) < minScenarios {
			continue
		}

		nodes = append(nodes, DomainNode{
			Key:       slug,
			Name:      graph.StrProp(d, "name"),
			Services:  svcNames,
			Endpoints: eps,
			Scenarios: scens,
		})
	}

	sort.Slice(nodes, func(i, j int) bool { return nodes[i].Name < nodes[j].Name })

	if domainFilter == "" {
		unassigned := scenBySlug["(unassigned)"]
		if len(unassigned) > 0 {
			sort.Slice(unassigned, func(i, j int) bool { return unassigned[i].Name < unassigned[j].Name })
			nodes = append(nodes, DomainNode{
				Key:       "(unassigned)",
				Name:      "(Unassigned)",
				Scenarios: unassigned,
			})
		}
	}

	report := Report{
		Generated: time.Now().Format("2006-01-02"),
		Domains:   nodes,
	}

	switch format {
	case "json":
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(report)
	case "markdown":
		printMarkdown(w, report, showScenarios)
	default:
		printTree(w, report, showScenarios, showEndpoints)
	}
	return nil
}

type ScenarioInfo struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Key    string `json:"key"`
}

type DomainNode struct {
	Key       string         `json:"key"`
	Name      string         `json:"name"`
	Services  []string       `json:"services"`
	Endpoints []EndpointInfo `json:"endpoints"`
	Scenarios []ScenarioInfo `json:"scenarios"`
}

type EndpointInfo struct {
	Method string `json:"method"`
	Path   string `json:"path"`
}

type Report struct {
	Generated string       `json:"generated"`
	Domains   []DomainNode `json:"domains"`
}

func domainKeySlug(key string) string {
	return strings.TrimPrefix(strings.TrimPrefix(key, "dom-"), "domain-")
}

var scenarioKeyAliases = map[string]string{
	"org-user":            "org-user",
	"org":                 "org-user",
	"graph-objects":       "graph-objects",
	"graph-relationships": "graph-relationships",
	"graph":               "graph-objects",
}

func scenarioDomainSlug(key string, domainSlugs []string) string {
	rest := key
	if strings.HasPrefix(rest, "scn-") {
		rest = rest[4:]
	} else if strings.HasPrefix(rest, "s-") {
		rest = rest[2:]
	} else {
		return ""
	}
	for _, slug := range domainSlugs {
		if rest == slug || strings.HasPrefix(rest, slug+"-") {
			return slug
		}
	}
	type aliasEntry struct{ prefix, target string }
	aliases := []aliasEntry{}
	for prefix, target := range scenarioKeyAliases {
		aliases = append(aliases, aliasEntry{prefix, target})
	}
	sort.Slice(aliases, func(i, j int) bool { return len(aliases[i].prefix) > len(aliases[j].prefix) })
	for _, a := range aliases {
		if rest == a.prefix || strings.HasPrefix(rest, a.prefix+"-") {
			return a.target
		}
	}
	return ""
}

func statusIcon(s string) string {
	switch s {
	case "planned":
		return "◌"
	case "implemented":
		return "✓"
	default:
		return "·"
	}
}

func printTree(w io.Writer, r Report, showScenarios, showEndpoints bool) {
	totalScen := 0
	totalEP := 0
	implemented := 0
	planned := 0
	for _, d := range r.Domains {
		totalEP += len(d.Endpoints)
		for _, s := range d.Scenarios {
			totalScen++
			if s.Status == "planned" {
				planned++
			} else {
				implemented++
			}
		}
	}

	fmt.Fprintf(w, "\n╔══ FUNCTIONALITY MAP  %s\n", r.Generated)
	fmt.Fprintf(w, "║   %d domains · %d scenarios (%d implemented, %d planned) · %d endpoints\n\n",
		len(r.Domains), totalScen, implemented, planned, totalEP)

	for _, d := range r.Domains {
		implCount := 0
		planCount := 0
		for _, s := range d.Scenarios {
			if s.Status == "planned" {
				planCount++
			} else {
				implCount++
			}
		}

		scenCount := len(d.Scenarios)
		coverage := ""
		if scenCount > 0 {
			pct := 100 * implCount / scenCount
			bar := strings.Repeat("█", pct/10) + strings.Repeat("░", 10-pct/10)
			coverage = fmt.Sprintf(" [%s %d%%]", bar, pct)
		}
		fmt.Fprintf(w, "┌─ %s%s\n", d.Name, coverage)

		if len(d.Services) > 0 {
			fmt.Fprintf(w, "│  ⚙  %s\n", strings.Join(d.Services, ", "))
		}

		if len(d.Endpoints) > 0 {
			fmt.Fprintf(w, "│  ⇄  %d endpoints\n", len(d.Endpoints))
			if showEndpoints {
				for _, ep := range d.Endpoints {
					fmt.Fprintf(w, "│     %-7s %s\n", ep.Method, ep.Path)
				}
			}
		}

		if scenCount > 0 {
			fmt.Fprintf(w, "│  ◈  %d scenarios", scenCount)
			if planCount > 0 {
				fmt.Fprintf(w, " (%d planned)", planCount)
			}
			fmt.Fprintln(w)
			if showScenarios {
				for _, s := range d.Scenarios {
					fmt.Fprintf(w, "│     %s %s\n", statusIcon(s.Status), s.Name)
				}
			}
		}
		fmt.Fprintln(w, "│")
	}
}

func printMarkdown(w io.Writer, r Report, showScenarios bool) {
	fmt.Fprintf(w, "# Functionality Map\n\nGenerated: %s\n\n", r.Generated)
	totalScen := 0
	totalEP := 0
	for _, d := range r.Domains {
		totalEP += len(d.Endpoints)
		totalScen += len(d.Scenarios)
	}
	fmt.Fprintf(w, "**%d domains · %d scenarios · %d endpoints**\n\n", len(r.Domains), totalScen, totalEP)
	fmt.Fprintln(w, "| Domain | Services | Endpoints | Scenarios | Planned |")
	fmt.Fprintln(w, "|--------|----------|-----------|-----------|---------|")
	for _, d := range r.Domains {
		planned := 0
		for _, s := range d.Scenarios {
			if s.Status == "planned" {
				planned++
			}
		}
		fmt.Fprintf(w, "| **%s** | %s | %d | %d | %d |\n", d.Name, strings.Join(d.Services, ", "), len(d.Endpoints), len(d.Scenarios), planned)
	}
	if showScenarios {
		for _, d := range r.Domains {
			if len(d.Scenarios) == 0 {
				continue
			}
			fmt.Fprintf(w, "\n## %s\n\n", d.Name)
			for _, s := range d.Scenarios {
				fmt.Fprintf(w, "- %s %s\n", statusIcon(s.Status), s.Name)
			}
		}
	}
}
