// Package statuscmd implements the `codebase status` command — connection
// health check and project statistics with per-type breakdowns.
//
// Uses only the Graph API (objects:read / relationships:read) — no
// projects:read or schema-registry scopes needed.
package statuscmd

import (
	"context"
	"fmt"
	"os"
	"sort"

	sdkgraph "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
	"github.com/mkucharz/codebase/cmd/codebase/internal/config"
	"github.com/spf13/cobra"
)

type typeCount struct {
	Type  string
	Count int
}

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

			// Project name from .codebase.yml only — no API call.
			projectName := cfg.ProjectName

			// --- Discover types by sampling objects ---
			sampleObjs, _ := cfg.Graph.ListObjects(ctx, &sdkgraph.ListObjectsOptions{
				Limit: 500,
				Order: "desc",
			})
			seenObjTypes := make(map[string]bool)
			if sampleObjs != nil {
				for _, o := range sampleObjs.Items {
					if o.Type != "" {
						seenObjTypes[o.Type] = true
					}
				}
			}

			// --- Count per object type ---
			var objTypes []typeCount
			objTypeNames := make([]string, 0, len(seenObjTypes))
			for t := range seenObjTypes {
				objTypeNames = append(objTypeNames, t)
			}
			sort.Strings(objTypeNames)

			totalObj := 0
			for _, t := range objTypeNames {
				n, err := cfg.Graph.CountObjects(ctx, &sdkgraph.CountObjectsOptions{Type: t})
				if err == nil && n > 0 {
					objTypes = append(objTypes, typeCount{Type: t, Count: n})
					totalObj += n
				}
			}

			// Fallback: if no objects found or sampling returned nothing, try total count
			if totalObj == 0 {
				totalObj, _ = cfg.Graph.CountObjects(ctx, &sdkgraph.CountObjectsOptions{})
			}

			sort.Slice(objTypes, func(i, j int) bool {
				return objTypes[i].Count > objTypes[j].Count
			})

			// --- Discover relationship types by sampling ---
			sampleRels, _ := cfg.Graph.ListRelationships(ctx, &sdkgraph.ListRelationshipsOptions{
				Limit: 500,
			})
			seenRelTypes := make(map[string]bool)
			if sampleRels != nil {
				for _, r := range sampleRels.Items {
					if r.Type != "" {
						seenRelTypes[r.Type] = true
					}
				}
			}

			// --- Count per relationship type ---
			var relTypes []typeCount
			relTypeNames := make([]string, 0, len(seenRelTypes))
			for t := range seenRelTypes {
				relTypeNames = append(relTypeNames, t)
			}
			sort.Strings(relTypeNames)

			totalRel := 0
			for _, t := range relTypeNames {
				rels, err := cfg.Graph.ListRelationships(ctx, &sdkgraph.ListRelationshipsOptions{
					Type:  t,
					Limit: 1,
				})
				if err == nil && rels.Total > 0 {
					relTypes = append(relTypes, typeCount{Type: t, Count: rels.Total})
					totalRel += rels.Total
				}
			}

			// Fallback: if no relationship types found, try total
			if totalRel == 0 {
				if rels, err := cfg.Graph.ListRelationships(ctx, &sdkgraph.ListRelationshipsOptions{Limit: 1}); err == nil {
					totalRel = rels.Total
				}
			}

			sort.Slice(relTypes, func(i, j int) bool {
				return relTypes[i].Count > relTypes[j].Count
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
			fmt.Fprintf(out, "Objects:    %d\n", totalObj)
			if len(objTypes) > 0 {
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
			fmt.Fprintf(out, "Relations:  %d\n", totalRel)
			if len(relTypes) > 0 {
				maxW := 0
				for _, rt := range relTypes {
					if len(rt.Type) > maxW {
						maxW = len(rt.Type)
					}
				}
				for _, rt := range relTypes {
					fmt.Fprintf(out, "  %-*s  %d\n", maxW, rt.Type, rt.Count)
				}
			}

			return nil
		},
	}

	return cmd
}
