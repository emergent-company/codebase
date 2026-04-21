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

type APIFinding struct {
	Check   string `json:"check"`
	Domain  string `json:"domain"`
	Method  string `json:"method"`
	Path    string `json:"path"`
	Handler string `json:"handler"`
	Key     string `json:"key"`
	Detail  string `json:"detail"`
}

type APIOptions struct {
	Domain string
	Checks string
	Format string
}

func RunAPI(ctx context.Context, g graph.GraphClient, opts *APIOptions, out io.Writer) error {
	enabled := make(map[string]bool)
	for _, chk := range strings.Split(opts.Checks, ",") {
		enabled[strings.TrimSpace(strings.ToLower(chk))] = true
	}

	epsResp, err := g.ListObjects(ctx, &sdkgraph.ListObjectsOptions{Type: "APIEndpoint"})
	if err != nil {
		return err
	}
	eps := epsResp.Items

	relsResp, err := g.ListRelationships(ctx, &sdkgraph.ListRelationshipsOptions{Type: "handles"})
	if err != nil {
		return err
	}
	rels := relsResp.Items

	hasHandles := make(map[string]bool)
	for _, r := range rels {
		hasHandles[r.DstID] = true
	}

	var findings []APIFinding
	byMethodPath := make(map[string][]*sdkgraph.GraphObject)

	for _, ep := range eps {
		domain := sync.StrProp(ep, "domain")
		if opts.Domain != "" && !strings.EqualFold(domain, opts.Domain) {
			continue
		}

		method := strings.ToUpper(sync.StrProp(ep, "method"))
		path := sync.StrProp(ep, "path")
		handler := sync.StrProp(ep, "handler")
		file := sync.StrProp(ep, "file")
		key := sync.DerefKey(ep.Key)

		f := func(check, detail string) {
			findings = append(findings, APIFinding{
				Check:   check,
				Domain:  domain,
				Method:  method,
				Path:    path,
				Handler: handler,
				Key:     key,
				Detail:  detail,
			})
		}

		if enabled["no_path"] && path == "" {
			f("NO_PATH", "missing path property")
		}
		if enabled["no_method"] && method == "" {
			f("NO_METHOD", "missing method property")
		}
		if enabled["no_handler"] && handler == "" {
			f("NO_HANDLER", "missing handler property")
		}
		if enabled["no_file"] && file == "" {
			f("NO_FILE", "missing file property")
		}
		if enabled["no_domain"] && domain == "" {
			f("NO_DOMAIN", "missing domain property")
		}
		if enabled["orphan"] && !hasHandles[ep.EntityID] {
			f("ORPHAN", "no `handles` relationship to a Service")
		}
		if enabled["duplicate"] && method != "" && path != "" {
			k := method + ":" + path
			byMethodPath[k] = append(byMethodPath[k], ep)
		}
	}

	if enabled["duplicate"] {
		for k, dupEps := range byMethodPath {
			if len(dupEps) <= 1 {
				continue
			}
			parts := strings.SplitN(k, ":", 2)
			method, path := parts[0], parts[1]
			var keys []string
			for _, ep := range dupEps {
				if ep.Key != nil {
					keys = append(keys, *ep.Key)
				}
			}
			detail := fmt.Sprintf("%d endpoints share %s %s: %s", len(dupEps), method, path, strings.Join(keys, ", "))
			for _, ep := range dupEps {
				findings = append(findings, APIFinding{
					Check:   "DUPLICATE",
					Domain:  sync.StrProp(ep, "domain"),
					Method:  method,
					Path:    path,
					Handler: sync.StrProp(ep, "handler"),
					Key:     sync.DerefKey(ep.Key),
					Detail:  detail,
				})
			}
		}
	}

	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Check != findings[j].Check {
			return findings[i].Check < findings[j].Check
		}
		if findings[i].Domain != findings[j].Domain {
			return findings[i].Domain < findings[j].Domain
		}
		return findings[i].Path < findings[j].Path
	})

	if opts.Format == "json" {
		return output.JSONTo(out, findings)
	}

	headers := []string{"CHECK", "DOMAIN", "METHOD", "PATH", "KEY", "DETAIL"}
	var rows [][]string
	for _, f := range findings {
		rows = append(rows, []string{f.Check, f.Domain, f.Method, f.Path, f.Key, f.Detail})
	}
	output.TableTo(out, headers, rows)
	return nil
}
