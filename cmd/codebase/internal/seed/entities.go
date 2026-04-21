package seedcmd

import (
	"context"

	"github.com/mkucharz/codebase/cmd/codebase/internal/config"
	"github.com/mkucharz/codebase/cmd/codebase/internal/graph"
	cbseed "github.com/emergent-company/codebase/commands/seed"
	"github.com/spf13/cobra"
)

func newEntitiesCmd(flagProjectID *string, flagBranch *string, flagFormat *string) *cobra.Command {
	var (
		flagRepo    string
		flagGlob    string
		flagDomain  string
		flagDryRun  bool
		flagVerbose bool
	)

	cmd := &cobra.Command{
		Use:   "entities",
		Short: "Seed entities from Go source files",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.New(*flagProjectID, *flagBranch)
			if err != nil {
				return err
			}
			adapter := graph.NewSDKAdapter(cfg.Graph)
			return cbseed.RunEntities(context.Background(), adapter, &cbseed.EntitiesOptions{
				Repo:    flagRepo,
				Glob:    flagGlob,
				Domain:  flagDomain,
				DryRun:  flagDryRun,
				Verbose: flagVerbose,
			}, cmd.OutOrStdout(), cmd.OutOrStderr())
		},
	}

	cmd.Flags().StringVar(&flagRepo, "repo", ".", "Repo root")
	cmd.Flags().StringVar(&flagGlob, "glob", "apps/server/domain/*/entity.go,apps/server/pkg/*/entity.go", "Comma-separated glob patterns")
	cmd.Flags().StringVar(&flagDomain, "domain", "", "Filter by domain slug")
	cmd.Flags().BoolVar(&flagDryRun, "dry-run", false, "Dry run")
	cmd.Flags().BoolVar(&flagVerbose, "verbose", false, "Verbose output")

	return cmd
}
