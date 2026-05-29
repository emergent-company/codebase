package checkcmd

import "github.com/spf13/cobra"

func NewCmd(flagProjectID *string, flagBranch *string, flagFormat *string) *cobra.Command {
	var flagApp string

	cmd := &cobra.Command{
		Use:   "check",
		Short: "Read-only graph quality analysis",
	}
	cmd.PersistentFlags().StringVar(&flagApp, "app", "", "Filter to a specific app (name from .codebase.yml apps section)")
	cmd.AddCommand(newAPICmd(flagProjectID, flagBranch, flagFormat, &flagApp))
	cmd.AddCommand(newLogicCmd(flagProjectID, flagBranch, flagFormat, &flagApp))
	cmd.AddCommand(newCoverageCmd(flagProjectID, flagBranch, flagFormat, &flagApp))
	cmd.AddCommand(newComplexityCmd(flagProjectID, flagBranch, flagFormat, &flagApp))
	return cmd
}
