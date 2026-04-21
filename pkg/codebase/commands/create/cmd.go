package create

import (
	"context"
	"fmt"
	"io"

	sdkgraph "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
	"github.com/emergent-company/codebase/graph"
)

type CreateOptions struct {
	Upsert bool
	// Common props
	Name        string
	Description string
	// Context
	Route       string
	ContextType string
	// UIComponent / Helper
	CompType string
	// Action
	DisplayLabel string
	ActionType   string
	Domain       string
	// APIEndpoint
	Handler      string
	Method       string
	Path         string
	File         string
	AuthRequired bool
	// SourceFile
	Language string
	// Domain
	Slug string
	App  string
	// Scenario
	Given string
	When  string
	Then  string
	// Step
	Order    int
	Scenario string
}

func GenerateKey(objType, name string, opts *CreateOptions) string {
	switch objType {
	case "context":
		return ContextKey(name)
	case "uicomponent":
		return UIComponentKey(name)
	case "helper":
		return HelperKey(name)
	case "action":
		return ActionKey(opts.Domain, name)
	case "apiendpoint":
		h := opts.Handler
		if h == "" {
			h = name
		}
		return APIEndpointKey(opts.Domain, h)
	case "sourcefile":
		p := opts.Path
		if p == "" {
			p = name
		}
		return SourceFileKey(p)
	case "domain":
		s := opts.Slug
		if s == "" {
			s = name
		}
		return DomainKey(s)
	case "scenario":
		return ScenarioKey(name)
	case "step":
		return ScenarioStepKey(opts.Scenario, opts.Order)
	default:
		return Slugify(name)
	}
}

func RunCreate(ctx context.Context, g graph.GraphClient, objType string, name string, opts *CreateOptions, out io.Writer) error {
	key := GenerateKey(objType, name, opts)

	objs, err := g.ListObjects(ctx, &sdkgraph.ListObjectsOptions{
		Type: mapType(objType),
	})
	var existing *sdkgraph.GraphObject
	if err == nil {
		for _, o := range objs.Items {
			if o.Key != nil && *o.Key == key {
				existing = o
				break
			}
		}
	}

	if existing != nil {
		if !opts.Upsert {
			fmt.Fprintf(out, "already exists: %s\n", key)
			return nil
		}
		props := buildProps(objType, name, opts)
		_, err = g.UpdateObject(ctx, existing.EntityID, &sdkgraph.UpdateObjectRequest{
			Properties: props,
		})
		if err != nil {
			return err
		}
		fmt.Fprintln(out, key)
		return nil
	}

	props := buildProps(objType, name, opts)
	realType := mapType(objType)
	_, err = g.CreateObject(ctx, &sdkgraph.CreateObjectRequest{
		Type:       realType,
		Key:        &key,
		Properties: props,
	})
	if err != nil {
		return err
	}

	fmt.Fprintln(out, key)
	return nil
}

func mapType(objType string) string {
	switch objType {
	case "context":
		return "Context"
	case "uicomponent":
		return "UIComponent"
	case "helper":
		return "Helper"
	case "action":
		return "Action"
	case "apiendpoint":
		return "APIEndpoint"
	case "sourcefile":
		return "SourceFile"
	case "domain":
		return "Domain"
	case "scenario":
		return "Scenario"
	case "step":
		return "ScenarioStep"
	default:
		return objType
	}
}

func buildProps(objType, name string, opts *CreateOptions) map[string]any {
	p := make(map[string]any)
	displayName := opts.Name
	if displayName == "" {
		displayName = name
	}
	p["name"] = displayName
	if opts.Description != "" {
		p["description"] = opts.Description
	}

	switch objType {
	case "context":
		p["route"] = opts.Route
		p["context_type"] = opts.ContextType
		p["type"] = "screen"
	case "uicomponent", "helper":
		p["type"] = opts.CompType
	case "action":
		dl := opts.DisplayLabel
		if dl == "" {
			dl = displayName
		}
		p["display_label"] = dl
		p["type"] = opts.ActionType
	case "apiendpoint":
		p["handler"] = opts.Handler
		p["method"] = opts.Method
		p["path"] = opts.Path
		p["domain"] = opts.Domain
		p["file"] = opts.File
		p["auth_required"] = opts.AuthRequired
	case "sourcefile":
		path := opts.Path
		if path == "" {
			path = name
		}
		p["path"] = path
		p["language"] = opts.Language
	case "domain":
		p["slug"] = opts.Slug
		p["app"] = opts.App
	case "scenario":
		p["given"] = opts.Given
		p["when"] = opts.When
		p["then"] = opts.Then
	case "step":
		p["order"] = opts.Order
	}
	return p
}
