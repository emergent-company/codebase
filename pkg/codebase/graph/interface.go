package graph

import (
	"context"

	sdkgraph "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
)

// GraphClient abstracts graph operations so they can be satisfied by either
// the Memory SDK directly or via the flow-server HTTP proxy.
type GraphClient interface {
	// Objects
	ListObjects(ctx context.Context, opts *sdkgraph.ListObjectsOptions) (*sdkgraph.SearchObjectsResponse, error)
	GetObject(ctx context.Context, id string) (*sdkgraph.GraphObject, error)
	CreateObject(ctx context.Context, req *sdkgraph.CreateObjectRequest) (*sdkgraph.GraphObject, error)
	UpsertObject(ctx context.Context, req *sdkgraph.CreateObjectRequest) (*sdkgraph.GraphObject, error)
	UpdateObject(ctx context.Context, id string, req *sdkgraph.UpdateObjectRequest) (*sdkgraph.GraphObject, error)
	// DeleteObject deletes an object by ID. branchID is optional (pass nil for default branch).
	DeleteObject(ctx context.Context, id string, branchID *string) error

	// Search
	HybridSearch(ctx context.Context, req *sdkgraph.HybridSearchRequest) (*sdkgraph.SearchResponse, error)
	FTSSearch(ctx context.Context, opts *sdkgraph.FTSSearchOptions) (*sdkgraph.SearchResponse, error)

	// Relationships
	ListRelationships(ctx context.Context, opts *sdkgraph.ListRelationshipsOptions) (*sdkgraph.SearchRelationshipsResponse, error)
	CreateRelationship(ctx context.Context, req *sdkgraph.CreateRelationshipRequest) (*sdkgraph.GraphRelationship, error)
	UpsertRelationship(ctx context.Context, req *sdkgraph.CreateRelationshipRequest) (*sdkgraph.GraphRelationship, error)
	BulkCreateRelationships(ctx context.Context, req *sdkgraph.BulkCreateRelationshipsRequest) (*sdkgraph.BulkCreateRelationshipsResponse, error)
	DeleteRelationship(ctx context.Context, id string) error

	// Bulk
	BulkCreateObjects(ctx context.Context, req *sdkgraph.BulkCreateObjectsRequest) (*sdkgraph.BulkCreateObjectsResponse, error)
	BulkUpdateObjects(ctx context.Context, req *sdkgraph.BulkUpdateObjectsRequest) (*sdkgraph.BulkUpdateObjectsResponse, error)

	// Branch operations
	MergeBranch(ctx context.Context, targetBranchID string, req *sdkgraph.BranchMergeRequest) (*sdkgraph.BranchMergeResponse, error)
}
