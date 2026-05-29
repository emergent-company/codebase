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

type OrphanOptions struct {
	Domain string
	Format string
}

func RunOrphans(ctx context.Context, g graph.GraphClient, opts *OrphanOptions, out io.Writer) error {
	// Fetch ALL objects by type
	typeObjects := make(map[string][]*sdkgraph.GraphObject)
	for _, t := range graph.FallbackTypes {
		objs, err := graph.ListAll(ctx, g, t)
		if err != nil {
			continue
		}
		typeObjects[t] = objs
	}

	// Fetch ALL relationships
	relTypes := []string{
		"has_step", "occurs_in", "has_action", "belongs_to",
		"exposes", "handles", "tested_by", "includes",
		"has_field", "references", "requires", "calls", "uses",
		"has_feature", "compares_on", "evaluated_by", "uses_pricing",
		"provides_integration", "exposes_gap", "responds_to",
		"closes_gap", "captures_trend", "impacts", "compares_against", "drives",
		"realizes",
	}
	hasAnyRel := make(map[string]bool)
	for _, rt := range relTypes {
		rels, err := graph.ListAllRels(ctx, g, rt)
		if err != nil {
			continue
		}
		for _, r := range rels {
			hasAnyRel[r.SrcID] = true
			hasAnyRel[r.DstID] = true
		}
	}

	// Domain filter support
	domainFilter := strings.ToLower(opts.Domain)

	var orphans []OrphanSummary
	for typ, objs := range typeObjects {
		for _, o := range objs {
			if hasAnyRel[o.EntityID] {
				continue
			}
			oDomain := strings.ToLower(graph.StrProp(o, "domain"))
			if domainFilter != "" && !strings.Contains(oDomain, domainFilter) && !strings.Contains(strings.ToLower(graph.DerefKey(o.Key)), domainFilter) {
				continue
			}
			orphans = append(orphans, OrphanSummary{
				Key:    graph.DerefKey(o.Key),
				Type:   typ,
				Name:   graph.StrProp(o, "name"),
				Domain: graph.StrProp(o, "domain"),
			})
		}
	}

	sort.Slice(orphans, func(i, j int) bool {
		if orphans[i].Type != orphans[j].Type {
			return orphans[i].Type < orphans[j].Type
		}
		return orphans[i].Key < orphans[j].Key
	})

	if opts.Format == "json" {
		return output.JSONTo(out, orphans)
	}

	if len(orphans) == 0 {
		fmt.Fprintf(out, "✓ No orphaned objects found — every graph object has at least one relationship.\n")
		return nil
	}

	fmt.Fprintf(out, "══ Orphaned Objects  (%d objects with 0 relationships)\n\n", len(orphans))
	headers := []string{"TYPE", "KEY", "NAME", "DOMAIN"}
	var rows [][]string
	for _, o := range orphans {
		rows = append(rows, []string{o.Type, o.Key, o.Name, o.Domain})
	}
	output.TableTo(out, headers, rows)
	return nil
}
