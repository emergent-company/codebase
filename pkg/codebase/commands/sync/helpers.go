package sync

import (
	"context"

	sdkgraph "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
	"github.com/emergent-company/codebase/graph"
)

func ListAllObjects(ctx context.Context, g graph.GraphClient, objType string) ([]*sdkgraph.GraphObject, error) {
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

func ListAllRelationships(ctx context.Context, g graph.GraphClient, relType string) ([]*sdkgraph.GraphRelationship, error) {
	var all []*sdkgraph.GraphRelationship
	cursor := ""
	for {
		resp, err := g.ListRelationships(ctx, &sdkgraph.ListRelationshipsOptions{Type: relType, Limit: 500, Cursor: cursor})
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

func StrProp(o *sdkgraph.GraphObject, key string) string {
	if o == nil || o.Properties == nil {
		return ""
	}
	v, _ := o.Properties[key].(string)
	return v
}

func DerefKey(k *string) string {
	if k == nil {
		return ""
	}
	return *k
}
