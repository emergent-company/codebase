package seedcmd

import "github.com/spf13/cobra"

func NewCmd(flagProjectID *string, flagBranch *string, flagFormat *string) *cobra.Command {
	var flagApp string

	cmd := &cobra.Command{Use: "seed", Short: "Write seed objects to the graph"}
	cmd.PersistentFlags().StringVar(&flagApp, "app", "", "Filter to a specific app (key from .codebase.yml apps section, e.g. \"api-server\")")
	cmd.AddCommand(newEntitiesCmd(flagProjectID, flagBranch, flagFormat, &flagApp))
	cmd.AddCommand(newExposesCmd(flagProjectID, flagBranch, flagFormat, &flagApp))
	return cmd
}
