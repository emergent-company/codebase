package analyzecmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk"
	sdkgraph "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
	"github.com/mkucharz/codebase/cmd/codebase/internal/config"
	"github.com/mkucharz/codebase/cmd/codebase/internal/graph"
	cbgraph "github.com/emergent-company/codebase/graph"
	"github.com/spf13/cobra"
)

func newCompetitiveCmd(flagProjectID *string, flagBranch *string, flagFormat *string) *cobra.Command {
	var flagCompetitor string

	cmd := &cobra.Command{
		Use:   "competitive",
		Short: "Analyze competitive landscape: matrix, gaps, initiatives, trends",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.New(*flagProjectID, *flagBranch)
			if err != nil {
				return err
			}
			return runCompetitive(cfg.SDK, flagCompetitor, *flagFormat)
		},
	}

	cmd.Flags().StringVar(&flagCompetitor, "competitor", "", "Drill down into a single competitor by key")
	return cmd
}

func runCompetitive(client *sdk.Client, filterKey, format string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	fmt.Fprintln(os.Stderr, "→ Fetching competitive landscape data...")

	adapter := graph.NewSDKAdapter(client.Graph)

	var (
		competitors  []*sdkgraph.GraphObject
		gaps         []*sdkgraph.GraphObject
		initiatives  []*sdkgraph.GraphObject
		trends       []*sdkgraph.GraphObject
		pricing      []*sdkgraph.GraphObject
		integrations []*sdkgraph.GraphObject

		exposesGapRels   []*sdkgraph.GraphRelationship
		usesPricingRels  []*sdkgraph.GraphRelationship
		providesIntgRels []*sdkgraph.GraphRelationship

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

	fetch(func() error { r, e := cbgraph.ListAll(ctx, adapter, "Competitor"); competitors = r; return e })
	fetch(func() error { r, e := cbgraph.ListAll(ctx, adapter, "FeatureGap"); gaps = r; return e })
	fetch(func() error { r, e := cbgraph.ListAll(ctx, adapter, "StrategicInitiative"); initiatives = r; return e })
	fetch(func() error { r, e := cbgraph.ListAll(ctx, adapter, "MarketTrend"); trends = r; return e })
	fetch(func() error { r, e := cbgraph.ListAll(ctx, adapter, "PricingModel"); pricing = r; return e })
	fetch(func() error { r, e := cbgraph.ListAll(ctx, adapter, "Integration"); integrations = r; return e })
	fetch(func() error { r, e := cbgraph.ListAllRels(ctx, adapter, "exposes_gap"); exposesGapRels = r; return e })
	fetch(func() error { r, e := cbgraph.ListAllRels(ctx, adapter, "uses_pricing"); usesPricingRels = r; return e })
	fetch(func() error { r, e := cbgraph.ListAllRels(ctx, adapter, "provides_integration"); providesIntgRels = r; return e })

	wg.Wait()
	if fetchErr != nil {
		return fetchErr
	}

	// Build lookup maps
	priceByID := map[string]*sdkgraph.GraphObject{}
	for _, p := range pricing {
		priceByID[p.EntityID] = p
	}
	intgByID := map[string]*sdkgraph.GraphObject{}
	for _, i := range integrations {
		intgByID[i.EntityID] = i
	}
	compNameByID := map[string]string{}
	for _, c := range competitors {
		n := cbgraph.StrProp(c, "name")
		if n == "" {
			n = cbgraph.DerefKey(c.Key)
		}
		compNameByID[c.EntityID] = n
	}

	// Relationship indexes
	compPricing := map[string]string{}
	for _, r := range usesPricingRels {
		compPricing[r.SrcID] = r.DstID
	}
	compGaps := map[string][]string{}
	for _, r := range exposesGapRels {
		compGaps[r.SrcID] = append(compGaps[r.SrcID], r.DstID)
	}
	gapCompetitors := map[string][]string{}
	for cid, gids := range compGaps {
		for _, gid := range gids {
			gapCompetitors[gid] = append(gapCompetitors[gid], compNameByID[cid])
		}
	}
	compIntegrations := map[string][]string{}
	for _, r := range providesIntgRels {
		compIntegrations[r.SrcID] = append(compIntegrations[r.SrcID], r.DstID)
	}

	// Build report
	type CompetitorRow struct {
		Key          string
		Name         string
		Category     string
		Status       string
		Maturity     string
		OpenSource   bool
		License      string
		TechStack    string
		RepoURL      string
		PricingModel string
		Integrations []string
	}
	type GapRow struct {
		Name            string
		CompetitorsHave []string
		Impact          string
		Effort          string
		InProgress      bool
	}
	type InitRow struct {
		Name     string
		Driver   string
		Status   string
		Priority string
		Owner    string
		Target   string
	}
	type TrendRow struct {
		Name        string
		Impact      string
		ImpactLevel string
	}

	var compRows []CompetitorRow
	for _, c := range competitors {
		key := cbgraph.DerefKey(c.Key)
		if filterKey != "" && key != filterKey {
			continue
		}
		row := CompetitorRow{
			Key:        key,
			Name:       cbgraph.StrProp(c, "name"),
			Category:   cbgraph.StrProp(c, "category"),
			Status:     cbgraph.StrProp(c, "status"),
			Maturity:   cbgraph.StrProp(c, "maturity"),
			OpenSource: competitiveBoolProp(c, "is_open_source"),
			License:    cbgraph.StrProp(c, "license"),
			TechStack:  cbgraph.StrProp(c, "tech_stack"),
			RepoURL:    cbgraph.StrProp(c, "repo_url"),
		}
		if pid, ok := compPricing[c.EntityID]; ok {
			if p, ok := priceByID[pid]; ok {
				row.PricingModel = cbgraph.StrProp(p, "model_type")
				if pr := cbgraph.StrProp(p, "price_range"); pr != "" {
					row.PricingModel += " (" + pr + ")"
				}
			}
		}
		for _, iid := range compIntegrations[c.EntityID] {
			if intg, ok := intgByID[iid]; ok {
				row.Integrations = append(row.Integrations, cbgraph.StrProp(intg, "name"))
			}
		}
		sort.Strings(row.Integrations)
		compRows = append(compRows, row)
	}
	sort.Slice(compRows, func(i, j int) bool { return compRows[i].Name < compRows[j].Name })

	var gapRows []GapRow
	for _, g := range gaps {
		row := GapRow{
			Name:            cbgraph.StrProp(g, "name"),
			CompetitorsHave: gapCompetitors[g.EntityID],
			Impact:          cbgraph.StrProp(g, "impact"),
			Effort:          cbgraph.StrProp(g, "effort_to_add"),
			InProgress:      competitiveBoolProp(g, "is_being_worked_on"),
		}
		sort.Strings(row.CompetitorsHave)
		gapRows = append(gapRows, row)
	}
	sort.Slice(gapRows, func(i, j int) bool {
		pi, pj := competitiveImpactOrder(gapRows[i].Impact), competitiveImpactOrder(gapRows[j].Impact)
		if pi != pj {
			return pi > pj
		}
		return gapRows[i].Name < gapRows[j].Name
	})

	var initRows []InitRow
	for _, ini := range initiatives {
		initRows = append(initRows, InitRow{
			Name:     cbgraph.StrProp(ini, "name"),
			Driver:   cbgraph.StrProp(ini, "competitive_driver"),
			Status:   cbgraph.StrProp(ini, "status"),
			Priority: cbgraph.StrProp(ini, "priority"),
			Owner:    cbgraph.StrProp(ini, "owner"),
			Target:   cbgraph.StrProp(ini, "target_date"),
		})
	}
	sort.Slice(initRows, func(i, j int) bool {
		pi, pj := competitiveImpactOrder(initRows[i].Priority), competitiveImpactOrder(initRows[j].Priority)
		if pi != pj {
			return pi > pj
		}
		return initRows[i].Name < initRows[j].Name
	})

	var trendRows []TrendRow
	for _, t := range trends {
		trendRows = append(trendRows, TrendRow{
			Name:        cbgraph.StrProp(t, "name"),
			Impact:      cbgraph.StrProp(t, "impact_on_diane"),
			ImpactLevel: cbgraph.StrProp(t, "impact_level"),
		})
	}
	sort.Slice(trendRows, func(i, j int) bool {
		pi, pj := competitiveImpactOrder(trendRows[i].ImpactLevel), competitiveImpactOrder(trendRows[j].ImpactLevel)
		if pi != pj {
			return pi > pj
		}
		return trendRows[i].Name < trendRows[j].Name
	})

	if format == "json" {
		type Report struct {
			Generated   string        `json:"generated"`
			Competitors []CompetitorRow `json:"competitors"`
			FeatureGaps []GapRow      `json:"feature_gaps"`
			Initiatives []InitRow     `json:"strategic_initiatives"`
			Trends      []TrendRow    `json:"market_trends"`
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(Report{
			Generated:   time.Now().Format("2006-01-02"),
			Competitors: compRows,
			FeatureGaps: gapRows,
			Initiatives: initRows,
			Trends:      trendRows,
		})
	}

	// Text output
	title := "COMPETITIVE LANDSCAPE"
	if filterKey != "" {
		title = "COMPETITOR DRILL-DOWN: " + filterKey
	}
	fmt.Printf("\n╔══ %s  %s\n\n", title, time.Now().Format("2006-01-02"))

	fmt.Println("── COMPETITORS ─────────────────────────────────────────────────────────")
	if len(compRows) == 0 {
		fmt.Println("  (none — add competitors with: codebase create competitor <name>)")
	} else {
		fmt.Printf("  %-25s %-18s %-12s %-14s %-6s  %s\n", "NAME", "CATEGORY", "STATUS", "MATURITY", "OSS", "PRICING")
		fmt.Println("  " + strings.Repeat("─", 95))
		for _, c := range compRows {
			oss := "no"
			if c.OpenSource {
				oss = "yes"
			}
			pricing := c.PricingModel
			if pricing == "" {
				pricing = "-"
			}
			fmt.Printf("  %-25s %-18s %-12s %-14s %-6s  %s\n",
				competitiveTruncate(c.Name, 24), competitiveTruncate(c.Category, 17), competitiveTruncate(c.Status, 11),
				competitiveTruncate(c.Maturity, 13), oss, pricing)
			if filterKey != "" {
				if c.TechStack != "" {
					fmt.Printf("    tech: %s\n", c.TechStack)
				}
				if c.RepoURL != "" {
					fmt.Printf("    repo: %s\n", c.RepoURL)
				}
				if len(c.Integrations) > 0 {
					fmt.Printf("    integrations: %s\n", strings.Join(c.Integrations, ", "))
				}
			}
		}
	}

	fmt.Println("\n── FEATURE GAPS ────────────────────────────────────────────────────────")
	if len(gapRows) == 0 {
		fmt.Println("  (none — add gaps with: codebase create featuregap <name>)")
	} else {
		fmt.Printf("  %-28s %-10s %-8s %-11s  %s\n", "GAP", "IMPACT", "EFFORT", "IN PROGRESS", "COMPETITORS HAVE IT")
		fmt.Println("  " + strings.Repeat("─", 95))
		for _, g := range gapRows {
			inProg := "no"
			if g.InProgress {
				inProg = "yes"
			}
			have := strings.Join(g.CompetitorsHave, ", ")
			if have == "" {
				have = "-"
			}
			fmt.Printf("  %-28s %-10s %-8s %-11s  %s\n",
				competitiveTruncate(g.Name, 27), competitiveTruncate(g.Impact, 9), competitiveTruncate(g.Effort, 7), inProg, have)
		}
	}

	fmt.Println("\n── STRATEGIC INITIATIVES ───────────────────────────────────────────────")
	if len(initRows) == 0 {
		fmt.Println("  (none — add initiatives with: codebase create strategicinitiative <name>)")
	} else {
		fmt.Printf("  %-28s %-10s %-12s  %s\n", "INITIATIVE", "PRIORITY", "STATUS", "DRIVER")
		fmt.Println("  " + strings.Repeat("─", 95))
		for _, ini := range initRows {
			driver := ini.Driver
			if driver == "" {
				driver = "-"
			}
			fmt.Printf("  %-28s %-10s %-12s  %s\n",
				competitiveTruncate(ini.Name, 27), competitiveTruncate(ini.Priority, 9), competitiveTruncate(ini.Status, 11), driver)
		}
	}

	fmt.Println("\n── MARKET TRENDS ───────────────────────────────────────────────────────")
	if len(trendRows) == 0 {
		fmt.Println("  (none — add trends with: codebase create markettrend <name>)")
	} else {
		fmt.Printf("  %-28s %-10s  %s\n", "TREND", "IMPACT LVL", "IMPACT ON DIANE")
		fmt.Println("  " + strings.Repeat("─", 95))
		for _, t := range trendRows {
			impact := t.Impact
			if impact == "" {
				impact = "-"
			}
			fmt.Printf("  %-28s %-10s  %s\n",
				competitiveTruncate(t.Name, 27), competitiveTruncate(t.ImpactLevel, 9), impact)
		}
	}

	fmt.Println()
	return nil
}

func competitiveBoolProp(obj *sdkgraph.GraphObject, key string) bool {
	if v, ok := obj.Properties[key]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return false
}

func competitiveImpactOrder(level string) int {
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

func competitiveTruncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
