// codebase — knowledge graph CLI for codebase analysis and sync.
//
// Reads project config from .codebase.yml (walked up from cwd) and
// authenticates via the Memory SDK (MEMORY_API_KEY / ~/.memory/config.yaml).
//
// Install:
//
//	task codebase:install   → /usr/local/bin/codebase
//
// Usage:
//
//	codebase sync routes
//	codebase check api
//	codebase graph list --type APIEndpoint
//	codebase --help
package main

import (
	"fmt"
	"os"

	analyzecmd "github.com/mkucharz/codebase/cmd/codebase/internal/analyze"
	branchcmd "github.com/mkucharz/codebase/cmd/codebase/internal/branch"
	checkcmd "github.com/mkucharz/codebase/cmd/codebase/internal/check"
	constitutioncmd "github.com/mkucharz/codebase/cmd/codebase/internal/constitution"
	createcmd "github.com/mkucharz/codebase/cmd/codebase/internal/create"
	discovercmd "github.com/mkucharz/codebase/cmd/codebase/internal/discover"
	fixcmd "github.com/mkucharz/codebase/cmd/codebase/internal/fix"
	graphcmd "github.com/mkucharz/codebase/cmd/codebase/internal/graph"
	onboardcmd "github.com/mkucharz/codebase/cmd/codebase/internal/onboard"
	seedcmd "github.com/mkucharz/codebase/cmd/codebase/internal/seed"
	skillscmd "github.com/mkucharz/codebase/cmd/codebase/internal/skills"
	statuscmd "github.com/mkucharz/codebase/cmd/codebase/internal/status"
	synccmd "github.com/mkucharz/codebase/cmd/codebase/internal/sync"
	upgradecmd "github.com/mkucharz/codebase/cmd/codebase/internal/upgrade"
	"github.com/spf13/cobra"
)

// Build-time version info — injected via ldflags.
var (
	Version   = "dev"
	Commit    = "none"
	BuildDate = "unknown"
)

var (
	flagProjectID string
	flagBranch    string
	flagFormat    string
	flagServer    string
)

var rootCmd = &cobra.Command{
	Use:     "codebase",
	Short:   "Knowledge graph CLI for codebase analysis and sync",
	Version: Version,
	Long: `codebase — populate, audit, and explore the Memory knowledge graph for your codebase.

Auth: reads from CODEBASE_API_KEY env var, .codebase.yml api_key, or ~/.memory/config.yaml.
Project: reads from CODEBASE_PROJECT_ID env var, .codebase.yml project_id or project name.

Examples:
  codebase sync routes          # populate APIEndpoint objects from route files
  codebase sync middleware      # wire Middleware→APIEndpoint relationships
  codebase check api            # audit APIEndpoint quality
  codebase check coverage       # test coverage gaps by domain
  codebase check complexity     # domain complexity scores
  codebase analyze tree         # Domain->Service->Endpoint map
  codebase graph list --type APIEndpoint --all
  codebase graph get ep-agents-listagents
  codebase fix stale            # remove stale graph objects
`,
}

func init() {
	rootCmd.PersistentFlags().StringVar(&flagProjectID, "project-id", "", "Memory project ID (overrides .codebase.yml and MEMORY_PROJECT_ID)")
	rootCmd.PersistentFlags().StringVar(&flagBranch, "branch", "", "Graph branch ID (default: main branch)")
	rootCmd.PersistentFlags().StringVar(&flagFormat, "format", "table", "Output format: table, json, markdown")
	rootCmd.PersistentFlags().StringVar(&flagServer, "server", "", "Memory server URL (overrides ~/.memory/config.yaml and .codebase.yml)")
	rootCmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		if flagServer != "" {
			os.Setenv("MEMORY_SERVER_URL", flagServer)
		}
		return nil
	}

	// Register command groups
	rootCmd.AddCommand(onboardcmd.NewCmd(&flagProjectID, &flagBranch))
	rootCmd.AddCommand(synccmd.NewCmd(&flagProjectID, &flagBranch, &flagFormat))
	rootCmd.AddCommand(checkcmd.NewCmd(&flagProjectID, &flagBranch, &flagFormat))
	rootCmd.AddCommand(analyzecmd.NewCmd(&flagProjectID, &flagBranch, &flagFormat))
	rootCmd.AddCommand(graphcmd.NewCmd(&flagProjectID, &flagBranch, &flagFormat))
	rootCmd.AddCommand(seedcmd.NewCmd(&flagProjectID, &flagBranch, &flagFormat))
	rootCmd.AddCommand(fixcmd.NewCmd(&flagProjectID, &flagBranch, &flagFormat))
	rootCmd.AddCommand(branchcmd.NewCmd(&flagProjectID))
	rootCmd.AddCommand(skillscmd.NewCmd())
	rootCmd.AddCommand(statuscmd.NewCmd(&flagProjectID, &flagBranch))
	rootCmd.AddCommand(upgradecmd.NewCmd(&Version))
	rootCmd.AddCommand(constitutioncmd.NewCmd(&flagProjectID, &flagBranch, &flagFormat))
	rootCmd.AddCommand(discovercmd.NewCmd(&flagProjectID))
	createcmd.Register(rootCmd, &flagProjectID, &flagBranch, &flagFormat)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
