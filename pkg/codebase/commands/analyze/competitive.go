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

// CompetitiveOptions controls the competitive analysis output.
type CompetitiveOptions struct {
	// Competitor is an optional competitor key to drill down into.
	Competitor string
	Format     string
}

// RunCompetitiveAnalysis queries the graph and prints a competitive landscape report.
func RunCompetitiveAnalysis(ctx context.Context, gc graph.GraphClient, opts CompetitiveOptions, w io.Writer) error {
	fmt.Fprintln(w, "→ Fetching competitive landscape data...")

	var (
		competitors   []*sdkgraph.GraphObject
		features      []*sdkgraph.GraphObject
		gaps          []*sdkgraph.GraphObject
		initiatives   []*sdkgraph.GraphObject
		trends        []*sdkgraph.GraphObject
		comparisons   []*sdkgraph.GraphObject
		pricing       []*sdkgraph.GraphObject
		integrations  []*sdkgraph.GraphObject

		hasFeatureRels     []*sdkgraph.GraphRelationship
		exposesGapRels     []*sdkgraph.GraphRelationship
		respondsToRels     []*sdkgraph.GraphRelationship
		closesGapRels      []*sdkgraph.GraphRelationship
		capturesTrendRels  []*sdkgraph.GraphRelationship
		impactsRels        []*sdkgraph.GraphRelationship
		usesPricingRels    []*sdkgraph.GraphRelationship
		providesIntgRels   []*sdkgraph.GraphRelationship

		mu       sync.Mutex
		wg       sync.WaitGroup
		fetchErr error
	)

	fetch := func(fn func() error) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if e := fn(); e != nil {
				mu.Lock()
				if fetchErr == nil {
					fetchErr = e
				}
				mu.Unlock()
			}
		}()
	}

	fetch(func() error { r, e := graph.ListAll(ctx, gc, schema.TypeCompetitor); competitors = r; return e })
	fetch(func() error { r, e := graph.ListAll(ctx, gc, schema.TypeCompetitorFeature); features = r; return e })
	fetch(func() error { r, e := graph.ListAll(ctx, gc, schema.TypeFeatureGap); gaps = r; return e })
	fetch(func() error { r, e := graph.ListAll(ctx, gc, schema.TypeStrategicInitiative); initiatives = r; return e })
	fetch(func() error { r, e := graph.ListAll(ctx, gc, schema.TypeMarketTrend); trends = r; return e })
	fetch(func() error { r, e := graph.ListAll(ctx, gc, schema.TypeComparisonPoint); comparisons = r; return e })
	fetch(func() error { r, e := graph.ListAll(ctx, gc, schema.TypePricingModel); pricing = r; return e })
	fetch(func() error { r, e := graph.ListAll(ctx, gc, schema.TypeIntegration); integrations = r; return e })

	fetch(func() error { r, e := graph.ListAllRels(ctx, gc, schema.RelHasFeature); hasFeatureRels = r; return e })
	fetch(func() error { r, e := graph.ListAllRels(ctx, gc, schema.RelExposesGap); exposesGapRels = r; return e })
	fetch(func() error { r, e := graph.ListAllRels(ctx, gc, schema.RelRespondsTo); respondsToRels = r; return e })
	fetch(func() error { r, e := graph.ListAllRels(ctx, gc, schema.RelClosesGap); closesGapRels = r; return e })
	fetch(func() error { r, e := graph.ListAllRels(ctx, gc, schema.RelCapturesTrend); capturesTrendRels = r; return e })
	fetch(func() error { r, e := graph.ListAllRels(ctx, gc, schema.RelImpacts); impactsRels = r; return e })
	fetch(func() error { r, e := graph.ListAllRels(ctx, gc, schema.RelUsesPricing); usesPricingRels = r; return e })
	fetch(func() error { r, e := graph.ListAllRels(ctx, gc, schema.RelProvidesIntegration); providesIntgRels = r; return e })

	wg.Wait()
	if fetchErr != nil {
		return fetchErr
	}

	// Build lookup maps
	byID := func(objs []*sdkgraph.GraphObject) map[string]*sdkgraph.GraphObject {
		m := make(map[string]*sdkgraph.GraphObject, len(objs))
		for _, o := range objs {
			m[o.EntityID] = o
		}
		return m
	}

	compByID := byID(competitors)
	featByID := byID(features)
	gapByID := byID(gaps)
	initByID := byID(initiatives)
	trendByID := byID(trends)
	cmpByID := byID(comparisons)
	priceByID := byID(pricing)
	intgByID := byID(integrations)

	// Relationship indexes
	compFeatures := multiIndex(hasFeatureRels)
	compGaps := multiIndex(exposesGapRels)
	initCompetitors := multiIndex(respondsToRels)
	initGaps := multiIndex(closesGapRels)
	initTrends := multiIndex(capturesTrendRels)
	trendCompetitors := multiIndex(impactsRels)
	compPricing := singleIndex(usesPricingRels)
	compIntegrations := multiIndex(providesIntgRels)

	// Build competitor → comparisons index (compares_on)
	_ = cmpByID
	_ = initCompetitors
	_ = initGaps
	_ = initTrends
	_ = trendCompetitors

	report := buildReport(
		competitors, features, gaps, initiatives, trends, pricing, integrations, comparisons,
		compByID, featByID, gapByID, initByID, trendByID, priceByID, intgByID,
		compFeatures, compGaps, compPricing, compIntegrations,
		opts.Competitor,
	)

	switch opts.Format {
	case "json":
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(report)
	default:
		printReport(w, report, opts.Competitor)
	}
	return nil
}

