package synccmd

import (
	"github.com/spf13/cobra"
)

func NewCmd(flagProjectID *string, flagBranch *string, flagFormat *string) *cobra.Command {
	var flagApp string

	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Populate the graph from codebase source files",
	}
	cmd.PersistentFlags().StringVar(&flagApp, "app", "", "Filter to a specific app (key from .codebase.yml apps section, e.g. \"api-server\")")
	cmd.AddCommand(newRoutesCmd(flagProjectID, flagBranch, flagFormat, &flagApp))
	cmd.AddCommand(newMiddlewareCmd(flagProjectID, flagBranch, flagFormat, &flagApp))
	cmd.AddCommand(newFilesCmd(flagProjectID, flagBranch, flagFormat, &flagApp))
	cmd.AddCommand(newViewsCmd(flagProjectID, flagBranch, flagFormat, &flagApp))
	cmd.AddCommand(newComponentsCmd(flagProjectID, flagBranch, flagFormat, &flagApp))
	cmd.AddCommand(newActionsCmd(flagProjectID, flagBranch, flagFormat, &flagApp))
	cmd.AddCommand(newScenariosCmd(flagProjectID, flagBranch, flagFormat, &flagApp))
	return cmd
}
