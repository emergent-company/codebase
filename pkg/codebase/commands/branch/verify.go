package branch

import (
	"context"
	"fmt"
	"io"
	"path/filepath"

	sdkgraph "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
	"github.com/emergent-company/codebase/graph"
)

type VerifyOptions struct {
	Branch  string
	Target  string
	Merge   bool
	Limit   int
	Verbose bool
	Repo    string
}

func RunVerify(ctx context.Context, g graph.GraphClient, opts *VerifyOptions, out io.Writer) error {
	absRepo, _ := filepath.Abs(opts.Repo)

	fmt.Fprintf(out, "branch-verify — branch %s → %s\n", opts.Branch, opts.Target)

	mergeReq := &sdkgraph.BranchMergeRequest{
		SourceBranchID: opts.Branch,
		Execute:        false,
		Limit:          &opts.Limit,
	}
	resp, err := g.MergeBranch(ctx, opts.Target, mergeReq)
	if err != nil {
		return err
	}

	fmt.Fprintf(out, "  Conflicts: %d\n", resp.ConflictCount)
	if resp.ConflictCount > 0 {
		return fmt.Errorf("conflicts exist")
	}

	_ = absRepo

	if opts.Merge {
		fmt.Fprintln(out, "Executing merge...")
		mergeReq.Execute = true
		_, err := g.MergeBranch(ctx, opts.Target, mergeReq)
		if err != nil {
			return err
		}
		fmt.Fprintln(out, "✓ Merge successful")
	}

	return nil
}
