package constitution

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"

	sdkgraph "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
	cbgraph "github.com/emergent-company/codebase/graph"
	"github.com/emergent-company/codebase/output"
	"github.com/emergent-company/codebase/schema"
)

func RunRules(ctx context.Context, gc cbgraph.GraphClient, w io.Writer, category, format string) error {
	items, err := cbgraph.ListAll(ctx, gc, schema.TypeRule)
	if err != nil {
		return fmt.Errorf("listing rules: %w", err)
	}

	if category != "" {
		var filtered []*sdkgraph.GraphObject
		for _, r := range items {
			if strings.EqualFold(cbgraph.StrProp(r, "category"), category) {
				filtered = append(filtered, r)
			}
		}
		items = filtered
	}

	sort.Slice(items, func(i, j int) bool {
		ci, cj := cbgraph.StrProp(items[i], "category"), cbgraph.StrProp(items[j], "category")
		if ci != cj {
			return ci < cj
		}
		ki, kj := cbgraph.DerefKey(items[i].Key), cbgraph.DerefKey(items[j].Key)
		return ki < kj
	})

	if format == "json" {
		type row struct {
			Key       string `json:"key"`
			Name      string `json:"name"`
			Category  string `json:"category"`
			Statement string `json:"statement"`
			AppliesTo string `json:"applies_to,omitempty"`
			AutoCheck string `json:"auto_check,omitempty"`
		}
		var out []row
		for _, r := range items {
			out = append(out, row{
				Key:       cbgraph.DerefKey(r.Key),
				Name:      cbgraph.StrProp(r, "name"),
				Category:  cbgraph.StrProp(r, "category"),
				Statement: cbgraph.StrProp(r, "statement"),
				AppliesTo: cbgraph.StrProp(r, "applies_to"),
				AutoCheck: cbgraph.StrProp(r, "auto_check"),
			})
		}
		return output.JSONTo(w, out)
	}

	if len(items) == 0 {
		fmt.Fprintln(w, "No rules found. Run `codebase onboard` to create starter rules, or add one with:")
		fmt.Fprintln(w, "  codebase constitution add-rule --key rule-api-<slug> --name '...' --statement '...' --category api --applies-to APIEndpoint")
		return nil
	}

	headers := []string{"CATEGORY", "KEY", "NAME", "STATEMENT"}
	var rows [][]string
	for _, r := range items {
		rows = append(rows, []string{
			cbgraph.StrProp(r, "category"),
			cbgraph.DerefKey(r.Key),
			cbgraph.StrProp(r, "name"),
			output.Truncate(cbgraph.StrProp(r, "statement"), 60),
		})
	}

	output.TableTo(w, headers, rows)
	fmt.Fprintf(w, "\n%d rules\n", len(items))
	return nil
}

// anyPropStr returns the property value as a string regardless of its stored type.
func anyPropStr(obj *sdkgraph.GraphObject, key string) string {
	if obj.Properties == nil {
		return ""
	}
	switch v := obj.Properties[key].(type) {
	case string:
		return v
	case bool:
		if v {
			return "true"
		}
		return "false"
	case float64:
		return fmt.Sprintf("%g", v)
	case int:
		return fmt.Sprintf("%d", v)
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", v)
	}
}
