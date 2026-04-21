package fix

import (
	"context"
	"fmt"
	"io"
	"strings"

	sdkgraph "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
	"github.com/emergent-company/codebase/graph"
	"github.com/emergent-company/codebase/commands/sync"
)

type RewireOptions struct {
	From   string
	Map    string
	Domain string
	Apply  bool
}

func getByKey(ctx context.Context, gc graph.GraphClient, key string) (*sdkgraph.GraphObject, error) {
	res, err := gc.ListObjects(ctx, &sdkgraph.ListObjectsOptions{Key: key, Limit: 5})
	if err != nil {
		return nil, err
	}
	for _, o := range res.Items {
		if o.Key != nil && *o.Key == key {
			return o, nil
		}
	}
	return nil, fmt.Errorf("object with key %q not found", key)
}

func RunRewire(ctx context.Context, g graph.GraphClient, opts *RewireOptions, out io.Writer) error {
	fromObj, err := getByKey(ctx, g, opts.From)
	if err != nil {
		return fmt.Errorf("resolve from context: %w", err)
	}

	slugToKey := make(map[string]string)
	for _, pair := range strings.Split(opts.Map, ",") {
		parts := strings.Split(pair, "=")
		if len(parts) == 2 {
			slugToKey[parts[0]] = parts[1]
		}
	}

	slugToID := make(map[string]string)
	for slug, key := range slugToKey {
		obj, err := getByKey(ctx, g, key)
		if err == nil {
			slugToID[slug] = obj.EntityID
		}
	}

	rels, err := sync.ListAllRelationships(ctx, g, "occurs_in")
	if err != nil {
		return err
	}

	var ops []rewireOp
	for _, r := range rels {
		if r.DstID != fromObj.EntityID {
			continue
		}
		_ = r
	}

	if !opts.Apply {
		fmt.Fprintf(out, "[DRY-RUN] Would rewire %d steps\n", len(ops))
		return nil
	}

	return nil
}

type rewireOp struct {
	relID, stepID, newCtxID string
}
