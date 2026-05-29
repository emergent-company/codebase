// Package check — graph quality analysis
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
)

// CompletenessReport is a holistic view of graph health.
type CompletenessReport struct {
	ObjectCounts     map[string]int            `json:"object_counts"`
	RelCounts        map[string]int            `json:"relationship_counts"`
	ScenarioHealth   *CategoryHealth           `json:"scenario_health"`
	EndpointHealth   *CategoryHealth           `json:"endpoint_health"`
	ServiceHealth    *CategoryHealth           `json:"service_health"`
	DomainHealth     *CategoryHealth           `json:"domain_health"`
	OverallHealth    float64                   `json:"overall_health_pct"`
	Orphans          []OrphanSummary           `json:"orphans,omitempty"`
	NextActions      []string                  `json:"next_actions"`
}

// CategoryHealth scores a single object category.
type CategoryHealth struct {
	Total    int     `json:"total"`
	Linked   int     `json:"linked"`
	Score    float64 `json:"score_pct"`
	Label    string  `json:"label"`
}

// OrphanSummary describes an object with no relationships.
type OrphanSummary struct {
	Key      string `json:"key"`
	Type     string `json:"type"`
	Domain   string `json:"domain,omitempty"`
	Name     string `json:"name,omitempty"`
}

type CompletenessOptions struct {
	Format string
}

