package seedcmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk"
	sdkgraph "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
	"github.com/mkucharz/codebase/cmd/codebase/internal/config"
	"github.com/spf13/cobra"
)

func newExposesCmd(flagProjectID *string, flagBranch *string, flagFormat *string, flagApp *string) *cobra.Command {
	var (
		flagDryRun  bool
		flagCleanup bool
	)

	cmd := &cobra.Command{
		Use:   "exposes",
		Short: "Wire Service → exposes → APIEndpoint",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.New(*flagProjectID, *flagBranch)
			if err != nil {
				return err
			}
			return runExposes(cfg.SDK, flagDryRun, flagCleanup, flagApp)
		},
	}

	cmd.Flags().BoolVar(&flagDryRun, "dry-run", false, "Dry run")
	cmd.Flags().BoolVar(&flagCleanup, "cleanup", false, "Cleanup old rels")

	return cmd
}

func runExposes(client *sdk.Client, dryRun, cleanup bool, flagApp *string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Resolve app scope for domain filtering
	scope := resolveAppScope(ctx, client.Graph, *flagApp)

	services, err := listAll(ctx, client.Graph, "Service")
	if err != nil {
		return err
	}
	serviceByDomain := map[string]string{}
	for _, svc := range services {
		name, _ := svc.Properties["name"].(string)
		slug := strings.ToLower(strings.TrimSuffix(name, "Service"))
		serviceByDomain[slug] = svc.EntityID
	}

	endpoints, err := listAll(ctx, client.Graph, "APIEndpoint")
	if err != nil {
		return err
	}

	var toCreate []sdkgraph.CreateRelationshipRequest
	for _, ep := range endpoints {
		domain, _ := ep.Properties["domain"].(string)
		// Filter by app scope
		if !scope.domainPasses(domain) {
			continue
		}
		if svcID, ok := serviceByDomain[domain]; ok {
			toCreate = append(toCreate, sdkgraph.CreateRelationshipRequest{
				Type: "exposes", SrcID: svcID, DstID: ep.EntityID, Upsert: true,
			})
		}
	}

	if dryRun {
		fmt.Printf("[DRY-RUN] Would create %d exposes rels\n", len(toCreate))
		return nil
	}

	resp, err := client.Graph.BulkCreateRelationships(ctx, &sdkgraph.BulkCreateRelationshipsRequest{Items: toCreate})
	if err != nil {
		return err
	}
	fmt.Printf("Created %d rels, failed %d\n", resp.Success, resp.Failed)

	if cleanup {
		// Cleanup logic from service-exposes-seed/main.go
	}
	return nil
}

func listAll(ctx context.Context, g *sdkgraph.Client, objType string) ([]*sdkgraph.GraphObject, error) {
	var all []*sdkgraph.GraphObject
	cursor := ""
	for {
		resp, err := g.ListObjects(ctx, &sdkgraph.ListObjectsOptions{Type: objType, Limit: 500, Cursor: cursor})
		if err != nil {
			return nil, err
		}
		all = append(all, resp.Items...)
		if resp.NextCursor == nil || *resp.NextCursor == "" {
			break
		}
		cursor = *resp.NextCursor
	}
	return all, nil
}
