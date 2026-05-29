package synccmd

import (
	"context"
	"os"
	"path/filepath"

	"github.com/mkucharz/codebase/cmd/codebase/internal/config"
	"github.com/mkucharz/codebase/cmd/codebase/internal/graph"
	configpkg "github.com/emergent-company/codebase/config"
	cbsync "github.com/emergent-company/codebase/commands/sync"
	"github.com/spf13/cobra"
)

func newRoutesCmd(flagProjectID *string, flagBranch *string, flagFormat *string, flagApp *string) *cobra.Command {
	opts := &cbsync.RoutesOptions{}
	cwd, _ := os.Getwd()

	cmd := &cobra.Command{
		Use:   "routes",
		Short: "Populate APIEndpoint metadata from route files",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Resolve app scope
			c, err := config.New(*flagProjectID, *flagBranch)
			if err != nil {
				return err
			}

			// When --app is given, resolve repo to the app's root_path if --repo was not explicitly set
			if *flagApp != "" {
				if !cmd.Flags().Changed("repo") {
					app := config.FindApp(*flagApp)
					if app != nil && app.RootPath != "" {
						opts.Repo = filepath.Join(cwd, app.RootPath)
					}
				}
			}
			adapter := graph.NewSDKAdapter(c.Graph)
			yml := config.LoadYML()
			var pkgYml *configpkg.CodebaseYML
			if yml != nil {
				pkgYml = &configpkg.CodebaseYML{
					Project:   yml.Project,
					ProjectID: yml.ProjectID,
					Server:    yml.Server,
					Sync: configpkg.SyncConfig{
						Routes: configpkg.SyncRoutesConfig{
							Framework:     yml.Sync.Routes.Framework,
							Command:       yml.Sync.Routes.Command,
							Runtime:       yml.Sync.Routes.Runtime,
							Glob:          yml.Sync.Routes.Glob,
							DomainSegment: yml.Sync.Routes.DomainSegment,
						},
					},
				}
			}
			return cbsync.RunRoutes(context.Background(), adapter, opts, pkgYml, cmd.OutOrStdout(), cmd.OutOrStderr())
		},
	}

	cmd.Flags().StringVar(&opts.Repo, "repo", cwd, "Path to repository root")
	cmd.Flags().BoolVar(&opts.DryRun, "dry-run", false, "Print what would be updated without writing to graph")
	cmd.Flags().StringVar(&opts.RouteGlob, "route-glob",
		"apps/server/domain/*/routes.go,apps/server/domain/*/*routes*.go,apps/server/domain/*/module.go",
		"Comma-separated glob patterns for route files, relative to --repo")
	cmd.Flags().BoolVar(&opts.Defines, "defines", false, "Wire defines relationships from SourceFile to APIEndpoint objects")

	return cmd
}
