// Package statuscmd implements the `codebase status` command — connection
// health check and project statistics with per-type breakdowns.
package statuscmd

import (
	"context"
	"fmt"
	"os"
	"sort"

	sdkprojects "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/projects"
	sdkgraph "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
	sdkregistry "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/schemaregistry"

	"github.com/mkucharz/codebase/cmd/codebase/internal/config"
	"github.com/spf13/cobra"
)

func NewCmd(flagProjectID *string, flagBranch *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Test connection and show project statistics",
		Long: `Test connection to the Memory Platform and show project statistics
with per-type breakdowns for objects and relationships.`,
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			cfg, err := config.New(*flagProjectID, *flagBranch)
			if err != nil {
				return fmt.Errorf("connection failed: %w", err)
			}

			out := cmd.OutOrStdout()

			serverURL := os.Getenv("MEMORY_SERVER_URL")
			if serverURL == "" {
				serverURL = "https://memory.emergent-company.ai"
			}

			// --- Project name ---
			// Try Projects.Get first; fall back to config-resolved name or ID.
			projectName := cfg.ProjectName
			if projectName == "" {
				if p, err := cfg.SDK.Projects.Get(ctx, cfg.ProjectID, &sdkprojects.GetOptions{}); err == nil {
					projectName = p.Name
				}
			}

			// --- Object types (with counts) ---
			type obTypeCount struct {
				Type  string
				Count int
			}
			type relTypeCount struct {
				Type  string
				Count int
			}

			var objTypes []obTypeCount
			allRelTypes := make(map[string]bool)

			entries, err := cfg.SchemaRegistry.GetProjectTypes(ctx, cfg.ProjectID, &sdkregistry.ListTypesOptions{})
			if err == nil {
				for _, e := range entries {
					if e.ObjectCount > 0 {
						objTypes = append(objTypes, obTypeCount{Type: e.Type, Count: e.ObjectCount})
					}
					// Collect all outgoing relationship types for this object type
					for _, rel := range e.OutgoingRelationships {
						allRelTypes[rel.Type] = true
					}
				}
			}
			// If GetProjectTypes fails (e.g. no schema-registry scope), fall back to total count
			if len(objTypes) == 0 {
				totalObjs, _ := cfg.Graph.CountObjects(ctx, &sdkgraph.CountObjectsOptions{})
				if totalObjs > 0 {
					objTypes = []obTypeCount{{Type: "(all types)", Count: totalObjs}}
				}
			}

			sort.Slice(objTypes, func(i, j int) bool {
				return objTypes[i].Count > objTypes[j].Count
			})

			// --- Relationship counts by type ---
			var relCounts []relTypeCount
			// Sort relationship types for deterministic output
			relTypeNames := make([]string, 0, len(allRelTypes))
			for t := range allRelTypes {
				relTypeNames = append(relTypeNames, t)
			}
			sort.Strings(relTypeNames)

			for _, rt := range relTypeNames {
				if rels, err := cfg.Graph.ListRelationships(ctx, &sdkgraph.ListRelationshipsOptions{
					Type:  rt,
					Limit: 1,
				}); err == nil && rels.Total > 0 {
					relCounts = append(relCounts, relTypeCount{Type: rt, Count: rels.Total})
				}
			}
			// If no relationship types collected via schema registry, try total
			if len(relCounts) == 0 {
				if rels, err := cfg.Graph.ListRelationships(ctx, &sdkgraph.ListRelationshipsOptions{Limit: 1}); err == nil && rels.Total > 0 {
					relCounts = append(relCounts, relTypeCount{Type: "(all types)", Count: rels.Total})
				}
			}

			sort.Slice(relCounts, func(i, j int) bool {
				return relCounts[i].Count > relCounts[j].Count
			})

			// --- Print ---
			shortID := cfg.ProjectID
			if len(shortID) > 8 {
				shortID = shortID[:8] + "..."
			}
			projLine := shortID
			if projectName != "" {
				projLine = fmt.Sprintf("%s (%s)", projectName, shortID)
			}

			fmt.Fprintf(out, "Server:     %s\n", serverURL)
			fmt.Fprintf(out, "Auth:       ✓ OK\n")
			fmt.Fprintf(out, "Project:    %s\n", projLine)
			fmt.Fprintf(out, "\n")

			// Objects by type
			totalObj := 0
			for _, ot := range objTypes {
				totalObj += ot.Count
			}
			fmt.Fprintf(out, "Objects:    %d\n", totalObj)
			if len(objTypes) > 0 && len(objTypes[0].Type) != len("(all types)") {
				maxW := 0
				for _, ot := range objTypes {
					if len(ot.Type) > maxW {
						maxW = len(ot.Type)
					}
				}
				for _, ot := range objTypes {
					fmt.Fprintf(out, "  %-*s  %d\n", maxW, ot.Type, ot.Count)
				}
			}
			fmt.Fprintf(out, "\n")

			// Relationships by type
			totalRel := 0
			for _, rt := range relCounts {
				totalRel += rt.Count
			}
			fmt.Fprintf(out, "Relations:  %d\n", totalRel)
			if len(relCounts) > 0 && len(relCounts[0].Type) != len("(all types)") {
				maxW := 0
				for _, rt := range relCounts {
					if len(rt.Type) > maxW {
						maxW = len(rt.Type)
					}
				}
				for _, rt := range relCounts {
					fmt.Fprintf(out, "  %-*s  %d\n", maxW, rt.Type, rt.Count)
				}
			}

			return nil
		},
	}

	return cmd
}
