package seedcmd

import (
	"context"
	"os"
	"path/filepath"

	"github.com/mkucharz/codebase/cmd/codebase/internal/config"
	"github.com/mkucharz/codebase/cmd/codebase/internal/graph"
	cbseed "github.com/emergent-company/codebase/commands/seed"
	"github.com/spf13/cobra"
)

func newEntitiesCmd(flagProjectID *string, flagBranch *string, flagFormat *string, flagApp *string) *cobra.Command {
	var (
		flagRepo    string
		flagGlob    string
		flagDomain  string
		flagDryRun  bool
		flagVerbose bool
	)
	cwd, _ := os.Getwd()

	cmd := &cobra.Command{
		Use:   "entities",
		Short: "Seed entities from Go source files",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.New(*flagProjectID, *flagBranch)
			if err != nil {
				return err
			}

			// When --app is given, set --repo to the app's root_path if --repo was not explicitly set
			if *flagApp != "" && !cmd.Flags().Changed("repo") {
				app := config.FindApp(*flagApp)
				if app != nil && app.RootPath != "" {
					flagRepo = filepath.Join(cwd, app.RootPath)
				}
			}

			adapter := graph.NewSDKAdapter(cfg.Graph)
			return cbseed.RunEntities(context.Background(), adapter, &cbseed.EntitiesOptions{
				Repo:    flagRepo,
				Glob:    flagGlob,
				Domain:  flagDomain,
				DryRun:  flagDryRun,
				Verbose: flagVerbose,
			}, cmd.OutOrStdout(), cmd.OutOrStderr())
		},
	}

	cmd.Flags().StringVar(&flagRepo, "repo", ".", "Repo root")
	cmd.Flags().StringVar(&flagGlob, "glob", "apps/server/domain/*/entity.go,apps/server/pkg/*/entity.go", "Comma-separated glob patterns")
	cmd.Flags().StringVar(&flagDomain, "domain", "", "Filter by domain slug")
	cmd.Flags().BoolVar(&flagDryRun, "dry-run", false, "Dry run")
	cmd.Flags().BoolVar(&flagVerbose, "verbose", false, "Verbose output")

	return cmd
}
