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
)
