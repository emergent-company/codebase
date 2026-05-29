package checkcmd

import (
	"context"
	"time"

	"github.com/mkucharz/codebase/cmd/codebase/internal/config"
	"github.com/mkucharz/codebase/cmd/codebase/internal/graph"
	cbcheck "github.com/emergent-company/codebase/commands/check"
	"github.com/spf13/cobra"
)

func newLogicCmd(flagProjectID *string, flagBranch *string, flagFormat *string, flagApp *string) *cobra.Command {
	var flagChecks string
	var flagDomain string
	var flagVerbose bool

	cmd := &cobra.Command{
		Use:   "logic",
		Short: "Audit graph for logical consistency and design gaps",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.New(*flagProjectID, *flagBranch)
			if err != nil {
				return err
			}
			adapter := graph.NewSDKAdapter(cfg.Graph)

			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()

			return cbcheck.RunLogic(ctx, adapter, &cbcheck.LogicOptions{
				Checks:  flagChecks,
				Domain:  flagDomain,
				Verbose: flagVerbose,
				Format:  *flagFormat,
			}, cmd.OutOrStdout())
		},
	}

	cmd.Flags().StringVar(&flagChecks, "checks", "", "Comma-separated checks to run")
	cmd.Flags().StringVar(&flagDomain, "domain", "", "Filter to a specific domain")
	cmd.Flags().BoolVar(&flagVerbose, "verbose", false, "Include passing checks in output")
	return cmd
}
