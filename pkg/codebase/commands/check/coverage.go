package check

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	sdkgraph "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
	"github.com/emergent-company/codebase/graph"
	"github.com/emergent-company/codebase/output"
	"github.com/emergent-company/codebase/commands/sync"
)

type CoverageReport struct {
	Domain      string   `json:"domain"`
	Services    []string `json:"services"`
	Methods     int      `json:"methods"`
	Endpoints   int      `json:"endpoints"`
	TestSuites  int      `json:"test_suites"`
	CoveragePct float64  `json:"coverage_pct"`
	RiskScore   int      `json:"risk_score"`
	Notes       string   `json:"notes"`
}

type CoverageOptions struct {
	Domain      string
	MinCoverage int
	Sort        string
	FailOnRisk  bool
	Format      string
}

func RunCoverage(ctx context.Context, g graph.GraphClient, opts *CoverageOptions, out io.Writer) error {
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
	methods := listAll("Method")
	endpoints := listAll("APIEndpoint")
	jobs := listAll("Job")
	testedByRels := listAllRels("tested_by")

	testedByMap := make(map[string][]string)
	for _, r := range testedByRels {
		testedByMap[r.SrcID] = append(testedByMap[r.SrcID], r.DstID)
	}

	reports := make(map[string]*CoverageReport)
	getReport := func(domain string) *CoverageReport {
		domain = strings.ToLower(domain)
		if r, ok := reports[domain]; ok {
			return r
		}
		r := &CoverageReport{Domain: domain}
		reports[domain] = r
		return r
	}

	for _, s := range services {
		d := GetDomain(s)
		if opts.Domain != "" && !strings.EqualFold(d, opts.Domain) {
			continue
		}
		r := getReport(d)
		r.Services = append(r.Services, sync.StrProp(s, "name"))
		if tsIDs, ok := testedByMap[s.EntityID]; ok {
			r.TestSuites += len(tsIDs)
			r.CoveragePct = 100
		}
	}

	for _, m := range methods {
		d := GetDomain(m)
		if opts.Domain != "" && !strings.EqualFold(d, opts.Domain) {
			continue
		}
		getReport(d).Methods++
	}

	for _, ep := range endpoints {
		d := GetDomain(ep)
		if opts.Domain != "" && !strings.EqualFold(d, opts.Domain) {
			continue
		}
		getReport(d).Endpoints++
	}

	jobCount := make(map[string]int)
	for _, j := range jobs {
		d := GetDomain(j)
		jobCount[d]++
	}

	var finalReports []*CoverageReport
	for _, r := range reports {
		score := 0
		if r.CoveragePct < 100 {
			score += 40
		}
		if r.Methods >= 10 {
			score += 50
		} else if r.Methods >= 5 {
			score += 30
		}
		if jobCount[r.Domain] > 0 {
			score += 10
		}
		if score > 100 {
			score = 100
		}
		r.RiskScore = score

		if r.CoveragePct >= float64(opts.MinCoverage) {
			finalReports = append(finalReports, r)
		}
	}

	sort.Slice(finalReports, func(i, j int) bool {
		switch opts.Sort {
		case "coverage":
			return finalReports[i].CoveragePct < finalReports[j].CoveragePct
		case "methods":
			return finalReports[i].Methods > finalReports[j].Methods
		case "risk":
			return finalReports[i].RiskScore > finalReports[j].RiskScore
		default:
			return finalReports[i].Domain < finalReports[j].Domain
		}
	})

	if opts.Format == "json" {
		return output.JSONTo(out, finalReports)
	}

	headers := []string{"DOMAIN", "SERVICES", "METHODS", "ENDPOINTS", "TESTS", "COVERAGE", "RISK", "NOTES"}
	var rows [][]string
	for _, r := range finalReports {
		rows = append(rows, []string{
			r.Domain,
			fmt.Sprintf("%d", len(r.Services)),
			fmt.Sprintf("%d", r.Methods),
			fmt.Sprintf("%d", r.Endpoints),
			fmt.Sprintf("%d", r.TestSuites),
			fmt.Sprintf("%.0f%%", r.CoveragePct),
			fmt.Sprintf("%d", r.RiskScore),
			r.Notes,
		})
	}
	output.TableTo(out, headers, rows)

	if opts.FailOnRisk {
		for _, r := range finalReports {
			if r.RiskScore >= 60 && r.CoveragePct < 50 {
				os.Exit(2)
			}
		}
	}

	return nil
}