// ──────────────────────────────────────────────────────────
// Data model
// ──────────────────────────────────────────────────────────

type CompetitorRow struct {
	Key          string `json:"key"`
	Name         string `json:"name"`
	Category     string `json:"category"`
	Status       string `json:"status"`
	Maturity     string `json:"maturity"`
	OpenSource   bool   `json:"is_open_source"`
	License      string `json:"license,omitempty"`
	TechStack    string `json:"tech_stack,omitempty"`
	RepoURL      string `json:"repo_url,omitempty"`
	PricingModel string `json:"pricing_model,omitempty"`
	Features     []string `json:"features,omitempty"`
	Integrations []string `json:"integrations,omitempty"`
}

type FeatureGapRow struct {
	Key              string   `json:"key"`
	Name             string   `json:"name"`
	CompetitorsHave  []string `json:"competitors_have"`
	Impact           string   `json:"impact"`
	Effort           string   `json:"effort_to_add"`
	InProgress       bool     `json:"is_being_worked_on"`
}

type InitiativeRow struct {
	Key    string `json:"key"`
	Name   string `json:"name"`
	Driver string `json:"competitive_driver"`
	Status string `json:"status"`
	Priority string `json:"priority"`
	Owner  string `json:"owner,omitempty"`
	Target string `json:"target_date,omitempty"`
}

type TrendRow struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Impact      string `json:"impact_on_diane"`
	ImpactLevel string `json:"impact_level"`
	Source      string `json:"source,omitempty"`
}

type CompetitiveReport struct {
	Generated    string          `json:"generated"`
	Competitors  []CompetitorRow `json:"competitors"`
	FeatureGaps  []FeatureGapRow `json:"feature_gaps"`
	Initiatives  []InitiativeRow `json:"strategic_initiatives"`
	Trends       []TrendRow      `json:"market_trends"`
}

// ──────────────────────────────────────────────────────────
// Build report
// ──────────────────────────────────────────────────────────

