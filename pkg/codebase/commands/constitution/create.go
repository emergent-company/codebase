package constitution

import (
	"context"
	"fmt"
	"io"

	sdkgraph "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
	cbgraph "github.com/emergent-company/codebase/graph"
	"github.com/emergent-company/codebase/schema"
)

// RunCreate creates constitution-v1 object and seeds it with starter rules.
func RunCreate(ctx context.Context, gc cbgraph.GraphClient, w io.Writer, name string) error {
	constKey := "constitution-v1"
	constObj, err := gc.UpsertObject(ctx, &sdkgraph.CreateObjectRequest{
		Type: schema.TypeConstitution,
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
	fmt.Fprintf(w, "created  constitution-v1  (%s)\n", constObj.EntityID)

	for _, rule := range StarterRules {
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
			Type:       schema.TypeRule,
			Key:        &key,
			Properties: props,
		})
		if err != nil {
			fmt.Fprintf(w, "warn: rule %s: %v\n", rule.Key, err)
			continue
		}

		_, err = gc.UpsertRelationship(ctx, &sdkgraph.CreateRelationshipRequest{
			Type:  schema.RelIncludes,
			SrcID: constObj.EntityID,
			DstID: ruleObj.EntityID,
		})
		if err != nil {
			fmt.Fprintf(w, "warn: wiring %s: %v\n", rule.Key, err)
			continue
		}
		fmt.Fprintf(w, "  rule  %s\n", rule.Key)
	}

	fmt.Fprintf(w, "\n%d rules wired to constitution-v1\n", len(StarterRules))
	return nil
}

// StarterRules mirrors the ones in onboard/constitution.go.
var StarterRules = []struct {
	Key       string
	Name      string
	Statement string
	Category  string
	AppliesTo string
	AutoCheck string
	PropCheck string // JSON PropCheckSpec
}{
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
