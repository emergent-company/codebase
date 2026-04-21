package fixcmd

import (
	"context"

	"github.com/mkucharz/codebase/cmd/codebase/internal/config"
	"github.com/mkucharz/codebase/cmd/codebase/internal/graph"
	cbfix "github.com/emergent-company/codebase/commands/fix"
	"github.com/spf13/cobra"
)

func newStaleCmd(flagProjectID *string, flagBranch *string, flagFormat *string) *cobra.Command {
	var (
		flagDryRun bool
		flagDomain string
	)

	cmd := &cobra.Command{
		Use:   "stale",
		Short: "Delete stale APIEndpoints",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.New(*flagProjectID, *flagBranch)
			if err != nil {
				return err
			}
			adapter := graph.NewSDKAdapter(cfg.Graph)
			return cbfix.RunStale(context.Background(), adapter, &cbfix.StaleOptions{
				DryRun: flagDryRun,
				Domain: flagDomain,
			}, cmd.OutOrStdout(), cmd.OutOrStderr())
		},
	}

	cmd.Flags().BoolVar(&flagDryRun, "dry-run", false, "Dry run")
	cmd.Flags().StringVar(&flagDomain, "domain", "", "Filter by domain")

	return cmd
}