func buildReport(
	competitors, features, gaps, initiatives, trends, pricing, integrations, _ []*sdkgraph.GraphObject,
	compByID, featByID, gapByID, initByID, trendByID, priceByID, intgByID map[string]*sdkgraph.GraphObject,
	compFeatures, compGaps map[string][]string,
	compPricing map[string]string,
	compIntegrations map[string][]string,
	filterKey string,
) CompetitiveReport {
	_ = features
	_ = featByID

	report := CompetitiveReport{Generated: time.Now().Format("2006-01-02")}

	// Competitors
	for _, c := range competitors {
		key := graph.DerefKey(c.Key)
		if filterKey != "" && key != filterKey {
			continue
		}
		row := CompetitorRow{
			Key:        key,
			Name:       graph.StrProp(c, "name"),
			Category:   graph.StrProp(c, "category"),
			Status:     graph.StrProp(c, "status"),
			Maturity:   graph.StrProp(c, "maturity"),
			OpenSource: boolProp(c, "is_open_source"),
			License:    graph.StrProp(c, "license"),
			TechStack:  graph.StrProp(c, "tech_stack"),
			RepoURL:    graph.StrProp(c, "repo_url"),
		}
		// Pricing
		if pid, ok := compPricing[c.EntityID]; ok {
			if p, ok := priceByID[pid]; ok {
				row.PricingModel = graph.StrProp(p, "model_type")
				pr := graph.StrProp(p, "price_range")
				if pr != "" {
					row.PricingModel += " (" + pr + ")"
				}
			}
		}
		// Features
		for _, fid := range compFeatures[c.EntityID] {
			if f, ok := compByID[fid]; ok {
				_ = f
			}
		}
		// Integrations
		for _, iid := range compIntegrations[c.EntityID] {
			if intg, ok := intgByID[iid]; ok {
				row.Integrations = append(row.Integrations, graph.StrProp(intg, "name"))
			}
		}
		sort.Strings(row.Integrations)
		report.Competitors = append(report.Competitors, row)
	}
	sort.Slice(report.Competitors, func(i, j int) bool {
		return report.Competitors[i].Name < report.Competitors[j].Name
	})

	// Feature Gaps — include all gaps (not filtered by competitor)
	compNameByID := map[string]string{}
	for _, c := range competitors {
		compNameByID[c.EntityID] = graph.StrProp(c, "name")
		if compNameByID[c.EntityID] == "" {
			compNameByID[c.EntityID] = graph.DerefKey(c.Key)
		}
	}
	// Build gap → competitors map (reverse of exposes_gap)
	gapCompetitors := map[string][]string{}
	for cid, gids := range compGaps {
		for _, gid := range gids {
			gapCompetitors[gid] = append(gapCompetitors[gid], compNameByID[cid])
		}
	}

	for _, g := range gaps {
		gkey := graph.DerefKey(g.Key)
		row := FeatureGapRow{
			Key:             gkey,
			Name:            graph.StrProp(g, "name"),
			CompetitorsHave: gapCompetitors[g.EntityID],
			Impact:          graph.StrProp(g, "impact"),
			Effort:          graph.StrProp(g, "effort_to_add"),
			InProgress:      boolProp(g, "is_being_worked_on"),
		}
		sort.Strings(row.CompetitorsHave)
		report.FeatureGaps = append(report.FeatureGaps, row)
	}
	sort.Slice(report.FeatureGaps, func(i, j int) bool {
		pi, pj := impactOrder(report.FeatureGaps[i].Impact), impactOrder(report.FeatureGaps[j].Impact)
		if pi != pj {
			return pi > pj
		}
		return report.FeatureGaps[i].Name < report.FeatureGaps[j].Name
	})

	// Strategic Initiatives
	for _, ini := range initiatives {
		_ = initByID
		row := InitiativeRow{
			Key:      graph.DerefKey(ini.Key),
			Name:     graph.StrProp(ini, "name"),
			Driver:   graph.StrProp(ini, "competitive_driver"),
			Status:   graph.StrProp(ini, "status"),
			Priority: graph.StrProp(ini, "priority"),
			Owner:    graph.StrProp(ini, "owner"),
			Target:   graph.StrProp(ini, "target_date"),
		}
		report.Initiatives = append(report.Initiatives, row)
	}
	sort.Slice(report.Initiatives, func(i, j int) bool {
		pi, pj := impactOrder(report.Initiatives[i].Priority), impactOrder(report.Initiatives[j].Priority)
		if pi != pj {
			return pi > pj
		}
		return report.Initiatives[i].Name < report.Initiatives[j].Name
	})

	// Market Trends
	for _, t := range trends {
		_ = trendByID
		row := TrendRow{
			Key:         graph.DerefKey(t.Key),
			Name:        graph.StrProp(t, "name"),
			Impact:      graph.StrProp(t, "impact_on_diane"),
			ImpactLevel: graph.StrProp(t, "impact_level"),
			Source:      graph.StrProp(t, "source"),
		}
		report.Trends = append(report.Trends, row)
	}
	sort.Slice(report.Trends, func(i, j int) bool {
		pi, pj := impactOrder(report.Trends[i].ImpactLevel), impactOrder(report.Trends[j].ImpactLevel)
		if pi != pj {
			return pi > pj
		}
		return report.Trends[i].Name < report.Trends[j].Name
	})

	return report
}

// ──────────────────────────────────────────────────────────
// Rendering
// ──────────────────────────────────────────────────────────

