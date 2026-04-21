package create

import (
	"context"
	"fmt"

	"github.com/mkucharz/codebase/cmd/codebase/internal/config"
	"github.com/mkucharz/codebase/cmd/codebase/internal/graph"
	cbcreate "github.com/emergent-company/codebase/commands/create"
	"github.com/spf13/cobra"
)

func NewCmd(flagProjectID *string, flagBranch *string, flagFormat *string) *cobra.Command {
	return newCreateRootCmd(flagProjectID, flagBranch, flagFormat)
}

// We need a way to return multiple commands to main.go
func Register(rootCmd *cobra.Command, flagProjectID *string, flagBranch *string, flagFormat *string) {
	rootCmd.AddCommand(newCreateRootCmd(flagProjectID, flagBranch, flagFormat))
	rootCmd.AddCommand(newKeyRootCmd())
}

func newCreateRootCmd(flagProjectID *string, flagBranch *string, flagFormat *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new graph object",
	}
	types := []string{"context", "uicomponent", "helper", "action", "apiendpoint", "sourcefile", "domain", "scenario", "step"}
	for _, t := range types {
		cmd.AddCommand(newCreateTypeCmd(t, flagProjectID, flagBranch))
	}
	return cmd
}

func newKeyRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "key",
		Short: "Generate and print a key for an object type",
	}
	types := []string{"context", "uicomponent", "helper", "action", "apiendpoint", "sourcefile", "domain", "scenario", "step"}
	for _, t := range types {
		cmd.AddCommand(newKeyTypeCmd(t))
	}
	return cmd
}

func newCreateTypeCmd(objType string, flagProjectID *string, flagBranch *string) *cobra.Command {
	opts := &cbcreate.CreateOptions{}
	cmd := &cobra.Command{
		Use:   objType + " <name>",
		Short: "Create a " + objType + " object",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := config.New(*flagProjectID, *flagBranch)
			if err != nil {
				return err
			}
			adapter := graph.NewSDKAdapter(c.Graph)
			return cbcreate.RunCreate(context.Background(), adapter, objType, args[0], opts, cmd.OutOrStdout())
		},
	}

	addFlags(cmd, objType, opts)
	return cmd
}

func newKeyTypeCmd(objType string) *cobra.Command {
	opts := &cbcreate.CreateOptions{}
	cmd := &cobra.Command{
		Use:   objType + " <name>",
		Short: "Generate key for " + objType,
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Fprintln(cmd.OutOrStdout(), cbcreate.GenerateKey(objType, args[0], opts))
		},
	}
	addFlags(cmd, objType, opts)
	return cmd
}

func addFlags(cmd *cobra.Command, objType string, opts *cbcreate.CreateOptions) {
	cmd.Flags().BoolVar(&opts.Upsert, "upsert", false, "Update if exists")
	cmd.Flags().StringVar(&opts.Name, "name", "", "Display name")
	cmd.Flags().StringVar(&opts.Description, "description", "", "Description")

	switch objType {
	case "context":
		cmd.Flags().StringVar(&opts.Route, "route", "", "Route path")
		cmd.Flags().StringVar(&opts.ContextType, "context-type", "screen", "Context type")
	case "uicomponent":
		cmd.Flags().StringVar(&opts.CompType, "type", "composite", "Component type (primitive/composite/layout)")
	case "helper":
		cmd.Flags().StringVar(&opts.CompType, "type", "hook", "Helper type")
	case "action":
		cmd.Flags().StringVar(&opts.DisplayLabel, "display-label", "", "Display label")
		cmd.Flags().StringVar(&opts.ActionType, "type", "trigger", "Action type")
		cmd.Flags().StringVar(&opts.Domain, "domain", "", "Domain (required for key)")
	case "apiendpoint":
		cmd.Flags().StringVar(&opts.Handler, "handler", "", "Handler name")
		cmd.Flags().StringVar(&opts.Method, "method", "GET", "HTTP method")
		cmd.Flags().StringVar(&opts.Path, "path", "", "API path")
		cmd.Flags().StringVar(&opts.Domain, "domain", "", "Domain")
		cmd.Flags().StringVar(&opts.File, "file", "", "Source file")
		cmd.Flags().BoolVar(&opts.AuthRequired, "auth-required", false, "Auth required")
	case "sourcefile":
		cmd.Flags().StringVar(&opts.Path, "path", "", "File path")
		cmd.Flags().StringVar(&opts.Language, "language", "typescript", "Language")
	case "domain":
		cmd.Flags().StringVar(&opts.Slug, "slug", "", "Slug")
		cmd.Flags().StringVar(&opts.App, "app", "", "App name")
	case "scenario":
		cmd.Flags().StringVar(&opts.Given, "given", "", "Given")
		cmd.Flags().StringVar(&opts.When, "when", "", "When")
		cmd.Flags().StringVar(&opts.Then, "then", "", "Then")
	case "step":
		cmd.Flags().IntVar(&opts.Order, "order", 0, "Order (required for key)")
		cmd.Flags().StringVar(&opts.Scenario, "scenario", "", "Scenario key (required for key)")
	}
}
