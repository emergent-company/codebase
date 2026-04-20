# codebase

An [Emergent](https://emergent.company) blueprint + CLI for mapping any software codebase into a Memory knowledge graph. Installs the **code-structure** template pack (21 object types, 42 relationship types) and ships the **`codebase` CLI** to populate, audit, and explore the graph.

Works for any language or framework — Go, TypeScript, Python, Swift, Rust, etc.

---

## What's included

| Component | Description |
|---|---|
| **Blueprint** (`packs/`) | `code-structure` template pack — schema only, no agents, no seed data |
| **`codebase` CLI** (`cmd/codebase/`) | Sync, check, analyze, and manage your codebase knowledge graph |
| **`graph-to-swagger`** (`tools/`) | Reconstruct an OpenAPI spec from the graph |

---

## Quick start

### 1. Install the blueprint

```bash
memory blueprints https://github.com/mkucharz/codebase --project <project-slug>
```

Or from a local clone:

```bash
git clone https://github.com/mkucharz/codebase
memory blueprints ./codebase --project <project-slug>
```

This installs the `code-structure` template pack into your Emergent project.

### 2. Install the CLI

Download the latest release binary for your platform from [GitHub Releases](https://github.com/mkucharz/codebase/releases):

```bash
# macOS (Apple Silicon)
curl -L https://github.com/mkucharz/codebase/releases/latest/download/codebase-darwin-arm64 -o /usr/local/bin/codebase
chmod +x /usr/local/bin/codebase

# macOS (Intel)
curl -L https://github.com/mkucharz/codebase/releases/latest/download/codebase-darwin-amd64 -o /usr/local/bin/codebase
chmod +x /usr/local/bin/codebase

# Linux (amd64)
curl -L https://github.com/mkucharz/codebase/releases/latest/download/codebase-linux-amd64 -o /usr/local/bin/codebase
chmod +x /usr/local/bin/codebase
```

> **Note:** The CLI requires an [Emergent](https://emergent.company) account and API key. Set `MEMORY_API_KEY` or configure `~/.memory/config.yaml`.

### 3. Configure

Create a `.codebase.yml` in your project root (see [Configuration](#configuration)).

---

## CLI reference

### `codebase sync`

Populate the graph by reading your codebase.

```bash
codebase sync routes       # sync HTTP routes → APIEndpoint objects
codebase sync middleware   # sync middleware → Middleware objects
codebase sync files        # sync source files → SourceFile objects
codebase sync components   # sync UI components → UIComponent objects
codebase sync actions      # sync actions → Action objects
codebase sync scenarios    # sync scenarios → Scenario objects
```

### `codebase check`

Audit the graph for gaps and inconsistencies.

```bash
codebase check api         # check API endpoint coverage
codebase check coverage    # check test coverage
codebase check complexity  # check complexity metrics
codebase check logic       # check business logic completeness
```

### `codebase analyze`

Explore and visualize the graph.

```bash
codebase analyze tree       # print dependency tree
codebase analyze uml        # generate UML diagram
codebase analyze scenarios  # list all scenarios
codebase analyze contexts   # list all contexts
```

### `codebase graph`

Direct CRUD on graph objects.

```bash
codebase graph get <key>
codebase graph list <type>
codebase graph create <type> [properties]
codebase graph delete <key>
```

### `codebase constitution`

Manage architectural rules.

```bash
codebase constitution rules       # list all rules
codebase constitution check       # run all rules against the graph
codebase constitution add-rule    # add a new rule interactively
```

### `codebase fix`

Repair graph inconsistencies.

```bash
codebase fix stale    # remove stale objects
codebase fix rewire   # rewire broken relationships
```

### `codebase branch`

Work with Memory branches.

```bash
codebase branch verify   # verify branch consistency
```

### `codebase seed`

Seed the graph with initial data.

```bash
codebase seed entities   # seed entity objects
codebase seed exposes    # seed exposes relationships
```

### `codebase skills`

Manage agent skills.

```bash
codebase skills install   # install bundled skills
```

---

## Configuration

Create `.codebase.yml` in your project root:

```yaml
# .codebase.yml
project: my-project-slug   # Emergent project slug
server: https://memory.emergent-company.ai   # Memory server URL (optional)
```

Auth via environment variable or config file:

```bash
export MEMORY_API_KEY=your-api-key
# or configure ~/.memory/config.yaml
```

---

## Blueprint: code-structure template pack

### Object types (21 across 6 layers)

| Layer | Types |
|-------|-------|
| Project structure | `App`, `Module`, `SourceFile` |
| UI surfaces | `Context`, `UIComponent`, `Action` |
| Behavior | `Actor`, `Scenario`, `ScenarioStep` |
| Backend | `Service`, `DataModel`, `Database` |
| API & async | `APIEndpoint`, `APIContract`, `Event`, `Middleware`, `Job` |
| Supporting | `ExternalDependency`, `ConfigVar`, `Pattern`, `TestSuite` |

**42 relationship types** across all layers — see [Relationship reference](#relationship-reference) below.

No agents. No seed data. You populate the graph yourself, or let an agent do it by reading your codebase.

---

## Object type reference

### App
A deployable application unit — frontend, backend, mobile, desktop, CLI, or library. Top-level container in the project hierarchy.

| Property | Type | Description |
|---|---|---|
| `app_type` | string | `frontend`, `backend`, `mobile`, `desktop`, `cli`, `library` |
| `platform` | string[] | Target platforms. E.g. `[web]`, `[ios, android]` |
| `root_path` | string | Root directory relative to repo. E.g. `apps/web` |
| `tech_stack` | string[] | Key technologies. E.g. `[react, typescript, vite]` |
| `deployment_target` | string | Where it deploys. E.g. `vercel`, `kubernetes` |
| `port` | number | Default local development port |

### Module
A sub-package or module within an app — Go package, npm package, Python module, feature folder, or any named unit of code organization below the app level.

| Property | Type | Description |
|---|---|---|
| `path` | string | Path relative to app root. E.g. `internal/auth` |
| `language` | string | Primary language. E.g. `go`, `typescript` |
| `purpose` | string | What this module does |

### SourceFile
A tracked source file within a module. All code-level definitions (services, models, components, etc.) link back to their source file via `defines_*` relationships rather than storing a string path.

| Property | Type | Description |
|---|---|---|
| `path` | string | File path relative to repo root |
| `language` | string | Programming language |
| `purpose` | string | What this file contains or does |

### Context
A screen, modal, panel, or interaction surface — a React page, Next.js route, SwiftUI View, modal dialog, drawer, or any named surface where a user interacts.

| Property | Type | Description |
|---|---|---|
| `context_type` | string | Medium: `web-view`, `mobile-screen`, `desktop-window`, `cli`, `notification`, `email`, `watch-face` |
| `type` | string | Surface kind: `screen`, `modal`, `panel`, `drawer`, `bottom-sheet`, `toast` |
| `scope` | string | `internal`, `external` |
| `route` | string | URL route or navigation path. E.g. `/dashboard` |
| `platform` | string[] | Target platforms |

### UIComponent
A reusable UI component — React component, SwiftUI View, widget, form field, layout primitive, or any named self-contained piece of UI.

| Property | Type | Description |
|---|---|---|
| `type` | string | `primitive`, `composite`, `layout`, `container` |

### Action
A user action or system operation — navigation, data mutation, toggle, form submission, external call. Available within Contexts; performed during Scenario steps.

| Property | Type | Description |
|---|---|---|
| `type` | string | `navigation`, `mutation`, `trigger`, `toggle`, `external` |
| `display_label` | string | Human-readable label. E.g. `Save Changes` |

### Actor
A user role, persona, or system actor that executes scenarios — guest, member, admin, anonymous user, external system.

| Property | Type | Description |
|---|---|---|
| `display_name` | string | Human-readable name. E.g. `Guest User` |

### Scenario
A concrete, testable example of behavior — a user story expressed as given/when/then.

| Property | Type | Description |
|---|---|---|
| `title` | string | E.g. `Valid Credentials Login` |
| `given` | string | Precondition |
| `when` | string | Triggering action |
| `then` | string | Expected outcome |
| `and_also` | string[] | Additional outcomes |

### ScenarioStep
An ordered step within a complex scenario. Optional — simple scenarios use just given/when/then. Steps pin to a specific Context and Action.

| Property | Type | Description |
|---|---|---|
| `sequence` | number | Step order: 1, 2, 3, … |

### Service
A business logic service — `UserService`, `AuthService`, `PaymentService`, etc. Encapsulates domain operations and orchestrates data access.

### DataModel
A domain data type, schema, or DTO shared across the system — Go struct, TypeScript interface, Pydantic model, Protobuf message, database table schema.

| Property | Type | Description |
|---|---|---|
| `language_type` | string | Language-specific type name if different from object name |
| `fields` | string[] | Key field names |
| `persistence` | string | How instances are stored. E.g. `postgres`, `memory`, `none` |

### Database / Store
A persistence or state store — Postgres, Redis, SQLite, S3, Zustand, Redux slice.

| Property | Type | Description |
|---|---|---|
| `kind` | string | `relational`, `key-value`, `document`, `object-storage`, `client-state` |
| `technology` | string | E.g. `postgres`, `redis`, `sqlite`, `zustand` |
| `host` | string | Hostname or connection hint |

### APIEndpoint
A single HTTP, gRPC, GraphQL, or WebSocket endpoint.

| Property | Type | Description |
|---|---|---|
| `method` | string | E.g. `GET`, `POST`, `PUT`, `DELETE`, `rpc` |
| `path` | string | URL path or RPC method. E.g. `/api/v1/users` |
| `auth_required` | boolean | Whether authentication is required |

### APIContract
A machine-readable API definition grouping multiple endpoints — OpenAPI/Swagger file, Protobuf file, GraphQL schema.

| Property | Type | Description |
|---|---|---|
| `format` | string | `openapi`, `protobuf`, `graphql` |
| `version` | string | API version. E.g. `v1` |
| `file_path` | string | Path to the spec file |
| `base_url` | string | Base URL for the API |

### Event / Message
A pub/sub event, message queue message type, WebSocket event name, or domain event.

| Property | Type | Description |
|---|---|---|
| `channel` | string | Queue name, topic, or event channel. E.g. `user.created` |
| `transport` | string | `kafka`, `rabbitmq`, `websocket`, `sns`, `in-process` |

### Middleware
A request pipeline handler — auth, logging, rate limiting, CORS, tracing, error handling.

| Property | Type | Description |
|---|---|---|
| `kind` | string | `auth`, `logging`, `rate-limiting`, `cors`, `tracing`, `error-handling` |
| `applies_to` | string | Scope. E.g. `all routes`, `/api/*`, `admin only` |

### Job / Worker
A background job, cron task, or queue worker.

| Property | Type | Description |
|---|---|---|
| `kind` | string | `cron`, `queue-worker`, `one-off`, `scheduled` |
| `schedule` | string | Cron expression. E.g. `0 * * * *` |

### ExternalDependency
A third-party library, SDK, or external service — npm package, Go module, Python package, SaaS API.

| Property | Type | Description |
|---|---|---|
| `kind` | string | `library`, `sdk`, `saas-api`, `infrastructure` |
| `version` | string | Version constraint. E.g. `^18.0.0` |
| `registry` | string | `npm`, `go-modules`, `pypi`, `cargo` |

### ConfigVar
An environment variable, feature flag, or configuration key the app reads at runtime.

| Property | Type | Description |
|---|---|---|
| `key` | string | Variable name. E.g. `DATABASE_URL` |
| `required` | boolean | Whether the app fails to start without this |
| `default_value` | string | Default if not set |
| `secret` | boolean | Whether this holds a secret and should not be logged |

### Pattern
A recurring implementation pattern observed or mandated in the codebase — repository pattern, optimistic UI update, retry with backoff.

| Property | Type | Description |
|---|---|---|
| `kind` | string | `architectural`, `ui`, `data-access`, `error-handling`, `concurrency` |
| `scope` | string | `backend`, `frontend`, `data-layer`, `global` |
| `example_path` | string | Path to a canonical example file |
| `usage_guidance` | string | When and how to apply this pattern |

### TestSuite
A test file, test group, or spec file — unit tests, integration tests, e2e specs.

| Property | Type | Description |
|---|---|---|
| `kind` | string | `unit`, `integration`, `e2e`, `snapshot` |
| `framework` | string | `jest`, `vitest`, `go-test`, `pytest`, `xctest` |
| `coverage_percent` | number | Approximate coverage percentage |

---

## Relationship reference

### App-level

| Relationship | From → To | Meaning |
|---|---|---|
| `depends_on_app` | App → App | Runtime or build-time dependency between apps |
| `contains_module` | App → Module | An app is composed of modules |
| `uses_dependency` | App, Module → ExternalDependency | Depends on a third-party library or service |
| `configured_by` | App, Module, Service → ConfigVar | Reads a config variable at runtime |
| `uses_middleware` | App, Module → Middleware | Applies middleware to its request pipeline |

### Module / SourceFile structure

| Relationship | From → To | Meaning |
|---|---|---|
| `contains_file` | Module → SourceFile | A module owns a source file |
| `defines_context` | SourceFile → Context | A file implements a screen or surface |
| `defines_component` | SourceFile → UIComponent | A file implements a UI component |
| `defines_service` | SourceFile → Service | A file implements a service |
| `defines_model` | SourceFile → DataModel | A file declares a data model |
| `defines_action` | SourceFile → Action | A file implements an action handler |
| `defines_middleware` | SourceFile → Middleware | A file implements a middleware handler |
| `defines_job` | SourceFile → Job | A file implements a background job |
| `defines_test_suite` | SourceFile → TestSuite | A file is the test file for a suite |
| `defines_interface` | SourceFile → Service | A file declares a service's interface |
| `entry_point_of` | SourceFile → App | A file is the main entry point for an app |
| `contains_context` | Module → Context | A module defines a surface |
| `contains_component` | Module → UIComponent | A module defines a UI component |
| `contains_action` | Module → Action | A module defines an action |
| `contains_service` | Module → Service | A module owns a service |
| `depends_on` | Module, Service → Module, Service | Import or runtime dependency |
| `provides_model` | Module, Service → DataModel | Owns and defines a data model |
| `exposes_endpoint` | Module, Service → APIEndpoint | Exposes an API endpoint |
| `grouped_in` | APIEndpoint → APIContract | An endpoint belongs to a contract |

### Context / UIComponent / Action

| Relationship | From → To | Meaning |
|---|---|---|
| `uses_component` | Context → UIComponent | A context renders a UI component |
| `nested_in` | Context → Context | A context is nested inside another (modal in screen) |
| `composed_of` | UIComponent → UIComponent | A component is built from other components |
| `available_in` | Action → Context | An action is available within a context |
| `navigates_to` | Action → Context | An action navigates to a target context |
| `uses_service` | Context, Action, UIComponent → Service | Calls a frontend or backend service |
| `calls_endpoint` | Context, Action, UIComponent → APIEndpoint | Directly invokes an API endpoint |
| `consumes_model` | Module, Service, Context, Action, UIComponent → DataModel | Uses a data model as payload, response shape, or state type |

### Scenario

| Relationship | From → To | Meaning |
|---|---|---|
| `executed_by` | Scenario → Actor | A scenario is executed by a user role |
| `has_step` | Scenario → ScenarioStep | A scenario contains an ordered step |
| `variant_of` | Scenario → Scenario | An alternative path of another scenario |
| `occurs_in` | ScenarioStep → Context | A step takes place in a specific context |
| `performs` | ScenarioStep → Action | A step performs a specific action |
| `inherits_from` | Actor → Actor | An actor inherits from another (e.g. admin from member) |

### Service / persistence

| Relationship | From → To | Meaning |
|---|---|---|
| `calls_service` | Service → Service | A service delegates to another service |
| `reads_from` | Service, Job → Database | Reads data from a store |
| `writes_to` | Service, Job → Database | Writes data to a store |
| `stores_model` | Database → DataModel | A store persists a data model |

### Events

| Relationship | From → To | Meaning |
|---|---|---|
| `publishes_event` | Service, Action, Job → Event | Emits an event or message |
| `subscribes_to` | Service, Action, Job → Event | Consumes an event or message |

### Jobs

| Relationship | From → To | Meaning |
|---|---|---|
| `triggers_job` | Service, Action, Job → Job | Enqueues or schedules a background job |

### Patterns & tests

| Relationship | From → To | Meaning |
|---|---|---|
| `uses_pattern` | Module, Service, Context, UIComponent, Action → Pattern | Follows a named pattern |
| `extends_pattern` | Pattern → Pattern | One pattern specializes another |
| `tested_by` | Context, UIComponent, Service, Action, Module, Scenario → TestSuite | Covered by a test suite |

---

## The dependency traceability chain

A key capability this schema enables is tracing the full complexity path of any feature:

```
Context
  → calls_endpoint → APIEndpoint
      ← exposes_endpoint ← Service
          → reads_from / writes_to → Database
              ← stores_model ← DataModel

Context
  → uses_component → UIComponent
      → calls_endpoint → APIEndpoint

Action
  → calls_endpoint → APIEndpoint
  → triggers_job → Job
  → publishes_event → Event
      ← subscribes_to ← Service
```

This lets you answer: "If I change this endpoint, what UI surfaces are affected?" or "What does this screen actually depend on all the way to the database?"

---

## Tools

### `graph-to-swagger`

Reconstructs an OpenAPI 2.0 (Swagger) spec from the knowledge graph. Reads `APIContract`, `APIEndpoint`, and `DataModel` objects and emits a valid `swagger.json`. Achieves 100% round-trip fidelity when the graph was populated with full OpenAPI data (`summary`, `tags`, `parameters`, `responses`, `openapi_schema`, `swagger_name`).

**Requirements:** Python 3.8+, `memory` CLI installed.

```bash
# Basic usage — writes swagger.json in current directory
python3 tools/graph-to-swagger

# Custom output path
python3 tools/graph-to-swagger -o api/swagger.json

# Diff against an existing swagger file
python3 tools/graph-to-swagger --diff apps/server/docs/swagger/swagger.json

# Only include endpoints wired to the contract via grouped_in
python3 tools/graph-to-swagger --filter-contract -o contract-only.json

# Point at a specific Memory server
python3 tools/graph-to-swagger --server https://api.example.com -o swagger.json

# Compact output (no indent)
python3 tools/graph-to-swagger --compact -o swagger.min.json
```

**Options:**

| Flag | Default | Description |
|---|---|---|
| `-o`, `--output` | `swagger.json` | Output file path |
| `-c`, `--contract` | first found | APIContract key to use |
| `--server` | `http://localhost:3012` | Memory server URL |
| `--memory-bin` | `~/.memory/bin/memory` | Path to memory CLI binary |
| `--filter-contract` | off | Only include endpoints wired via `grouped_in` |
| `--diff FILE` | — | Diff output against an existing swagger file |
| `--compact` | off | Compact JSON (no indent) |
| `-q`, `--quiet` | off | Suppress progress output |

**Graph requirements** — the following properties must be populated for full fidelity:

| Object | Properties needed |
|---|---|
| `APIContract` | `title`, `version`, `description`, `base_url` |
| `APIEndpoint` | `path`, `method`, `summary`, `description`, `tags`, `auth_required`, `parameters`, `responses` |
| `DataModel` | `swagger_name` (full OpenAPI key, e.g. `domain_agents.AgentDTO`), `openapi_schema` |

---

## Design decisions

- **SourceFile as first-class object** — all code artifacts (services, models, components, actions, jobs, middleware, test suites) link to their implementing file via `defines_*` relationships. No string `file_path` properties on domain types; the graph itself is the index.
- **Context replaces Page** — `Context` is medium-agnostic: it covers web views, mobile screens, desktop windows, CLI prompts, email templates, and more. The `context_type` property captures the medium; `type` captures the surface kind.
- **Action is a first-class type** — not just a relationship. Actions are independently queryable, can have their own test suites, can publish events, trigger jobs, and call endpoints.
- **Scenario layer is structural, not workflow** — Scenarios and ScenarioSteps describe observable behavior anchored to Contexts and Actions. They are part of the code's structure, not a sprint planning tool.
- **No workflow types** — this pack contains no `Task`, `Spec`, `Requirement`, `Change`, or `WorkPackage` types. For those, see the `product-memory-blueprint` or a SpecMCP-based pack.

---

## Directory layout

```
codebase/
  cmd/
    codebase/          # CLI source (Go)
  packs/
    code-structure.yaml    # object types and relationship types
  tools/
    graph-to-swagger       # reconstruct OpenAPI spec from graph
  README.md
  project.yaml
```

---

## Prerequisites

- An [Emergent](https://emergent.company) account with API key
- `memory` CLI installed
- No agents, no seed data, no external MCP servers required for the blueprint

---

## License

MIT
