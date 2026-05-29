package constitutioncmd

import (
	"context"
	"fmt"
	"os"

	sdkgraph "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
	"github.com/mkucharz/codebase/cmd/codebase/internal/config"
	"github.com/spf13/cobra"
)

func newCreateCmd(flagProjectID *string, flagBranch *string) *cobra.Command {
	var flagName string

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create constitution-v1 with starter rules",
		Long: `Create the constitution-v1 object and seed it with starter rules.

This is called automatically by 'codebase onboard'. Use this command directly
if you want to (re)create the constitution without running the full onboard.

Starter rules are selected based on the app types detected in .codebase.yml:
  - backend:  APIEndpoint naming/quality rules (default)
  - cli:      CLI service/error-handling/testing rules
  - library:  Library export/doc/dependency rules

Use --app-type to override detection.
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := config.New(*flagProjectID, *flagBranch)
			if err != nil {
				return err
			}
			ctx := context.Background()

			appTypes := config.AppTypes()

			rules := selectRules(appTypes)

			fmt.Printf("Selected %d rules for app types: %v\n", len(rules), appTypes)
			return runCreate(ctx, c.Graph, flagName, rules)
		},
	}

	cmd.Flags().StringVar(&flagName, "name", "Codebase Constitution v1", "Constitution name")
	return cmd
}

// selectRules picks the appropriate rule set based on detected app types.
// When both backend and CLI/library exist (monorepo), merges rule sets.
func selectRules(appTypes []string) []ruleSpec {
	hasBackend := false
	hasCLI := false
	hasLibrary := false
	for _, t := range appTypes {
		switch t {
		case "backend":
			hasBackend = true
		case "cli":
			hasCLI = true
		case "library":
			hasLibrary = true
		}
	}

	if len(appTypes) == 0 || (hasBackend && !hasCLI && !hasLibrary) {
		return backendStarterRules
	}
	if hasCLI && !hasBackend && !hasLibrary {
		return cliStarterRules
	}
	if hasLibrary && !hasBackend && !hasCLI {
		return libraryStarterRules
	}

	// Multiple app types — merge all rule sets, deduplicating by key
	seen := make(map[string]bool)
	var merged []ruleSpec
	for _, set := range [][]ruleSpec{backendStarterRules, cliStarterRules, libraryStarterRules} {
		for _, r := range set {
			if !seen[r.Key] {
				seen[r.Key] = true
				merged = append(merged, r)
			}
		}
	}
	return merged
}

// ruleSpec defines a single constitution rule.
type ruleSpec struct {
	Key       string
	Name      string
	Statement string
	Category  string
	AppliesTo string
	AutoCheck string
	PropCheck string
}

// backendStarterRules are the original API-centric rules.
var backendStarterRules = []ruleSpec{
	{
		Key:       "rule-naming-api-endpoint-key",
		Name:      "APIEndpoint key prefix",
		Statement: "Every APIEndpoint key must start with 'ep-' followed by domain and handler slug.",
		Category:  "naming",
		AppliesTo: "APIEndpoint",
		AutoCheck: `^ep-[a-z][a-z0-9-]+$`,
	},
	{
		Key:       "rule-naming-service-key",
		Name:      "Service key prefix",
		Statement: "Every Service key must start with 'svc-' followed by domain slug.",
		Category:  "naming",
		AppliesTo: "Service",
		AutoCheck: `^svc-[a-z][a-z0-9-]+$`,
	},
	{
		Key:       "rule-naming-domain-key",
		Name:      "Domain key prefix",
		Statement: "Every Domain key must start with 'domain-' followed by the domain slug.",
		Category:  "naming",
		AppliesTo: "Domain",
		AutoCheck: `^domain-[a-z][a-z0-9-]+$`,
	},
	{
		Key:       "rule-api-has-method",
		Name:      "APIEndpoint must have HTTP method",
		Statement: "Every APIEndpoint must have a non-empty 'method' property (GET, POST, PUT, DELETE, PATCH).",
		Category:  "api",
		AppliesTo: "APIEndpoint",
		PropCheck: `{"field":"method","nonempty":true}`,
	},
	{
		Key:       "rule-api-has-path",
		Name:      "APIEndpoint must have path",
		Statement: "Every APIEndpoint must have a non-empty 'path' property starting with '/'.",
		Category:  "api",
		AppliesTo: "APIEndpoint",
		PropCheck: `{"field":"path","prefix":"/"}`,
	},
	{
		Key:       "rule-api-has-domain",
		Name:      "APIEndpoint must have domain",
		Statement: "Every APIEndpoint must have a 'domain' property matching its owning domain slug.",
		Category:  "api",
		AppliesTo: "APIEndpoint",
		PropCheck: `{"field":"domain","nonempty":true}`,
	},
	{
		Key:       "rule-api-auth-documented",
		Name:      "APIEndpoint auth must be documented",
		Statement: "Every APIEndpoint must have 'auth_required' set to true or false — never absent.",
		Category:  "api",
		AppliesTo: "APIEndpoint",
		PropCheck: `{"field":"auth_required","bool":true}`,
	},
	{
		Key:       "rule-coverage-high-risk-tested",
		Name:      "High-risk domains must have tests",
		Statement: "Domains with 10+ endpoints and no test coverage are high-risk and must have at least one test file.",
		Category:  "service",
		AppliesTo: "Domain",
	},
}

// cliStarterRules are rules for CLI (non-HTTP) applications.
var cliStarterRules = []ruleSpec{
	{
		Key:       "rule-naming-service-key",
		Name:      "Service key prefix",
		Statement: "Every Service key must start with 'svc-' followed by domain slug.",
		Category:  "naming",
		AppliesTo: "Service",
		AutoCheck: `^svc-[a-z][a-z0-9-]+$`,
	},
	{
		Key:       "rule-naming-domain-key",
		Name:      "Domain key prefix",
		Statement: "Every Domain key must start with 'domain-' followed by the domain slug.",
		Category:  "naming",
		AppliesTo: "Domain",
		AutoCheck: `^domain-[a-z][a-z0-9-]+$`,
	},
	{
		Key:       "rule-service-error-wrapping",
		Name:      "Errors must be wrapped with context",
		Statement: "Errors returned from services must be wrapped with fmt.Errorf(\"context: %w\", err) to preserve the call chain.",
		Category:  "service",
		AppliesTo: "Service",
	},
	{
		Key:       "rule-service-no-panic-library",
		Name:      "No panic() in library/service packages",
		Statement: "Service and library packages must never call panic() — return errors instead.",
		Category:  "service",
		AppliesTo: "Service",
	},
	{
		Key:       "rule-service-no-global-state",
		Name:      "No mutable package-level state",
		Statement: "Service and library packages must not have mutable package-level variables (outside main or test packages).",
		Category:  "service",
		AppliesTo: "Service",
	},
	{
		Key:       "rule-service-e2e-skip-no-creds",
		Name:      "Integration tests skip when env vars absent",
		Statement: "Integration/e2e tests must call t.Skip() when required environment credentials are not set, so CI passes without special secrets.",
		Category:  "testing",
		AppliesTo: "TestSuite",
	},
	{
		Key:       "rule-coverage-pure-logic-tested",
		Name:      "I/O-free services must have unit tests",
		Statement: "Domains with services that perform no I/O must have at least one unit test file covering the domain.",
		Category:  "testing",
		AppliesTo: "Domain",
	},
}

// libraryStarterRules are rules for library (no executable) projects.
var libraryStarterRules = []ruleSpec{
	{
		Key:       "rule-naming-domain-key",
		Name:      "Domain key prefix",
		Statement: "Every Domain key must start with 'domain-' followed by the domain slug.",
		Category:  "naming",
		AppliesTo: "Domain",
		AutoCheck: `^domain-[a-z][a-z0-9-]+$`,
	},
	{
		Key:       "rule-service-error-wrapping",
		Name:      "Errors must be wrapped with context",
		Statement: "Errors returned from exported functions must be wrapped with fmt.Errorf(\"context: %w\", err) to preserve the call chain.",
		Category:  "service",
		AppliesTo: "Service",
	},
	{
		Key:       "rule-service-no-panic-library",
		Name:      "No panic() in library code",
		Statement: "Library packages must never call panic() — return errors instead.",
		Category:  "service",
		AppliesTo: "Service",
	},
	{
		Key:       "rule-service-no-global-state",
		Name:      "No mutable package-level state",
		Statement: "Library packages must not have mutable package-level variables.",
		Category:  "service",
		AppliesTo: "Service",
	},
	{
		Key:       "rule-coverage-exported-tested",
		Name:      "Exported functions must have tests",
		Statement: "Every exported function or type in the public API should have at least one corresponding test.",
		Category:  "testing",
		AppliesTo: "Domain",
	},
	{
		Key:       "rule-minimize-external-deps",
		Name:      "Minimize external dependencies",
		Statement: "Library packages should minimize external imports. Prefer stdlib over third-party packages when reasonable.",
		Category:  "service",
		AppliesTo: "Service",
	},
}

func runCreate(ctx context.Context, gc *sdkgraph.Client, name string, rules []ruleSpec) error {
	constKey := "constitution-v1"
	constObj, err := gc.UpsertObject(ctx, &sdkgraph.CreateObjectRequest{
		Type: "Constitution",
		Key:  &constKey,
		Properties: map[string]any{
			"name":        name,
			"description": "Non-negotiable constraints for this codebase's knowledge graph.",
			"version":     "1",
		},
	})
	if err != nil {
		return fmt.Errorf("creating constitution: %w", err)
	}
	fmt.Printf("created  constitution-v1  (%s)\n", constObj.EntityID)

	for _, rule := range rules {
		key := rule.Key
		props := map[string]any{
			"name":      rule.Name,
			"statement": rule.Statement,
			"category":  rule.Category,
		}
		if rule.AppliesTo != "" {
			props["applies_to"] = rule.AppliesTo
		}
		if rule.AutoCheck != "" {
			props["auto_check"] = rule.AutoCheck
		}
		if rule.PropCheck != "" {
			props["prop_check"] = rule.PropCheck
		}

		ruleObj, err := gc.UpsertObject(ctx, &sdkgraph.CreateObjectRequest{
			Type:       "Rule",
			Key:        &key,
			Properties: props,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "warn: rule %s: %v\n", rule.Key, err)
			continue
		}

		_, err = gc.UpsertRelationship(ctx, &sdkgraph.CreateRelationshipRequest{
			Type:  "includes",
			SrcID: constObj.EntityID,
			DstID: ruleObj.EntityID,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "warn: wiring %s: %v\n", rule.Key, err)
			continue
		}
		fmt.Printf("  rule  %s\n", rule.Key)
	}

	fmt.Printf("\n%d rules wired to constitution-v1\n", len(rules))
	return nil
}
