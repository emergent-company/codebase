package graph

import (
	"context"

	sdkgraph "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
	cbgraph "github.com/emergent-company/codebase/graph"
)

// SDKAdapter wraps *sdkgraph.Client to satisfy cbgraph.GraphClient.
// This allows the codebase CLI to use the Memory SDK directly while
// sharing all business logic with flow (which uses the HTTP proxy).
type SDKAdapter struct {
	c *sdkgraph.Client
}

// NewSDKAdapter creates a new SDKAdapter wrapping the given SDK graph client.
func NewSDKAdapter(c *sdkgraph.Client) cbgraph.GraphClient {
	return &SDKAdapter{c: c}
}

func (a *SDKAdapter) ListObjects(ctx context.Context, opts *sdkgraph.ListObjectsOptions) (*sdkgraph.SearchObjectsResponse, error) {
	return a.c.ListObjects(ctx, opts)
}

func (a *SDKAdapter) GetObject(ctx context.Context, id string) (*sdkgraph.GraphObject, error) {
	return a.c.GetObject(ctx, id)
}

func (a *SDKAdapter) CreateObject(ctx context.Context, req *sdkgraph.CreateObjectRequest) (*sdkgraph.GraphObject, error) {
	return a.c.CreateObject(ctx, req)
}

func (a *SDKAdapter) UpsertObject(ctx context.Context, req *sdkgraph.CreateObjectRequest) (*sdkgraph.GraphObject, error) {
	return a.c.UpsertObject(ctx, req)
}

func (a *SDKAdapter) UpdateObject(ctx context.Context, id string, req *sdkgraph.UpdateObjectRequest) (*sdkgraph.GraphObject, error) {
	return a.c.UpdateObject(ctx, id, req)
}

func (a *SDKAdapter) DeleteObject(ctx context.Context, id string, branchID *string) error {
	return a.c.DeleteObject(ctx, id, branchID)
}

func (a *SDKAdapter) ListRelationships(ctx context.Context, opts *sdkgraph.ListRelationshipsOptions) (*sdkgraph.SearchRelationshipsResponse, error) {
	return a.c.ListRelationships(ctx, opts)
}

func (a *SDKAdapter) CreateRelationship(ctx context.Context, req *sdkgraph.CreateRelationshipRequest) (*sdkgraph.GraphRelationship, error) {
	return a.c.CreateRelationship(ctx, req)
}

func (a *SDKAdapter) UpsertRelationship(ctx context.Context, req *sdkgraph.CreateRelationshipRequest) (*sdkgraph.GraphRelationship, error) {
	return a.c.UpsertRelationship(ctx, req)
}

func (a *SDKAdapter) DeleteRelationship(ctx context.Context, id string) error {
	return a.c.DeleteRelationship(ctx, id)
}

func (a *SDKAdapter) MergeBranch(ctx context.Context, targetBranchID string, req *sdkgraph.BranchMergeRequest) (*sdkgraph.BranchMergeResponse, error) {
	return a.c.MergeBranch(ctx, targetBranchID, req)
}

func (a *SDKAdapter) BulkCreateObjects(ctx context.Context, req *sdkgraph.BulkCreateObjectsRequest) (*sdkgraph.BulkCreateObjectsResponse, error) {
	return a.c.BulkCreateObjects(ctx, req)
}

func (a *SDKAdapter) BulkUpdateObjects(ctx context.Context, req *sdkgraph.BulkUpdateObjectsRequest) (*sdkgraph.BulkUpdateObjectsResponse, error) {
	return a.c.BulkUpdateObjects(ctx, req)
}

func (a *SDKAdapter) BulkCreateRelationships(ctx context.Context, req *sdkgraph.BulkCreateRelationshipsRequest) (*sdkgraph.BulkCreateRelationshipsResponse, error) {
	return a.c.BulkCreateRelationships(ctx, req)
}

func (a *SDKAdapter) HybridSearch(ctx context.Context, req *sdkgraph.HybridSearchRequest) (*sdkgraph.SearchResponse, error) {
	return a.c.HybridSearch(ctx, req)
}

func (a *SDKAdapter) FTSSearch(ctx context.Context, opts *sdkgraph.FTSSearchOptions) (*sdkgraph.SearchResponse, error) {
	return a.c.FTSSearch(ctx, opts)
}
