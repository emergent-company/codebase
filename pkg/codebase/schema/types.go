// Package schema defines the object type names and key patterns for the codebase graph schema.
package schema

// Object type names used in the graph.
const (
	TypeApp           = "App"
	TypeModule        = "Module"
	TypeSourceFile    = "SourceFile"
	TypeContext       = "Context"
	TypeUIComponent   = "UIComponent"
	TypeAction        = "Action"
	TypeService       = "Service"
	TypeDataModel     = "DataModel"
	TypeAPIEndpoint   = "APIEndpoint"
	TypeServiceMethod = "ServiceMethod"
	TypeSQLQuery      = "SQLQuery"
	TypeScenario      = "Scenario"
	TypeScenarioStep  = "ScenarioStep"
	TypeActor         = "Actor"
	TypeDomain        = "Domain"
	TypeTestSuite     = "TestSuite"
	TypePattern       = "Pattern"
	TypeRule          = "Rule"
	TypeConstitution  = "Constitution"
	TypeMiddleware    = "Middleware"
	TypeHelper        = "Helper"
	TypeEntity        = "Entity"
	TypeField         = "Field"
	TypeJob           = "Job"
	TypeMethod        = "Method"

	// Competitive landscape types (competitive-landscape pack)
	TypeCompetitor          = "Competitor"
	TypeCompetitorFeature   = "CompetitorFeature"
	TypeComparisonPoint     = "ComparisonPoint"
	TypeStrategicInitiative = "StrategicInitiative"
	TypeMarketTrend         = "MarketTrend"
	TypeCapabilityMatrix    = "CapabilityMatrix"
	TypeFeatureGap          = "FeatureGap"
	TypePricingModel        = "PricingModel"
	TypeIntegration         = "Integration"
)

// Relationship type names used in the graph.
const (
	RelHasStep        = "has_step"
	RelOccursIn       = "occurs_in"
	RelHasAction      = "has_action"
	RelBelongsTo      = "belongs_to"
	RelExposes        = "exposes"
	RelHandles        = "handles"
	RelTestedBy       = "tested_by"
	RelIncludes       = "includes"
	RelHasField       = "has_field"
	RelReferences     = "references"
	RelRequires       = "requires"
	RelCalls          = "calls"
	RelUses           = "uses"

	// Competitive landscape relationships (competitive-landscape pack)
	RelHasFeature          = "has_feature"
	RelComparesOn          = "compares_on"
	RelEvaluatedBy         = "evaluated_by"
	RelUsesPricing         = "uses_pricing"
	RelProvidesIntegration = "provides_integration"
	RelExposesGap          = "exposes_gap"
	RelRespondsTo          = "responds_to"
	RelClosesGap           = "closes_gap"
	RelCapturesTrend       = "captures_trend"
	RelImpacts             = "impacts"
	RelComparesAgainst     = "compares_against"
	RelDrives              = "drives"
)
