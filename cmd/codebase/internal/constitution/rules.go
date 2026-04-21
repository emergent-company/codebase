package constitutioncmd

import (
	"context"

	"github.com/mkucharz/codebase/cmd/codebase/internal/config"
	"github.com/mkucharz/codebase/cmd/codebase/internal/graph"
	cbconstitution "github.com/emergent-company/codebase/commands/constitution"
	"github.com/spf13/cobra"
)

func newRulesCmd(flagProjectID *string, flagBranch *string, flagFormat *string) *cobra.Command {
	var flagCategory string

	cmd := &cobra.Command{
		Use:   "rules",
		Short: "List constitution rules",
		Long: `List all Rule objects in the constitution.

Examples:
  codebase constitution rules
  codebase constitution rules --category naming
  codebase constitution rules --format json
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := config.New(*flagProjectID, *flagBranch)
			if err != nil {
				return err
			}
			adapter := graph.NewSDKAdapter(c.Graph)
			return cbconstitution.RunRules(context.Background(), adapter, cmd.OutOrStdout(), flagCategory, *flagFormat)
		},
	}

	cmd.Flags().StringVar(&flagCategory, "category", "", "Filter by category: naming, api, service, db, scenario")
	return cmd
}
