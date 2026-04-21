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

type ComplexityReport struct {
	Domain       string   `json:"domain"`
	Endpoints    int      `json:"endpoints"`
	Methods      int      `json:"methods"`
	SQLQueries   int      `json:"sql_queries"`
	Jobs         int      `json:"jobs"`
	Score        int      `json:"score"`
	Tier         string   `json:"tier"`
	Priority     int      `json:"priority,omitempty"`
	PriorityTier string   `json:"priority_tier,omitempty"`
	Actions      []string `json:"actions,omitempty"`
}

type ComplexityOptions struct {
	Domain          string
	Top             int
	MinPriority     int
	Recommendations bool
	Format          string
}

func RunComplexity(ctx context.Context, g graph.GraphClient, opts *ComplexityOptions, out io.Writer) error {
	listAll := func(typ string) []*sdkgraph.GraphObject {
		resp, _ := g.ListObjects(ctx, &sdkgraph.ListObjectsOptions{Type: typ})
		if resp == nil {
			return nil
		}
		return resp.Items
	}
	listAllRels := func(typ string) []*sdkgraph.GraphRelationship {
		resp, _ := g.ListRelationships(ctx, &sdkgraph.ListRelationshipsOptions{Type: typ})
		if resp == nil {
			return nil
		}
		return resp.Items
	}

	services := listAll("Service")
	endpoints := listAll("APIEndpoint")
	methods := listAll("Method")
	sqlqueries := listAll("SQLQuery")
	jobs := listAll("Job")
	testedByRels := listAllRels("tested_by")

	reports := make(map[string]*ComplexityReport)
	getReport := func(domain string) *ComplexityReport {
		domain = strings.ToLower(domain)
		if r, ok := reports[domain]; ok {
			return r
		}
		r := &ComplexityReport{Domain: domain}
		reports[domain] = r
		return r
	}

	for _, ep := range endpoints {
		d := GetDomain(ep)
		if opts.Domain != "" && !strings.EqualFold(d, opts.Domain) {
			continue
		}
		getReport(d).Endpoints++
	}
	for _, m := range methods {
		d := GetDomain(m)
		if opts.Domain != "" && !strings.EqualFold(d, opts.Domain) {
			continue
		}
		getReport(d).Methods++
	}
	for _, sq := range sqlqueries {
		d := GetDomain(sq)
		if opts.Domain != "" && !strings.EqualFold(d, opts.Domain) {
			continue
		}
		getReport(d).SQLQueries++
	}
	for _, j := range jobs {
		d := GetDomain(j)
		if opts.Domain != "" && !strings.EqualFold(d, opts.Domain) {
			continue
		}
		getReport(d).Jobs++
	}

	hasTests := make(map[string]bool)
	for _, r := range testedByRels {
		for _, s := range services {
			if s.EntityID == r.SrcID {
				hasTests[GetDomain(s)] = true
			}
		}
	}

	var finalReports []*ComplexityReport
	for _, r := range reports {
		r.Score = r.Endpoints*3 + r.Methods*2 + r.SQLQueries + r.Jobs*2
		switch {
		case r.Score >= 100:
			r.Tier = "critical"
		case r.Score >= 50:
			r.Tier = "high"
		case r.Score >= 20:
			r.Tier = "medium"
		default:
			r.Tier = "low"
		}

		if opts.Recommendations {
			penalty := 1.0
			if hasTests[r.Domain] {
				penalty = 0.4
			}
			r.Priority = int(float64(r.Score) * penalty)
			switch {
			case r.Priority >= 80:
				r.PriorityTier = "P0"
			case r.Priority >= 40:
				r.PriorityTier = "P1"
			default:
				r.PriorityTier = "P2"
			}

			if !hasTests[r.Domain] && r.Score > 20 {
				r.Actions = append(r.Actions, "Add tests")
			}
			if r.Endpoints > 15 {
				r.Actions = append(r.Actions, "Split domain")
			}
		}

		if r.Priority >= opts.MinPriority {
			finalReports = append(finalReports, r)
		}
	}

	sort.Slice(finalReports, func(i, j int) bool {
		return finalReports[i].Score > finalReports[j].Score
	})

	if opts.Top > 0 && opts.Top < len(finalReports) {
		finalReports = finalReports[:opts.Top]
	}

	if opts.Format == "json" {
		return output.JSONTo(out, finalReports)
	}

	headers := []string{"DOMAIN", "ENDPOINTS", "METHODS", "SQL", "JOBS", "SCORE", "TIER"}
	if opts.Recommendations {
		headers = append(headers, "PRIORITY", "P-TIER", "ACTIONS")
	}

	var rows [][]string
	for _, r := range finalReports {
		row := []string{
			r.Domain,
			fmt.Sprintf("%d", r.Endpoints),
			fmt.Sprintf("%d", r.Methods),
			fmt.Sprintf("%d", r.SQLQueries),
			fmt.Sprintf("%d", r.Jobs),
			fmt.Sprintf("%d", r.Score),
			r.Tier,
		}
		if opts.Recommendations {
			row = append(row, fmt.Sprintf("%d", r.Priority), r.PriorityTier, strings.Join(r.Actions, ", "))
		}
		rows = append(rows, row)
	}
	output.TableTo(out, headers, rows)
	return nil
}

func GetDomain(o *sdkgraph.GraphObject) string {
	if d, ok := o.Properties["domain"].(string); ok && d != "" {
		return d
	}
	name := sync.StrProp(o, "name")
	return strings.ToLower(strings.TrimSuffix(name, "Service"))
}
