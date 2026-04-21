package seed

import (
	"context"
	"fmt"
	"io"
	"strings"

	sdkgraph "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
	"github.com/emergent-company/codebase/graph"
	"github.com/emergent-company/codebase/commands/sync"
)

type ExposesOptions struct {
	DryRun  bool
	Cleanup bool
}

func RunExposes(ctx context.Context, g graph.GraphClient, opts *ExposesOptions, out io.Writer) error {
	services, err := sync.ListAllObjects(ctx, g, "Service")
	if err != nil {
		return err
	}
	serviceByDomain := map[string]string{}
	for _, svc := range services {
		name := sync.StrProp(svc, "name")
		slug := strings.ToLower(strings.TrimSuffix(name, "Service"))
		serviceByDomain[slug] = svc.EntityID
	}

	endpoints, err := sync.ListAllObjects(ctx, g, "APIEndpoint")
	if err != nil {
		return err
	}

	var toCreate []sdkgraph.CreateRelationshipRequest
	for _, ep := range endpoints {
		domain := sync.StrProp(ep, "domain")
		if svcID, ok := serviceByDomain[domain]; ok {
			toCreate = append(toCreate, sdkgraph.CreateRelationshipRequest{
				Type: "exposes", SrcID: svcID, DstID: ep.EntityID, Upsert: true,
			})
		}
	}

	if opts.DryRun {
		fmt.Fprintf(out, "[DRY-RUN] Would create %d exposes rels\n", len(toCreate))
		return nil
	}

	resp, err := g.BulkCreateRelationships(ctx, &sdkgraph.BulkCreateRelationshipsRequest{Items: toCreate})
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Created %d rels, failed %d\n", resp.Success, resp.Failed)

	return nil
}
