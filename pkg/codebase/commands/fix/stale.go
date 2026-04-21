package fix

import (
	"context"
	"fmt"
	"io"

	"github.com/emergent-company/codebase/graph"
	"github.com/emergent-company/codebase/commands/sync"
)

type StaleOptions struct {
	DryRun bool
	Domain string
}

func RunStale(ctx context.Context, g graph.GraphClient, opts *StaleOptions, out io.Writer, stderr io.Writer) error {
	eps, err := sync.ListAllObjects(ctx, g, "APIEndpoint")
	if err != nil {
		return err
	}

	deleted := 0
	for _, ep := range eps {
		domain := sync.StrProp(ep, "domain")
		if opts.Domain != "" && domain != opts.Domain {
			continue
		}

		key := sync.DerefKey(ep.Key)
		handler := sync.StrProp(ep, "handler")

		if key == "" && domain == "" && handler == "" {
			if opts.DryRun {
				fmt.Fprintf(out, "[DRY-RUN] Delete stale endpoint: %s\n", ep.EntityID)
			} else {
				if err := g.DeleteObject(ctx, ep.EntityID, nil); err != nil {
					fmt.Fprintf(stderr, "Error deleting %s: %v\n", ep.EntityID, err)
				} else {
					deleted++
				}
			}
		}
	}
	fmt.Fprintf(out, "Deleted %d stale endpoints\n", deleted)
	return nil
}