func RunCompleteness(ctx context.Context, g graph.GraphClient, opts *CompletenessOptions, out io.Writer) error {
	// Fetch ALL objects by type
	typeCount := make(map[string]int)
	typeObjects := make(map[string][]*sdkgraph.GraphObject)

	for _, t := range graph.FallbackTypes {
		objs, err := graph.ListAll(ctx, g, t)
		if err != nil {
			continue // skip types that fail
		}
		typeCount[t] = len(objs)
		typeObjects[t] = objs
	}

	// Fetch ALL relationships by type
	relTypes := []string{
		"has_step", "occurs_in", "has_action", "belongs_to",
		"exposes", "handles", "tested_by", "includes",
		"has_field", "references", "requires", "calls", "uses",
		"has_feature", "compares_on", "evaluated_by", "uses_pricing",
		"provides_integration", "exposes_gap", "responds_to",
		"closes_gap", "captures_trend", "impacts", "compares_against", "drives",
		"realizes",
	}
	relCount := make(map[string]int)
	allRels := make([]*sdkgraph.GraphRelationship, 0)
	for _, rt := range relTypes {
		rels, err := graph.ListAllRels(ctx, g, rt)
		if err != nil {
			continue
		}
		relCount[rt] = len(rels)
		allRels = append(allRels, rels...)
	}

	// Build relationship indexes
	srcHasRel := make(map[string]bool)  // object IDs that are src of any relationship
	dstHasRel := make(map[string]bool)  // object IDs that are dst of any relationship
	hasAnyRel := make(map[string]bool)  // object IDs with ANY relationship

	for _, r := range allRels {
		srcHasRel[r.SrcID] = true
		dstHasRel[r.SrcID] = true
		hasAnyRel[r.SrcID] = true
		hasAnyRel[r.DstID] = true
	}

	// Build scenario→has_step index
	scenarios := typeObjects["Scenario"]
	hasStep := make(map[string]bool)
	for _, r := range allRels {
		if r.Type == "has_step" {
			hasStep[r.SrcID] = true
		}
	}

	// Build endpoint→belongs_to index
	endpoints := typeObjects["APIEndpoint"]
	epBelongsTo := make(map[string]bool)
	for _, r := range allRels {
		if r.Type == "belongs_to" {
			epBelongsTo[r.SrcID] = true
		}
	}

	// Build service→exposes index
	services := typeObjects["Service"]
	svcExposes := make(map[string]bool)
	for _, r := range allRels {
		if r.Type == "exposes" {
			svcExposes[r.SrcID] = true
		}
	}

	// Build domain→service index
	domains := typeObjects["Domain"]
	domainIDs := make(map[string]bool)
	for _, d := range domains {
		domainIDs[d.EntityID] = true
	}
	domHasSvc := make(map[string]bool)      // service belongs_to domain
	domRealizes := make(map[string]bool)     // functional domain realizes code domain
	domIsScenario := make(map[string]bool)   // domain type=scenario
	for _, d := range domains {
		if strings.ToLower(graph.StrProp(d, "type")) == "scenario" {
			domIsScenario[d.EntityID] = true
		}
	}
	for _, r := range allRels {
		if r.Type == "belongs_to" && domainIDs[r.DstID] {
			domHasSvc[r.DstID] = true
		}
		if r.Type == "realizes" && domainIDs[r.SrcID] {
			domRealizes[r.SrcID] = true
		}
	}

	scHealth := &CategoryHealth{
		Total:  len(scenarios),
		Linked: countTrue(scenarios, hasStep),
		Label:  "scenarios with has_step",
	}
	epHealth := &CategoryHealth{
		Total:  len(endpoints),
		Linked: countTrue(endpoints, epBelongsTo),
		Label:  "endpoints with belongs_to",
	}
	svcHealth := &CategoryHealth{
		Total:  len(services),
		Linked: countTrue(services, svcExposes),
		Label:  "services with exposes",
	}
	domHealth := &CategoryHealth{
		Total:  len(domains),
		Linked: countDomainLinked(domains, domHasSvc, domIsScenario, domRealizes),
		Label:  "domains with service or realizes",
	}
	
	scHealth.Score = pct(scHealth.Total, scHealth.Linked)
	epHealth.Score = pct(epHealth.Total, epHealth.Linked)
	svcHealth.Score = pct(svcHealth.Total, svcHealth.Linked)
	domHealth.Score = pct(domHealth.Total, domHealth.Linked)

	// Overall = weighted average (scenarios 30%, endpoints 30%, services 20%, domains 20%)
	overall := scHealth.Score*0.30 + epHealth.Score*0.30 + svcHealth.Score*0.20 + domHealth.Score*0.20

	// Find orphans — objects with 0 relationships in either direction (filter well-known systemic types)
	orphans := findOrphans(ctx, g, typeObjects, hasAnyRel)
	sort.Slice(orphans, func(i, j int) bool {
		if orphans[i].Type != orphans[j].Type {
			return orphans[i].Type < orphans[j].Type
		}
		return orphans[i].Key < orphans[j].Key
	})

	// Generate next actions
	var nextActions []string

	if scHealth.Total > 0 && scHealth.Score < 50 {
		nextActions = append(nextActions, fmt.Sprintf("LINK SCENARIOS: %d/%d scenarios have no has_step relationships — populate scenario→APIEndpoint links", scHealth.Total-scHealth.Linked, scHealth.Total))
	}

	// Check scenarios with no domain property
	scNoDomain := 0
	for _, s := range scenarios {
		if graph.StrProp(s, "domain") == "" {
			scNoDomain++
		}
	}
	if scNoDomain > 0 {
		nextActions = append(nextActions, fmt.Sprintf("SET DOMAIN: %d scenarios missing 'domain' property — each scenario should belong to a domain", scNoDomain))
	}

	// Check scenarios with empty key — they should follow scn- pattern
	scBadKey := 0
	for _, s := range scenarios {
		if s.Key != nil && !strings.HasPrefix(*s.Key, "scn-") && !strings.HasPrefix(*s.Key, "s-") {
			scBadKey++
		}
	}
	if scBadKey > 0 {
		nextActions = append(nextActions, fmt.Sprintf("FIX KEYS: %d scenarios use non-standard key prefixes (expected scn- or s-)", scBadKey))
	}

	if epHealth.Total > 0 && epHealth.Score < 50 {
		nextActions = append(nextActions, fmt.Sprintf("LINK ENDPOINTS: %d/%d endpoints lack belongs_to — connect endpoints to their domains", epHealth.Total-epHealth.Linked, epHealth.Total))
	}

	if svcHealth.Total > 0 && svcHealth.Score < 50 {
		nextActions = append(nextActions, fmt.Sprintf("LINK SERVICES: %d/%d services lack exposes — connect services to their endpoints", svcHealth.Total-svcHealth.Linked, svcHealth.Total))
	}

	if domHealth.Total > 0 && domHealth.Score < 50 {
		nextActions = append(nextActions, fmt.Sprintf("LINK DOMAINS: %d/%d domains have no service — connect domains to services via belongs_to", domHealth.Total-domHealth.Linked, domHealth.Total))
	}

	// Check for similar domain names (functional vs code duplication)
	domainNames := make(map[string]string) // lowercase name → key
	for _, d := range domains {
		name := strings.ToLower(graph.StrProp(d, "name"))
		if name != "" && d.Key != nil {
			domainNames[name] = *d.Key
		}
	}
	for _, d := range domains {
		name := strings.ToLower(graph.StrProp(d, "name"))
		for existingName, existingKey := range domainNames {
			if existingName == name {
				continue
			}
			suffixMatch := strings.HasPrefix(existingName, name+"-") || strings.HasPrefix(name, existingName+"-")
			if suffixMatch {
				nextActions = append(nextActions, fmt.Sprintf("MERGE OR LINK: domains %q (%s) and %q (%s) may be duplicates or need a realizes/implements relationship", graph.StrProp(d, "name"), graph.DerefKey(d.Key), existingName, existingKey))
				break
			}
		}
	}

	// Check contexts and actions
	contexts := typeObjects["Context"]
	actions := typeObjects["Action"]
	if len(contexts) == 0 && len(scenarios) > 0 {
		nextActions = append(nextActions, fmt.Sprintf("CREATE CONTEXTS: 0 contexts exist with %d scenarios — contexts map UI surfaces to scenario triggers", len(scenarios)))
	}
	if len(actions) == 0 && len(scenarios) > 0 {
		nextActions = append(nextActions, fmt.Sprintf("CREATE ACTIONS: 0 actions exist with %d scenarios — actions are the atomic user-facing operations within scenario steps", len(scenarios)))
	}

	if len(typeObjects["Competitor"]) > 0 && relCount["exposes_gap"] == 0 {
		nextActions = append(nextActions, "COMPETITIVE GAPS: competitors exist but no exposes_gap relationships — fill in feature gap analysis")
	}
	if len(typeObjects["StrategicInitiative"]) > 0 && relCount["closes_gap"] == 0 {
		nextActions = append(nextActions, "STRATEGIC LINKING: strategic initiatives exist but no closes_gap — connect initiatives to the gaps they address")
	}

	// Check UML readiness: no relates_to relationships means UML will be empty
	if len(typeObjects["Service"]) > 0 && relCount["handles"] == 0 && relCount["exposes"] == 0 {
		nextActions = append(nextActions, "UML READY: services exist but no handles/exposes — connect services to endpoints for UML generation")
	}

	if len(typeObjects["ScenarioStep"]) == 0 && len(scenarios) > 0 {
		nextActions = append(nextActions, "CREATE STEPS: 0 ScenarioStep objects exist — scenarios need steps to trace to code")
	}

	report := &CompletenessReport{
		ObjectCounts:   typeCount,
		RelCounts:      relCount,
		ScenarioHealth: scHealth,
		EndpointHealth: epHealth,
		ServiceHealth:  svcHealth,
		DomainHealth:   domHealth,
		OverallHealth:  overall,
		Orphans:        orphans,
		NextActions:    nextActions,
	}

	if opts.Format == "json" {
		return output.JSONTo(out, report)
	}

	printCompleteness(out, report)
	return nil
}

