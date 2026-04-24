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
	// Actor
	Role      string
	ActorType string

	// Competitor
	Status         string
	Category       string
	Maturity       string
	IsOpenSource   bool
	License        string
	RepoURL        string
	WebsiteURL     string
	TechStack      string
	TargetAudience string

	// CompetitorFeature
	CapabilityArea string
	IsCore         bool
	MaturityLevel  string

	// ComparisonPoint
	Assessment string
	Reasoning  string
	Evidence   string
	Priority   string

	// StrategicInitiative
	CompetitiveDriver string
	TargetDate        string
	Owner             string

	// MarketTrend
	ImpactOnDiane string
	ImpactLevel   string
	Source        string
	ObservedDate  string

	// CapabilityMatrix
	AnalysisDate        string
	CompetitorsAnalyzed string

	// FeatureGap
	CompetitorsHaveIt string
	Impact            string
	EffortToAdd       string
	IsBeingWorkedOn   bool

	// PricingModel
	ModelType  string
	PriceRange string
	Currency   string

	// Integration
	IntegrationType string

	// Cross-pack linking (competitor → competitor feature, etc.)
	Competitor string
	Feature    string
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
	case "actor":
		return ActorKey(name)
	case "competitor":
		return CompetitorKey(name)
	case "competitorfeature":
		return CompetitorFeatureKey(opts.Competitor, name)
	case "featuregap":
		return FeatureGapKey(name)
	case "strategicinitiative":
		return StrategicInitiativeKey(name)
	case "markettrend":
		return MarketTrendKey(name)
	case "capabilitymatrix":
		return CapabilityMatrixKey(name)
	case "comparisonpoint":
		return ComparisonPointKey(opts.Competitor, opts.Feature)
	case "pricingmodel":
		return PricingModelKey(opts.Competitor)
	case "integration":
		return IntegrationKey(opts.Competitor, name)
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
	case "actor":
		return "Actor"
	case "competitor":
		return "Competitor"
	case "competitorfeature":
		return "CompetitorFeature"
	case "featuregap":
		return "FeatureGap"
	case "strategicinitiative":
		return "StrategicInitiative"
	case "markettrend":
		return "MarketTrend"
	case "capabilitymatrix":
		return "CapabilityMatrix"
	case "comparisonpoint":
		return "ComparisonPoint"
	case "pricingmodel":
		return "PricingModel"
	case "integration":
		return "Integration"
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
	case "actor":
		p["role"] = opts.Role
		p["actor_type"] = opts.ActorType
	case "competitor":
		if opts.Status != "" {
			p["status"] = opts.Status
		}
		if opts.Category != "" {
			p["category"] = opts.Category
		}
		if opts.Maturity != "" {
			p["maturity"] = opts.Maturity
		}
		p["is_open_source"] = opts.IsOpenSource
		if opts.License != "" {
			p["license"] = opts.License
		}
		if opts.RepoURL != "" {
			p["repo_url"] = opts.RepoURL
		}
		if opts.WebsiteURL != "" {
			p["website_url"] = opts.WebsiteURL
		}
		if opts.TechStack != "" {
			p["tech_stack"] = opts.TechStack
		}
		if opts.TargetAudience != "" {
			p["target_audience"] = opts.TargetAudience
		}
	case "competitorfeature":
		if opts.CapabilityArea != "" {
			p["capability_area"] = opts.CapabilityArea
		}
		p["is_core"] = opts.IsCore
		if opts.MaturityLevel != "" {
			p["maturity_level"] = opts.MaturityLevel
		}
	case "featuregap":
		if opts.CompetitorsHaveIt != "" {
			p["competitors_have_it"] = opts.CompetitorsHaveIt
		}
		if opts.Impact != "" {
			p["impact"] = opts.Impact
		}
		if opts.EffortToAdd != "" {
			p["effort_to_add"] = opts.EffortToAdd
		}
		p["is_being_worked_on"] = opts.IsBeingWorkedOn
	case "strategicinitiative":
		if opts.CompetitiveDriver != "" {
			p["competitive_driver"] = opts.CompetitiveDriver
		}
		if opts.Status != "" {
			p["status"] = opts.Status
		}
		if opts.Priority != "" {
			p["priority"] = opts.Priority
		}
		if opts.TargetDate != "" {
			p["target_date"] = opts.TargetDate
		}
		if opts.Owner != "" {
			p["owner"] = opts.Owner
		}
	case "markettrend":
		if opts.ImpactOnDiane != "" {
			p["impact_on_diane"] = opts.ImpactOnDiane
		}
		if opts.ImpactLevel != "" {
			p["impact_level"] = opts.ImpactLevel
		}
		if opts.Source != "" {
			p["source"] = opts.Source
		}
		if opts.ObservedDate != "" {
			p["observed_date"] = opts.ObservedDate
		}
	case "capabilitymatrix":
		if opts.AnalysisDate != "" {
			p["analysis_date"] = opts.AnalysisDate
		}
		if opts.CompetitorsAnalyzed != "" {
			p["competitors_analyzed"] = opts.CompetitorsAnalyzed
		}
	case "comparisonpoint":
		if opts.Assessment != "" {
			p["assessment"] = opts.Assessment
		}
		if opts.Reasoning != "" {
			p["reasoning"] = opts.Reasoning
		}
		if opts.Evidence != "" {
			p["evidence"] = opts.Evidence
		}
		if opts.Priority != "" {
			p["priority"] = opts.Priority
		}
	case "pricingmodel":
		if opts.ModelType != "" {
			p["model_type"] = opts.ModelType
		}
		if opts.PriceRange != "" {
			p["price_range"] = opts.PriceRange
		}
		if opts.Currency != "" {
			p["currency"] = opts.Currency
		}
	case "integration":
		if opts.IntegrationType != "" {
			p["integration_type"] = opts.IntegrationType
		}
		if opts.MaturityLevel != "" {
			p["maturity_level"] = opts.MaturityLevel
		}
	}
	return p
}
