package analyze

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"

	sdkgraph "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
	"github.com/emergent-company/codebase/graph"
	"github.com/emergent-company/codebase/schema"
)

func RunContexts(ctx context.Context, gc graph.GraphClient, w io.Writer, contextFilter, typeFilter string, showEmpty bool, format string) error {
	var (
		contexts  []*sdkgraph.GraphObject
		actions   []*sdkgraph.GraphObject
		occursIn  []*sdkgraph.GraphRelationship
		hasAction []*sdkgraph.GraphRelationship
		mu        sync.Mutex
		wg        sync.WaitGroup
		fetchErr  error
	)

	fetch := func(fn func() error) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if e := fn(); e != nil {
				mu.Lock()
				fetchErr = e
				mu.Unlock()
			}
		}()
	}

	fetch(func() error { r, e := graph.ListAll(ctx, gc, schema.TypeContext); contexts = r; return e })
	fetch(func() error { r, e := graph.ListAll(ctx, gc, schema.TypeAction); actions = r; return e })
	fetch(func() error { r, e := graph.ListAllRels(ctx, gc, schema.RelOccursIn); occursIn = r; return e })
	fetch(func() error { r, e := graph.ListAllRels(ctx, gc, schema.RelHasAction); hasAction = r; return e })

	wg.Wait()
	if fetchErr != nil {
		return fetchErr
	}

	actionByID := map[string]*sdkgraph.GraphObject{}
	for _, a := range actions {
		actionByID[a.EntityID] = a
	}

	stepToCtx := map[string]map[string]bool{}
	for _, r := range occursIn {
		if stepToCtx[r.SrcID] == nil {
			stepToCtx[r.SrcID] = map[string]bool{}
		}
		stepToCtx[r.SrcID][r.DstID] = true
	}

	stepToActions := map[string][]string{}
	for _, r := range hasAction {
		stepToActions[r.SrcID] = append(stepToActions[r.SrcID], r.DstID)
	}

	ctxToActions := map[string]map[string]bool{}
	for stepID, ctxIDs := range stepToCtx {
		for _, aid := range stepToActions[stepID] {
			for ctxID := range ctxIDs {
				if ctxToActions[ctxID] == nil {
					ctxToActions[ctxID] = map[string]bool{}
				}
				ctxToActions[ctxID][aid] = true
			}
		}
	}

	var rows []ContextRow
	for _, c := range contexts {
		key := graph.DerefKey(c.Key)
		ctype := graph.StrProp(c, "context_type")
		if contextFilter != "" && !strings.EqualFold(key, contextFilter) {
			continue
		}
		if typeFilter != "" && !strings.EqualFold(ctype, typeFilter) {
			continue
		}

		var actLabels []string
		for aid := range ctxToActions[c.EntityID] {
			if a, ok := actionByID[aid]; ok {
				lbl := graph.StrProp(a, "name")
				if lbl == "" {
					lbl = graph.StrProp(a, "label")
				}
				if lbl != "" {
					actLabels = append(actLabels, lbl)
				}
			}
		}
		sort.Strings(actLabels)
		if !showEmpty && len(actLabels) == 0 {
			continue
		}

		rows = append(rows, ContextRow{
			Key: key, Name: graph.StrProp(c, "name"), ContextType: ctype, Description: graph.StrProp(c, "description"), Actions: actLabels,
		})
	}

	sort.Slice(rows, func(i, j int) bool {
		if rows[i].ContextType != rows[j].ContextType {
			return rows[i].ContextType < rows[j].ContextType
		}
		return rows[i].Key < rows[j].Key
	})

	if format == "json" {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(rows)
	}

	fmt.Fprintf(w, "\n┌─ CONTEXT → ACTION MAP  (%d contexts)\n\n", len(rows))
	for _, r := range rows {
		fmt.Fprintf(w, "  ┌─ %-45s [%s]\n", r.Key, r.ContextType)
		fmt.Fprintf(w, "  │  name: %s\n", r.Name)
		fmt.Fprintf(w, "  │  actions (%d):\n", len(r.Actions))
		for _, a := range r.Actions {
			fmt.Fprintf(w, "  │    • %s\n", a)
		}
		fmt.Fprintln(w)
	}
	return nil
}

type ContextRow struct {
	Key         string   `json:"key"`
	Name        string   `json:"name"`
	ContextType string   `json:"context_type"`
	Description string   `json:"description"`
	Actions     []string `json:"actions"`
}
