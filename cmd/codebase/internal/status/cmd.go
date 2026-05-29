// Package statuscmd implements the `codebase status` command — connection
// health check and project statistics.
package statuscmd

import (
	"context"
	"fmt"
	"os"

	sdkprojects "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/projects"
	"github.com/mkucharz/codebase/cmd/codebase/internal/config"
	"github.com/spf13/cobra"
)

func NewCmd(flagProjectID *string, flagBranch *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Test connection and show project stats",
		Long: `Test connection to the Memory Platform and show project statistics.

Tests authentication, retrieves project info, and displays object/relationship
counts for the configured codebase project.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			cfg, err := config.New(*flagProjectID, *flagBranch)
			if err != nil {
				return fmt.Errorf("connection failed: %w", err)
			}

			out := cmd.OutOrStdout()

			// Fetch project info with stats
			proj, err := cfg.SDK.Projects.Get(ctx, cfg.ProjectID, &sdkprojects.GetOptions{
				IncludeStats: true,
			})
			if err != nil {
				return fmt.Errorf("fetch project: %w", err)
			}

			serverURL := os.Getenv("MEMORY_SERVER_URL")
			if serverURL == "" {
				serverURL = "https://memory.emergent-company.ai"
			}

			// Print summary
			fmt.Fprintf(out, "Server:    %s\n", serverURL)
			fmt.Fprintf(out, "Auth:      ✓ OK\n")
			fmt.Fprintf(out, "Project:   %s (%s)\n", proj.Name, proj.ID)
			if proj.Stats != nil {
				fmt.Fprintf(out, "Objects:   %d\n", proj.Stats.ObjectCount)
				fmt.Fprintf(out, "Relations: %d\n", proj.Stats.RelationshipCount)
				fmt.Fprintf(out, "Schemas:   %d pack(s) installed\n", len(proj.Stats.Schemas))
			} else {
				fmt.Fprintf(out, "Objects:   n/a\n")
				fmt.Fprintf(out, "Relations: n/a\n")
				fmt.Fprintf(out, "Schemas:   n/a\n")
			}

			return nil
		},
	}

	return cmd
}
