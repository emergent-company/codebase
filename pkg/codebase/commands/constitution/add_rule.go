package constitution

import (
	"context"
	"fmt"
	"io"

	sdkgraph "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
	cbgraph "github.com/emergent-company/codebase/graph"
	"github.com/emergent-company/codebase/schema"
)

// RunAddRule creates a Rule object and wires it to constitution-v1.
func RunAddRule(ctx context.Context, gc cbgraph.GraphClient, w io.Writer, key, name, statement, category, appliesTo, autoCheck, propCheck, relationCheck, rationale, auditType string) error {
	props := map[string]any{
		"name":      name,
		"statement": statement,
		"category":  category,
	}
	if appliesTo != "" {
		props["applies_to"] = appliesTo
	}
	if autoCheck != "" {
		props["auto_check"] = autoCheck
	}
	if propCheck != "" {
		props["prop_check"] = propCheck
	}
	if relationCheck != "" {
		props["relation_check"] = relationCheck
	}
	if rationale != "" {
		props["rationale"] = rationale
	}
	if auditType != "" {
		props["audit_type"] = auditType
	}

	ruleObj, err := gc.UpsertObject(ctx, &sdkgraph.CreateObjectRequest{
		Type:       schema.TypeRule,
		Key:        &key,
		Properties: props,
	})
	if err != nil {
		return fmt.Errorf("creating rule: %w", err)
	}

	// Wire to constitution-v1 if it exists
	constResp, err := gc.ListObjects(ctx, &sdkgraph.ListObjectsOptions{Key: "constitution-v1", Type: schema.TypeConstitution, Limit: 1})
	if err == nil && len(constResp.Items) > 0 {
		constObj := constResp.Items[0]
		if cbgraph.DerefKey(constObj.Key) == "constitution-v1" {
			_, err = gc.UpsertRelationship(ctx, &sdkgraph.CreateRelationshipRequest{
				Type:  schema.RelIncludes,
				SrcID: constObj.EntityID,
				DstID: ruleObj.EntityID,
			})
			if err != nil {
				fmt.Fprintf(w, "warn: wiring to constitution: %v\n", err)
			}
		}
	}

	fmt.Fprintf(w, "created  %s  (%s)\n", key, ruleObj.EntityID)
	return nil
}
