// Package statuscmd implements the `codebase status` command — connection
// health check and project statistics.
package statuscmd

import (
	"context"
	"fmt"
	"os"

	sdkgraph "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
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

			serverURL := os.Getenv("MEMORY_SERVER_URL")
			if serverURL == "" {
				serverURL = "https://memory.emergent-company.ai"
			}

			// Verify auth by counting objects (uses objects:read, not projects:read)
			objCount, err := cfg.Graph.CountObjects(ctx, &sdkgraph.CountObjectsOptions{})
			if err != nil {
				return fmt.Errorf("auth/connect failed: %w", err)
			}

			// Count relationships
			relCount := 0
			if rels, err := cfg.Graph.ListRelationships(ctx, &sdkgraph.ListRelationshipsOptions{
				Limit: 1,
			}); err == nil {
				relCount = rels.Total
			}

			// Print summary
			fmt.Fprintf(out, "Server:    %s\n", serverURL)
			fmt.Fprintf(out, "Auth:      ✓ OK\n")
			fmt.Fprintf(out, "Project:   %s\n", cfg.ProjectID)
			fmt.Fprintf(out, "Objects:   %d\n", objCount)
			fmt.Fprintf(out, "Relations: %d\n", relCount)

			return nil
		},
	}

	return cmd
}
