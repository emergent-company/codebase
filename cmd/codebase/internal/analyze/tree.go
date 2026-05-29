package analyzecmd

import (
	"context"

	"github.com/mkucharz/codebase/cmd/codebase/internal/config"
	"github.com/mkucharz/codebase/cmd/codebase/internal/graph"
	cbanalyze "github.com/emergent-company/codebase/commands/analyze"
	"github.com/spf13/cobra"
)

func newTreeCmd(flagProjectID *string, flagBranch *string, flagFormat *string, flagApp *string) *cobra.Command {
	var (
		flagDomain        string
		flagShowScenarios bool
		flagShowEndpoints bool
		flagMinScenarios  int
	)

	cmd := &cobra.Command{
		Use:   "tree",
		Short: "Show functionality map as a tree",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.New(*flagProjectID, *flagBranch)
			if err != nil {
				return err
			}
			adapter := graph.NewSDKAdapter(cfg.Graph)

			// Resolve app scope
			scope := resolveAppScope(cmd.Context(), cfg.Graph, *flagApp)
			appDomain := flagDomain
			if scope != nil && len(scope.Domains) == 1 {
				for d := range scope.Domains {
					appDomain = d
					break
				}
			}

			return cbanalyze.RunTree(context.Background(), adapter, cmd.OutOrStdout(), appDomain, flagShowScenarios, flagShowEndpoints, flagMinScenarios, *flagFormat)
		},
	}

	cmd.Flags().StringVar(&flagDomain, "domain", "", "Filter to one domain (key slug, e.g. 'agents')")
	cmd.Flags().BoolVar(&flagShowScenarios, "show-scenarios", false, "List individual scenarios")
	cmd.Flags().BoolVar(&flagShowEndpoints, "show-endpoints", false, "List individual endpoints")
	cmd.Flags().IntVar(&flagMinScenarios, "min-scenarios", 0, "Only show domains with >= N scenarios")

	return cmd
}
