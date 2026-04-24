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
	types := []string{"context", "uicomponent", "helper", "action", "apiendpoint", "sourcefile", "domain", "scenario", "step", "actor",
		"competitor", "competitorfeature", "featuregap", "strategicinitiative", "markettrend", "capabilitymatrix", "comparisonpoint", "pricingmodel", "integration"}
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
	types := []string{"context", "uicomponent", "helper", "action", "apiendpoint", "sourcefile", "domain", "scenario", "step", "actor",
		"competitor", "competitorfeature", "featuregap", "strategicinitiative", "markettrend", "capabilitymatrix", "comparisonpoint", "pricingmodel", "integration"}
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
	case "actor":
		cmd.Flags().StringVar(&opts.Role, "role", "", "Actor role")
		cmd.Flags().StringVar(&opts.ActorType, "actor-type", "", "Actor type")
	case "competitor":
		cmd.Flags().StringVar(&opts.Status, "status", "", "Status: active, alpha, beta, mature, deprecated, unknown")
		cmd.Flags().StringVar(&opts.Category, "category", "", "Category: personal-agent, enterprise-agent, chat-ui, mcp-server, full-stack")
		cmd.Flags().StringVar(&opts.Maturity, "maturity", "", "Market maturity: experimental, growing, established, dominant")
		cmd.Flags().BoolVar(&opts.IsOpenSource, "open-source", false, "Is open source")
		cmd.Flags().StringVar(&opts.License, "license", "", "License (e.g. MIT, Apache-2.0)")
		cmd.Flags().StringVar(&opts.RepoURL, "repo-url", "", "Repository URL")
		cmd.Flags().StringVar(&opts.WebsiteURL, "website-url", "", "Website URL")
		cmd.Flags().StringVar(&opts.TechStack, "tech-stack", "", "Tech stack")
		cmd.Flags().StringVar(&opts.TargetAudience, "target-audience", "", "Target audience")
	case "competitorfeature":
		cmd.Flags().StringVar(&opts.Competitor, "competitor", "", "Competitor key (required for key generation)")
		cmd.Flags().StringVar(&opts.CapabilityArea, "capability-area", "", "Capability area")
		cmd.Flags().BoolVar(&opts.IsCore, "core", false, "Is core feature")
		cmd.Flags().StringVar(&opts.MaturityLevel, "maturity-level", "", "Maturity level: experimental, beta, stable, deprecated")
	case "featuregap":
		cmd.Flags().StringVar(&opts.CompetitorsHaveIt, "competitors-have-it", "", "Comma-separated competitor keys that have this feature")
		cmd.Flags().StringVar(&opts.Impact, "impact", "", "Impact: low, medium, high, critical")
		cmd.Flags().StringVar(&opts.EffortToAdd, "effort", "", "Effort to add: low, medium, high")
		cmd.Flags().BoolVar(&opts.IsBeingWorkedOn, "in-progress", false, "Is being worked on")
	case "strategicinitiative":
		cmd.Flags().StringVar(&opts.CompetitiveDriver, "competitive-driver", "", "What competitive pressure drives this")
		cmd.Flags().StringVar(&opts.Status, "status", "", "Status: planned, in-progress, done, cancelled")
		cmd.Flags().StringVar(&opts.Priority, "priority", "", "Priority: low, medium, high, critical")
		cmd.Flags().StringVar(&opts.TargetDate, "target-date", "", "Target date")
		cmd.Flags().StringVar(&opts.Owner, "owner", "", "Owner")
	case "markettrend":
		cmd.Flags().StringVar(&opts.ImpactOnDiane, "impact-on-diane", "", "How this trend impacts Diane")
		cmd.Flags().StringVar(&opts.ImpactLevel, "impact-level", "", "Impact level: low, medium, high, critical")
		cmd.Flags().StringVar(&opts.Source, "source", "", "Source of this trend observation")
		cmd.Flags().StringVar(&opts.ObservedDate, "observed-date", "", "Date first observed")
	case "capabilitymatrix":
		cmd.Flags().StringVar(&opts.AnalysisDate, "analysis-date", "", "Analysis date")
		cmd.Flags().StringVar(&opts.CompetitorsAnalyzed, "competitors", "", "Comma-separated competitor keys analyzed")
	case "comparisonpoint":
		cmd.Flags().StringVar(&opts.Competitor, "competitor", "", "Competitor key (required for key generation)")
		cmd.Flags().StringVar(&opts.Feature, "feature", "", "Feature/capability area (required for key generation)")
		cmd.Flags().StringVar(&opts.Assessment, "assessment", "", "Assessment: stronger, equal, weaker, missing")
		cmd.Flags().StringVar(&opts.Reasoning, "reasoning", "", "Reasoning for the assessment")
		cmd.Flags().StringVar(&opts.Evidence, "evidence", "", "Evidence or source")
		cmd.Flags().StringVar(&opts.Priority, "priority", "", "Priority: low, medium, high, critical")
	case "pricingmodel":
		cmd.Flags().StringVar(&opts.Competitor, "competitor", "", "Competitor key (required for key generation)")
		cmd.Flags().StringVar(&opts.ModelType, "model-type", "", "Model type: free, freemium, subscription, usage-based, enterprise, open-source")
		cmd.Flags().StringVar(&opts.PriceRange, "price-range", "", "Price range (e.g. '$0-20/mo')")
		cmd.Flags().StringVar(&opts.Currency, "currency", "USD", "Currency")
	case "integration":
		cmd.Flags().StringVar(&opts.Competitor, "competitor", "", "Competitor key (required for key generation)")
		cmd.Flags().StringVar(&opts.IntegrationType, "type", "", "Integration type: native, plugin, api, webhook, mcp")
		cmd.Flags().StringVar(&opts.MaturityLevel, "maturity-level", "", "Maturity level: experimental, beta, stable, deprecated")
	}
}
