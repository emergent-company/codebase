package check

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"

	sdkgraph "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
	"github.com/emergent-company/codebase/graph"
	"github.com/emergent-company/codebase/output"
	"github.com/emergent-company/codebase/commands/sync"
)

// ObjectType set for "architectural" domain categories that should not
// trigger SCENARIO_NO_ENDPOINTS in functional/domain-space checks.
var scenarioDomainTypes = map[string]bool{
	"action": true, "automation": true, "conversation": true,
	"distribution": true, "knowledge": true, "memory": true,
	"observability": true, "system": true,
}

type LogicFinding struct {
	Check  string `json:"check"`
	Domain string `json:"domain"`
	Object string `json:"object"`
	Detail string `json:"detail"`
	Tier   int    `json:"tier"`
}

type LogicOptions struct {
	Checks  string
	Domain  string
	Verbose bool
	Format  string
}

func RunLogic(ctx context.Context, g graph.GraphClient, opts *LogicOptions, out io.Writer) error {
	enabled := make(map[string]bool)
	if opts.Checks != "" {
		for _, chk := range strings.Split(opts.Checks, ",") {
			enabled[strings.TrimSpace(strings.ToUpper(chk))] = true
		}
	}
	checkEnabled := func(name string) bool {
		if len(enabled) == 0 {
			return true
		}
		return enabled[name]
	}

	listAll := func(typ string) []*sdkgraph.GraphObject {
		var all []*sdkgraph.GraphObject
		cursor := ""
		for {
			resp, _ := g.ListObjects(ctx, &sdkgraph.ListObjectsOptions{Type: typ, Limit: 500, Cursor: cursor})
			if resp == nil || len(resp.Items) == 0 {
				break
			}
			all = append(all, resp.Items...)
			if resp.NextCursor == nil || *resp.NextCursor == "" {
				break
			}
			cursor = *resp.NextCursor
		}
		return all
	}
	listAllRels := func(typ string) []*sdkgraph.GraphRelationship {
		var all []*sdkgraph.GraphRelationship
		cursor := ""
		for {
			resp, _ := g.ListRelationships(ctx, &sdkgraph.ListRelationshipsOptions{Type: typ, Limit: 500, Cursor: cursor})
			if resp == nil || len(resp.Items) == 0 {
				break
			}
			all = append(all, resp.Items...)
			if resp.NextCursor == nil || *resp.NextCursor == "" {
				break
			}
			cursor = *resp.NextCursor
		}
		return all
	}

	domains := listAll("Domain")
	endpoints := listAll("APIEndpoint")
	belongsToRels := listAllRels("belongs_to")

	epByDomain := make(map[string][]string)
	for _, ep := range endpoints {
		d := strings.ToLower(sync.StrProp(ep, "domain"))
		epByDomain[d] = append(epByDomain[d], sync.StrProp(ep, "path"))
	}

	svcToDomains := make(map[string]map[string]bool)
	domainToSvcs := make(map[string]map[string]bool)
	domainIDSet := make(map[string]bool)
	for _, d := range domains {
		domainIDSet[d.EntityID] = true
	}
	for _, r := range belongsToRels {
		if domainIDSet[r.DstID] {
			if svcToDomains[r.SrcID] == nil {
				svcToDomains[r.SrcID] = make(map[string]bool)
			}
			svcToDomains[r.SrcID][r.DstID] = true
			if domainToSvcs[r.DstID] == nil {
				domainToSvcs[r.DstID] = make(map[string]bool)
			}
			domainToSvcs[r.DstID][r.SrcID] = true
		}
	}

	var findings []LogicFinding
	add := func(check, domain, object, detail string, tier int) {
		if opts.Domain != "" && !strings.EqualFold(domain, opts.Domain) {
			return
		}
		findings = append(findings, LogicFinding{Check: check, Domain: domain, Object: object, Detail: detail, Tier: tier})
	}

	if checkEnabled("DOMAIN_NO_ENDPOINTS") {
		for _, d := range domains {
			if strings.ToLower(sync.StrProp(d, "type")) == "frontend" {
				continue
			}
			name := strings.ToLower(sync.StrProp(d, "name"))
			if len(epByDomain[name]) == 0 {
				add("DOMAIN_NO_ENDPOINTS", name, name, "domain has no APIEndpoints", 2)
			}
		}
	}

	if checkEnabled("DOMAIN_NO_SERVICE") {
		for _, d := range domains {
			if strings.ToLower(sync.StrProp(d, "type")) == "frontend" {
				continue
			}
			name := strings.ToLower(sync.StrProp(d, "name"))
			if len(domainToSvcs[d.EntityID]) == 0 {
				add("DOMAIN_NO_SERVICE", name, sync.StrProp(d, "name"), "domain has no Service linked via belongs_to", 2)
			}
		}
	}

	// ── Scenario checks ──────────────────────────────────────────
	scenarios := listAll("Scenario")

	scenarioHasStep := make(map[string]bool)
	for _, r := range listAllRels("has_step") {
		scenarioHasStep[r.SrcID] = true
	}

	// SCENARIO_NO_STEPS: scenarios without has_step relationships
	if checkEnabled("SCENARIO_NO_STEPS") {
		for _, s := range scenarios {
			if !scenarioHasStep[s.EntityID] {
				name := sync.StrProp(s, "name")
				key := sync.DerefKey(s.Key)
				dom := sync.StrProp(s, "domain")
				if dom == "" {
					dom = "unknown"
				}
				add("SCENARIO_NO_STEPS", dom, key, fmt.Sprintf("scenario %q has no has_step relationships", name), 1)
			}
		}
	}

	// SCENARIO_NO_DOMAIN: scenarios without a domain property
	if checkEnabled("SCENARIO_NO_DOMAIN") {
		for _, s := range scenarios {
			if sync.StrProp(s, "domain") == "" {
				key := sync.DerefKey(s.Key)
				name := sync.StrProp(s, "name")
				add("SCENARIO_NO_DOMAIN", "unknown", key, fmt.Sprintf("scenario %q missing domain property", name), 2)
			}
		}
	}

	// ── Domain checks ────────────────────────────────────────────
	allDomains := listAll("Domain")

	// DOMAIN_NO_SCENARIOS: domains (functional type, not code domain) with no scenarios
	if checkEnabled("DOMAIN_NO_SCENARIOS") {
		scenarioByDomain := make(map[string]int)
		for _, s := range scenarios {
			d := strings.ToLower(sync.StrProp(s, "domain"))
			scenarioByDomain[d]++
		}
		for _, d := range allDomains {
			// Only flag functional domains (type=scenario or has scenario-pattern name)
			dtype := strings.ToLower(sync.StrProp(d, "type"))
			dname := strings.ToLower(sync.StrProp(d, "name"))
			if dtype != "scenario" && !scenarioDomainTypes[dname] {
				continue
			}
			if scenarioByDomain[dname] == 0 && scenarioByDomain[strings.TrimPrefix(dname, "dom-")] == 0 {
				add("DOMAIN_NO_SCENARIOS", dname, sync.DerefKey(d.Key), fmt.Sprintf("domain %q has no scenarios", sync.StrProp(d, "name")), 2)
			}
		}
	}

	// DOMAIN_DUPLICATE_NAME: domains whose names overlap confusingly
	if checkEnabled("DOMAIN_DUPLICATE_NAME") {
		domainNames := make(map[string][]*sdkgraph.GraphObject)
		for _, d := range allDomains {
			n := strings.ToLower(sync.StrProp(d, "name"))
			domainNames[n] = append(domainNames[n], d)
		}
		checked := make(map[string]bool)
		for _, d := range allDomains {
			n := strings.ToLower(sync.StrProp(d, "name"))
			if checked[n] {
				continue
			}
			for otherName, objs := range domainNames {
				if otherName == n {
					continue
				}
				// Detect: "memory" vs "dom-memory", "graph-api" vs "dom-graph-api"
				stripped := strings.TrimPrefix(otherName, "dom-")
				if stripped == n || strings.TrimPrefix(n, "dom-") == otherName {
					checked[n] = true
					checked[otherName] = true
					add("DOMAIN_DUPLICATE_NAME", n, fmt.Sprintf("%s, %s", sync.DerefKey(d.Key), sync.DerefKey(objs[0].Key)),
						fmt.Sprintf("domains %q (%s) and %q (%s) have overlapping names — may be redundant or need a linking relationship",
							n, sync.DerefKey(d.Key), otherName, sync.DerefKey(objs[0].Key)), 2)
					break
				}
			}
			checked[n] = true
		}
	}

	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Tier != findings[j].Tier {
			return findings[i].Tier < findings[j].Tier
		}
		return findings[i].Check < findings[j].Check
	})

	if opts.Format == "json" {
		return output.JSONTo(out, findings)
	}

	headers := []string{"TIER", "CHECK", "DOMAIN", "OBJECT", "DETAIL"}
	var rows [][]string
	for _, f := range findings {
		rows = append(rows, []string{fmt.Sprintf("T%d", f.Tier), f.Check, f.Domain, f.Object, f.Detail})
	}
	output.TableTo(out, headers, rows)
	return nil
}