func printCompleteness(w io.Writer, r *CompletenessReport) {
	fmt.Fprintf(w, "\n╔══ GRAPH COMPLETENESS REPORT\n")
	fmt.Fprintf(w, "║  Overall Health: %.0f%%\n\n", r.OverallHealth)

	// Object counts
	fmt.Fprintf(w, "══ Objects by Type\n")
	var objRows [][]string
	objTypes := sortedKeys(r.ObjectCounts)
	for _, t := range objTypes {
		c := r.ObjectCounts[t]
		if c > 0 {
			objRows = append(objRows, []string{t, fmt.Sprintf("%d", c)})
		}
	}
	output.TableTo(w, []string{"TYPE", "COUNT"}, objRows)
	fmt.Fprintln(w)

	// Relationship counts
	fmt.Fprintf(w, "══ Relationships by Type\n")
	var relRows [][]string
	relTypes := sortedKeys(r.RelCounts)
	for _, t := range relTypes {
		c := r.RelCounts[t]
		if c > 0 {
			relRows = append(relRows, []string{t, fmt.Sprintf("%d", c)})
		}
	}
	output.TableTo(w, []string{"TYPE", "COUNT"}, relRows)
	fmt.Fprintln(w)

	// Health scores
	fmt.Fprintf(w, "══ Health Scores\n")
	healthRows := [][]string{
		{fmt.Sprintf("%.0f%%", r.ScenarioHealth.Score), fmt.Sprintf("%d/%d", r.ScenarioHealth.Linked, r.ScenarioHealth.Total), r.ScenarioHealth.Label},
		{fmt.Sprintf("%.0f%%", r.EndpointHealth.Score), fmt.Sprintf("%d/%d", r.EndpointHealth.Linked, r.EndpointHealth.Total), r.EndpointHealth.Label},
		{fmt.Sprintf("%.0f%%", r.ServiceHealth.Score), fmt.Sprintf("%d/%d", r.ServiceHealth.Linked, r.ServiceHealth.Total), r.ServiceHealth.Label},
		{fmt.Sprintf("%.0f%%", r.DomainHealth.Score), fmt.Sprintf("%d/%d", r.DomainHealth.Linked, r.DomainHealth.Total), r.DomainHealth.Label},
	}
	output.TableTo(w, []string{"SCORE", "COUNT", "METRIC"}, healthRows)
	fmt.Fprintln(w)

	// Orphans
	if len(r.Orphans) > 0 {
		fmt.Fprintf(w, "══ Orphans (%d objects with 0 relationships)\n", len(r.Orphans))
		var orphanRows [][]string
		maxShow := 20
		if len(r.Orphans) < maxShow {
			maxShow = len(r.Orphans)
		}
		for _, o := range r.Orphans[:maxShow] {
			orphanRows = append(orphanRows, []string{o.Type, o.Key, o.Name})
		}
		output.TableTo(w, []string{"TYPE", "KEY", "NAME"}, orphanRows)
		if len(r.Orphans) > maxShow {
			fmt.Fprintf(w, "  ... and %d more orphans\n", len(r.Orphans)-maxShow)
		}
		fmt.Fprintln(w)
	}

	// Next actions
	if len(r.NextActions) > 0 {
		fmt.Fprintf(w, "══ Recommended Actions\n")
		for i, a := range r.NextActions {
			fmt.Fprintf(w, "  %d. %s\n", i+1, a)
		}
		fmt.Fprintln(w)
	}

	fmt.Fprintf(w, "\n")
}

