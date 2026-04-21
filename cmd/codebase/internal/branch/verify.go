package branchcmd

import (
	"context"

	"github.com/mkucharz/codebase/cmd/codebase/internal/config"
	"github.com/mkucharz/codebase/cmd/codebase/internal/graph"
	cbbranch "github.com/emergent-company/codebase/commands/branch"
	"github.com/spf13/cobra"
)

func newVerifyCmd(flagProjectID *string) *cobra.Command {
	var (
		flagBranch  string
		flagTarget  string
		flagMerge   bool
		flagLimit   int
		flagVerbose bool
		flagRepo    string
	)

	cmd := &cobra.Command{
		Use:   "verify",
		Short: "Verify branch changes against disk",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.New(*flagProjectID, "")
			if err != nil {
				return err
			}
			adapter := graph.NewSDKAdapter(cfg.Graph)
			return cbbranch.RunVerify(context.Background(), adapter, &cbbranch.VerifyOptions{
				Branch:  flagBranch,
				Target:  flagTarget,
				Merge:   flagMerge,
				Limit:   flagLimit,
				Verbose: flagVerbose,
				Repo:    flagRepo,
			}, cmd.OutOrStdout())
		},
	}

	cmd.Flags().StringVar(&flagBranch, "branch", "", "Branch ID to verify (required)")
	cmd.Flags().StringVar(&flagTarget, "target", "main", "Target branch ID or 'main'")
	cmd.Flags().BoolVar(&flagMerge, "merge", false, "Execute merge after verification")
	cmd.Flags().IntVar(&flagLimit, "limit", 2000, "Merge diff limit")
	cmd.Flags().BoolVar(&flagVerbose, "verbose", false, "Verbose output")
	cmd.Flags().StringVar(&flagRepo, "repo", ".", "Repo root")
	cmd.MarkFlagRequired("branch")

	return cmd
}
