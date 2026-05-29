package onboardcmd

import (
	"context"
	"fmt"
	"os"

	sdkgraph "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
)

// ruleSpec defines a single constitution rule.
type ruleSpec struct {
	Key       string
	Name      string
	Statement string
	Category  string
	AppliesTo string
	AutoCheck string
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
	},
	{
		Key:       "rule-api-has-path",
		Name:      "APIEndpoint must have path",
		Statement: "Every APIEndpoint must have a non-empty 'path' property starting with '/'.",
		Category:  "api",
		AppliesTo: "APIEndpoint",
	},
	{
		Key:       "rule-api-has-domain",
		Name:      "APIEndpoint must have domain",
		Statement: "Every APIEndpoint must have a 'domain' property matching its owning domain slug.",
		Category:  "api",
		AppliesTo: "APIEndpoint",
	},
	{
		Key:       "rule-api-auth-documented",
		Name:      "APIEndpoint auth must be documented",
		Statement: "Every APIEndpoint must have 'auth_required' set to true or false — never absent.",
		Category:  "api",
		AppliesTo: "APIEndpoint",
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

// selectRules picks the appropriate rule set based on detected app types.
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

	// Default: backend rules (backward compatible)
	if len(appTypes) == 0 || hasBackend {
		return backendStarterRules
	}

	if hasCLI {
		return cliStarterRules
	}

	if hasLibrary {
		return libraryStarterRules
	}

	return backendStarterRules
}

// createConstitution creates constitution-v1 and seeds starter rules, wiring
// each rule to the constitution via an 'includes' relationship.
// purpose describes the project's high-level goal and is stored on the constitution.
func createConstitution(ctx context.Context, gc *sdkgraph.Client, purpose string, rules []ruleSpec) error {
	constKey := "constitution-v1"

	props := map[string]any{
		"name":        "Codebase Constitution v1",
		"description": "Non-negotiable constraints for this codebase's knowledge graph. Created by codebase onboard.",
		"version":     "1",
	}
	if purpose != "" {
		props["purpose"] = purpose
	}

	constObj, err := gc.UpsertObject(ctx, &sdkgraph.CreateObjectRequest{
		Type:       "Constitution",
		Key:        &constKey,
		Properties: props,
	})
	if err != nil {
		return fmt.Errorf("creating constitution: %w", err)
	}

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

		ruleObj, err := gc.UpsertObject(ctx, &sdkgraph.CreateObjectRequest{
			Type:       "Rule",
			Key:        &key,
			Properties: props,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "warn: creating rule %s: %v\n", rule.Key, err)
			continue
		}

		_, err = gc.UpsertRelationship(ctx, &sdkgraph.CreateRelationshipRequest{
			Type:  "includes",
			SrcID: constObj.EntityID,
			DstID: ruleObj.EntityID,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "warn: wiring rule %s: %v\n", rule.Key, err)
		}
	}

	return nil
}
