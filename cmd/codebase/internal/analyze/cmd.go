package analyzecmd

import "github.com/spf13/cobra"

func NewCmd(flagProjectID *string, flagBranch *string, flagFormat *string) *cobra.Command {
	var flagApp string

	cmd := &cobra.Command{
		Use:   "analyze",
		Short: "Analyze codebase structure",
	}
	cmd.PersistentFlags().StringVar(&flagApp, "app", "", "Filter to a specific app (key from .codebase.yml apps section, e.g. \"api-server\")")
	cmd.AddCommand(newTreeCmd(flagProjectID, flagBranch, flagFormat, &flagApp))
	cmd.AddCommand(newUMLCmd(flagProjectID, flagBranch, flagFormat, &flagApp))
	cmd.AddCommand(newScenariosCmd(flagProjectID, flagBranch, flagFormat, &flagApp))
	cmd.AddCommand(newContextsCmd(flagProjectID, flagBranch, flagFormat, &flagApp))
	cmd.AddCommand(newCompetitiveCmd(flagProjectID, flagBranch, flagFormat, &flagApp))
	return cmd
}
