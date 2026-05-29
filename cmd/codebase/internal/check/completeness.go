package checkcmd

import (
	"context"
	"time"

	"github.com/mkucharz/codebase/cmd/codebase/internal/config"
	"github.com/mkucharz/codebase/cmd/codebase/internal/graph"
	cbcheck "github.com/emergent-company/codebase/commands/check"
	"github.com/spf13/cobra"
)

func newCompletenessCmd(flagProjectID *string, flagBranch *string, flagFormat *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "completeness",
		Short: "Holistic graph completeness assessment with scores and guidance",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.New(*flagProjectID, *flagBranch)
			if err != nil {
				return err
			}
			adapter := graph.NewSDKAdapter(cfg.Graph)

			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()

			return cbcheck.RunCompleteness(ctx, adapter, &cbcheck.CompletenessOptions{
				Format: *flagFormat,
			}, cmd.OutOrStdout())
		},
	}
	return cmd
}