func printReport(w io.Writer, r CompetitiveReport, filterKey string) {
	title := "COMPETITIVE LANDSCAPE"
	if filterKey != "" {
		title = "COMPETITOR DRILL-DOWN: " + filterKey
	}
	fmt.Fprintf(w, "\n╔══ %s  %s\n\n", title, r.Generated)

	// Competitor matrix
	fmt.Fprintln(w, "── COMPETITORS ─────────────────────────────────────────────────────────")
	if len(r.Competitors) == 0 {
		fmt.Fprintln(w, "  (none)")
	} else {
		fmt.Fprintf(w, "  %-25s %-18s %-12s %-14s %-6s  %s\n", "NAME", "CATEGORY", "STATUS", "MATURITY", "OSS", "PRICING")
		fmt.Fprintln(w, "  "+strings.Repeat("─", 95))
		for _, c := range r.Competitors {
			oss := "no"
			if c.OpenSource {
				oss = "yes"
			}
			pricing := c.PricingModel
			if pricing == "" {
				pricing = "-"
			}
			fmt.Fprintf(w, "  %-25s %-18s %-12s %-14s %-6s  %s\n",
				truncate(c.Name, 24), truncate(c.Category, 17), truncate(c.Status, 11),
				truncate(c.Maturity, 13), oss, pricing)

			if filterKey != "" {
				// Drill-down: show integrations and tech stack
				if c.TechStack != "" {
					fmt.Fprintf(w, "    tech: %s\n", c.TechStack)
				}
				if c.RepoURL != "" {
					fmt.Fprintf(w, "    repo: %s\n", c.RepoURL)
				}
				if len(c.Integrations) > 0 {
					fmt.Fprintf(w, "    integrations: %s\n", strings.Join(c.Integrations, ", "))
				}
			}
		}
	}

	// Feature gaps
	fmt.Fprintln(w, "\n── FEATURE GAPS ────────────────────────────────────────────────────────")
	if len(r.FeatureGaps) == 0 {
		fmt.Fprintln(w, "  (none)")
	} else {
		fmt.Fprintf(w, "  %-28s %-10s %-8s %-11s  %s\n", "GAP", "IMPACT", "EFFORT", "IN PROGRESS", "COMPETITORS HAVE IT")
		fmt.Fprintln(w, "  "+strings.Repeat("─", 95))
		for _, g := range r.FeatureGaps {
			inProg := "no"
			if g.InProgress {
				inProg = "yes"
			}
			have := strings.Join(g.CompetitorsHave, ", ")
			if have == "" {
				have = "-"
			}
			fmt.Fprintf(w, "  %-28s %-10s %-8s %-11s  %s\n",
				truncate(g.Name, 27), truncate(g.Impact, 9), truncate(g.Effort, 7), inProg, have)
		}
	}

	// Strategic initiatives
	fmt.Fprintln(w, "\n── STRATEGIC INITIATIVES ───────────────────────────────────────────────")
	if len(r.Initiatives) == 0 {
		fmt.Fprintln(w, "  (none)")
	} else {
		fmt.Fprintf(w, "  %-28s %-10s %-12s  %s\n", "INITIATIVE", "PRIORITY", "STATUS", "DRIVER")
		fmt.Fprintln(w, "  "+strings.Repeat("─", 95))
		for _, ini := range r.Initiatives {
			driver := ini.Driver
			if driver == "" {
				driver = "-"
			}
			fmt.Fprintf(w, "  %-28s %-10s %-12s  %s\n",
				truncate(ini.Name, 27), truncate(ini.Priority, 9), truncate(ini.Status, 11), driver)
		}
	}

	// Market trends
	fmt.Fprintln(w, "\n── MARKET TRENDS ───────────────────────────────────────────────────────")
	if len(r.Trends) == 0 {
		fmt.Fprintln(w, "  (none)")
	} else {
		fmt.Fprintf(w, "  %-28s %-10s  %s\n", "TREND", "IMPACT LVL", "IMPACT ON DIANE")
		fmt.Fprintln(w, "  "+strings.Repeat("─", 95))
		for _, t := range r.Trends {
			impact := t.Impact
			if impact == "" {
				impact = "-"
			}
			fmt.Fprintf(w, "  %-28s %-10s  %s\n",
				truncate(t.Name, 27), truncate(t.ImpactLevel, 9), impact)
		}
	}

	fmt.Fprintln(w)
}

// ──────────────────────────────────────────────────────────
// Helpers
// ──────────────────────────────────────────────────────────

func multiIndex(rels []*sdkgraph.GraphRelationship) map[string][]string {
	m := make(map[string][]string)
	for _, r := range rels {
		m[r.SrcID] = append(m[r.SrcID], r.DstID)
	}
	return m
}

func singleIndex(rels []*sdkgraph.GraphRelationship) map[string]string {
	m := make(map[string]string)
	for _, r := range rels {
		m[r.SrcID] = r.DstID
	}
	return m
}

func boolProp(obj *sdkgraph.GraphObject, key string) bool {
	if v, ok := obj.Properties[key]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return false
}

func impactOrder(level string) int {
	switch strings.ToLower(level) {
	case "critical":
		return 4
	case "high":
		return 3
	case "medium":
		return 2
	case "low":
		return 1
	}
	return 0
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