func sortedKeys(m map[string]int) []string {
	var keys []string
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func countTrue(objs []*sdkgraph.GraphObject, linked map[string]bool) int {
	n := 0
	for _, o := range objs {
		if linked[o.EntityID] {
			n++
		}
	}
	return n
}

func countTrueByID(objs []*sdkgraph.GraphObject, linked map[string]bool) int {
	n := 0
	for _, o := range objs {
		if linked[o.EntityID] {
			n++
		}
	}
	return n
}

// countDomainLinked counts domains that are either:
// - backed by a service (code domains via belongs_to)
// - scenario-type domains with a realizes relationship to a code domain
func countDomainLinked(domains []*sdkgraph.GraphObject, hasService, isScenario, hasRealizes map[string]bool) int {
	n := 0
	for _, d := range domains {
		if hasService[d.EntityID] {
			n++
		} else if isScenario[d.EntityID] && hasRealizes[d.EntityID] {
			n++
		}
	}
	return n
}

func pct(total, done int) float64 {
	if total == 0 {
		return 100.0
	}
	return float64(done) / float64(total) * 100.0
}

func findOrphans(ctx context.Context, g graph.GraphClient, typeObjects map[string][]*sdkgraph.GraphObject, hasAnyRel map[string]bool) []OrphanSummary {
	var orphans []OrphanSummary
	for typ, objs := range typeObjects {
		// Skip types that are systemic/structural — Domain, Competitor, etc. often are valid orphans
		if typ == "Domain" || typ == "Constitution" || typ == "Pattern" || typ == "Rule" {
			continue
		}
		for _, o := range objs {
			if !hasAnyRel[o.EntityID] {
				orphans = append(orphans, OrphanSummary{
					Key:    graph.DerefKey(o.Key),
					Type:   typ,
					Name:   graph.StrProp(o, "name"),
					Domain: graph.StrProp(o, "domain"),
				})
			}
		}
	}
	return orphans
}
