package checkcmd

import (
	"context"
	"time"

	"github.com/mkucharz/codebase/cmd/codebase/internal/config"
	"github.com/mkucharz/codebase/cmd/codebase/internal/graph"
	cbcheck "github.com/emergent-company/codebase/commands/check"
	"github.com/spf13/cobra"
)

func newOrphansCmd(flagProjectID *string, flagBranch *string, flagFormat *string) *cobra.Command {
	var flagDomain string

	cmd := &cobra.Command{
		Use:   "orphans",
		Short: "Find graph objects with no relationships",
		Long: `Find objects that have no relationships of any kind (incoming or outgoing).
These are disconnected nodes that may be stale or incomplete.

Use --domain to filter orphans by domain property or key pattern.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.New(*flagProjectID, *flagBranch)
			if err != nil {
				return err
			}
			adapter := graph.NewSDKAdapter(cfg.Graph)

			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()

			return cbcheck.RunOrphans(ctx, adapter, &cbcheck.OrphanOptions{
				Domain: flagDomain,
				Format: *flagFormat,
			}, cmd.OutOrStdout())
		},
	}

	cmd.Flags().StringVar(&flagDomain, "domain", "", "Filter orphans by domain name or key pattern")
	return cmd
}
