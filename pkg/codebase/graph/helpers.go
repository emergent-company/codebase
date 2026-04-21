package graph

import (
	"context"
	"fmt"
	"strings"

	sdkgraph "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
)

// GetByKey looks up an object by its key field.
func GetByKey(ctx context.Context, gc GraphClient, key string) (*sdkgraph.GraphObject, error) {
	res, err := gc.ListObjects(ctx, &sdkgraph.ListObjectsOptions{Key: key, Limit: 1})
	if err != nil {
		return nil, err
	}
	for _, o := range res.Items {
		if o.Key != nil && *o.Key == key {
			return o, nil
		}
	}
	return nil, fmt.Errorf("object with key %q not found", key)
}

// GetByKeyAndType looks up an object by key and type.
func GetByKeyAndType(ctx context.Context, gc GraphClient, key, objType string) (*sdkgraph.GraphObject, error) {
	res, err := gc.ListObjects(ctx, &sdkgraph.ListObjectsOptions{Key: key, Type: objType, Limit: 1})
	if err != nil {
		return nil, err
	}
	for _, o := range res.Items {
		if o.Key != nil && *o.Key == key {
			return o, nil
		}
	}
	return nil, fmt.Errorf("object %q of type %s not found", key, objType)
}

// ListAll fetches all objects of a given type, handling pagination.
func ListAll(ctx context.Context, gc GraphClient, objType string) ([]*sdkgraph.GraphObject, error) {
	var all []*sdkgraph.GraphObject
	cursor := ""
	for {
		resp, err := gc.ListObjects(ctx, &sdkgraph.ListObjectsOptions{Type: objType, Limit: 500, Cursor: cursor})
		if err != nil {
			return nil, err
		}
		all = append(all, resp.Items...)
		if resp.NextCursor == nil || *resp.NextCursor == "" {
			break
		}
		cursor = *resp.NextCursor
	}
	return all, nil
}

// ListAllRels fetches all relationships of a given type, handling pagination.
func ListAllRels(ctx context.Context, gc GraphClient, relType string) ([]*sdkgraph.GraphRelationship, error) {
	var all []*sdkgraph.GraphRelationship
	cursor := ""
	for {
		resp, err := gc.ListRelationships(ctx, &sdkgraph.ListRelationshipsOptions{Type: relType, Limit: 500, Cursor: cursor})
		if err != nil {
			return nil, err
		}
		all = append(all, resp.Items...)
		if resp.NextCursor == nil || *resp.NextCursor == "" {
			break
		}
		cursor = *resp.NextCursor
	}
	return all, nil
}

// StrProp returns a string property from a graph object.
func StrProp(o *sdkgraph.GraphObject, key string) string {
	if o == nil || o.Properties == nil {
		return ""
	}
	v, _ := o.Properties[key].(string)
	return v
}

// DerefKey safely dereferences a *string key.
func DerefKey(k *string) string {
	if k == nil {
		return ""
	}
	return *k
}

// AnyPropStr returns the property value as a string regardless of its stored type.
func AnyPropStr(obj *sdkgraph.GraphObject, key string) string {
	if obj == nil || obj.Properties == nil {
		return ""
	}
	switch v := obj.Properties[key].(type) {
	case string:
		return v
	case bool:
		if v {
			return "true"
		}
		return "false"
	case float64:
		return fmt.Sprintf("%g", v)
	case int:
		return fmt.Sprintf("%d", v)
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", v)
	}
}

// AnyPropInt returns the property value as an int64.
func AnyPropInt(obj *sdkgraph.GraphObject, key string) int64 {
	if obj == nil || obj.Properties == nil {
		return 0
	}
	switch v := obj.Properties[key].(type) {
	case float64:
		return int64(v)
	case int:
		return int64(v)
	case int64:
		return v
	default:
		return 0
	}
}

// AnyPropBool returns the property value as a bool.
func AnyPropBool(obj *sdkgraph.GraphObject, key string) bool {
	if obj == nil || obj.Properties == nil {
		return false
	}
	switch v := obj.Properties[key].(type) {
	case bool:
		return v
	case string:
		return strings.ToLower(v) == "true"
	default:
		return false
	}
}

// FallbackTypes is the list of known object types for key-based lookup fallback.
var FallbackTypes = []string{
	"Scenario", "Context", "Action", "APIEndpoint", "ServiceMethod",
	"SQLQuery", "SourceFile", "ScenarioStep", "UIComponent", "Actor",
	"Domain", "TestSuite", "Pattern", "Rule", "Middleware",
}
