package graph

import (
	"context"
	"fmt"
	"io"
	"strings"

	sdkgraph "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
	cbgraph "github.com/emergent-company/codebase/graph"
	"github.com/emergent-company/codebase/output"
)

// QueryOptions controls natural-language graph search behaviour.
type QueryOptions struct {
	// Types filters results to these object types (comma-separated or slice).
	Types []string
	// Limit caps the number of results (default 20).
	Limit int
	// Mode selects the search backend: "hybrid" (default) or "fts".
	Mode string
}

// RunQuery performs a hybrid (FTS + vector) search against the graph and
// writes results to w in the requested format.
func RunQuery(ctx context.Context, gc cbgraph.GraphClient, w io.Writer, query, format string, opts QueryOptions) error {
	if query == "" {
		return fmt.Errorf("query must not be empty")
	}
	if opts.Limit <= 0 {
		opts.Limit = 20
	}
	if opts.Mode == "" {
		opts.Mode = "hybrid"
	}

	var items []*sdkgraph.SearchResultItem
	var total int

	switch opts.Mode {
	case "fts":
		resp, err := gc.FTSSearch(ctx, &sdkgraph.FTSSearchOptions{
			Query: query,
			Types: opts.Types,
			Limit: opts.Limit,
		})
		if err != nil {
			return fmt.Errorf("fts search: %w", err)
		}
		items = resp.Data
		total = resp.Total

	default: // hybrid
		resp, err := gc.HybridSearch(ctx, &sdkgraph.HybridSearchRequest{
			Query: query,
			Types: opts.Types,
			Limit: opts.Limit,
		})
		if err != nil {
			return fmt.Errorf("hybrid search: %w", err)
		}
		items = resp.Data
		total = resp.Total
	}

	if format == "json" {
		type jsonRow struct {
			Key   string         `json:"key"`
			Type  string         `json:"type"`
			Score float32        `json:"score"`
			Props map[string]any `json:"properties,omitempty"`
		}
		var rows []jsonRow
		for _, item := range items {
			if item.Object == nil {
				continue
			}
			rows = append(rows, jsonRow{
				Key:   cbgraph.DerefKey(item.Object.Key),
				Type:  item.Object.Type,
				Score: item.Score,
				Props: item.Object.Properties,
			})
		}
		return output.JSONTo(w, rows)
	}

	// Table output
	fmt.Fprintf(w, "Query: %q  mode=%s  results=%d", query, opts.Mode, len(items))
	if total > len(items) {
		fmt.Fprintf(w, " (of %d)", total)
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, strings.Repeat("─", 72))

	if len(items) == 0 {
		fmt.Fprintln(w, "No results.")
		return nil
	}

	headers := []string{"SCORE", "TYPE", "KEY", "NAME"}
	var rows [][]string
	for _, item := range items {
		if item.Object == nil {
			continue
		}
		name := ""
		if item.Object.Properties != nil {
			if v, ok := item.Object.Properties["name"].(string); ok {
				name = output.Truncate(v, 48)
			}
		}
		rows = append(rows, []string{
			fmt.Sprintf("%.3f", item.Score),
			item.Object.Type,
			cbgraph.DerefKey(item.Object.Key),
			name,
		})
	}
	output.TableTo(w, headers, rows)
	return nil
}
