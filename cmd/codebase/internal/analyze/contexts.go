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

func newContextsCmd(flagProjectID *string, flagBranch *string, flagFormat *string, flagApp *string) *cobra.Command {
	var (
		flagContext   string
		flagType      string
		flagShowEmpty bool
	)

	cmd := &cobra.Command{
		Use:   "contexts",
		Short: "List contexts and their reachable actions",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.New(*flagProjectID, *flagBranch)
			if err != nil {
				return err
			}
			scope := resolveAppScope(cmd.Context(), cfg.Graph, *flagApp)
			return runContexts(cfg.SDK, flagContext, flagType, flagShowEmpty, scope, *flagFormat)
		},
	}

	cmd.Flags().StringVar(&flagContext, "context", "", "Filter to one context by key")
	cmd.Flags().StringVar(&flagType, "type", "", "Filter by context_type")
	cmd.Flags().BoolVar(&flagShowEmpty, "show-empty", false, "Include contexts with no actions")

	return cmd
}

func runContexts(client *sdk.Client, contextFilter, typeFilter string, showEmpty bool, scope *appScope, format string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	var (
		scenarios []*sdkgraph.GraphObject
		contexts  []*sdkgraph.GraphObject
		actions   []*sdkgraph.GraphObject
		occursIn  []*sdkgraph.GraphRelationship
		hasAction []*sdkgraph.GraphRelationship
		hasStep   []*sdkgraph.GraphRelationship
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

			adapter := graph.NewSDKAdapter(client.Graph)
			fetch(func() error { r, e := cbgraph.ListAll(ctx, adapter, "Scenario"); scenarios = r; return e })
			fetch(func() error { r, e := cbgraph.ListAll(ctx, adapter, "Context"); contexts = r; return e })
			fetch(func() error { r, e := cbgraph.ListAll(ctx, adapter, "Action"); actions = r; return e })
			fetch(func() error { r, e := cbgraph.ListAllRels(ctx, adapter, "occurs_in"); occursIn = r; return e })
			fetch(func() error { r, e := cbgraph.ListAllRels(ctx, adapter, "has_action"); hasAction = r; return e })
			fetch(func() error { r, e := cbgraph.ListAllRels(ctx, adapter, "has_step"); hasStep = r; return e })

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

	// Build context -> domain mapping for app scope filtering
	scenarioByID := map[string]*sdkgraph.GraphObject{}
	for _, sc := range scenarios {
		scenarioByID[sc.EntityID] = sc
	}
	stepToScenario := map[string]string{}
	for _, r := range hasStep {
		stepToScenario[r.DstID] = r.SrcID
	}
	ctxDomains := map[string]map[string]bool{}
	for _, r := range occursIn {
		stepID := r.SrcID
		ctxID := r.DstID
		if scID, ok := stepToScenario[stepID]; ok {
			if sc, ok := scenarioByID[scID]; ok {
				domain := cbgraph.StrProp(sc, "domain")
				if domain == "" {
					domain = domainFromKey(cbgraph.DerefKey(sc.Key))
				}
				if domain != "" {
					if ctxDomains[ctxID] == nil {
						ctxDomains[ctxID] = map[string]bool{}
					}
					ctxDomains[ctxID][strings.ToLower(domain)] = true
				}
			}
		}
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
		key := cbgraph.DerefKey(c.Key)
		ctype := cbgraph.StrProp(c, "context_type")
		if contextFilter != "" && !strings.EqualFold(key, contextFilter) {
			continue
		}
		if typeFilter != "" && !strings.EqualFold(ctype, typeFilter) {
			continue
		}

		// Filter by app scope: check if any domain linked to this context passes
		if scope != nil && scope.Domains != nil {
			domains := ctxDomains[c.EntityID]
			passes := len(domains) == 0 // pass if no domain info
			for d := range domains {
				if scope.domainPasses(d) {
					passes = true
					break
				}
			}
			if !passes {
				continue
			}
		}

		var actLabels []string
		for aid := range ctxToActions[c.EntityID] {
			if a, ok := actionByID[aid]; ok {
				lbl := cbgraph.StrProp(a, "name")
				if lbl == "" {
					lbl = cbgraph.StrProp(a, "label")
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
			Key: key, Name: cbgraph.StrProp(c, "name"), ContextType: ctype, Description: cbgraph.StrProp(c, "description"), Actions: actLabels,
		})
	}

	sort.Slice(rows, func(i, j int) bool {
		if rows[i].ContextType != rows[j].ContextType {
			return rows[i].ContextType < rows[j].ContextType
		}
		return rows[i].Key < rows[j].Key
	})

	if format == "json" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(rows)
	}

	fmt.Printf("\n┌─ CONTEXT → ACTION MAP  (%d contexts)\n\n", len(rows))
	for _, r := range rows {
		fmt.Printf("  ┌─ %-45s [%s]\n", r.Key, r.ContextType)
		fmt.Printf("  │  name: %s\n", r.Name)
		fmt.Printf("  │  actions (%d):\n", len(r.Actions))
		for _, a := range r.Actions {
			fmt.Printf("  │    • %s\n", a)
		}
		fmt.Println()
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
