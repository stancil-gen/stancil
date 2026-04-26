# Stencil — Architecture & Build Guide

**What we are building, how it works, and how to build it.**

---

## 1. What Is Stencil

Stencil is a deterministic backend code generation tool. You write one YAML file describing your backend service — tables, APIs, external calls, messaging, caching, auth — and Stencil generates 100% of the structural Go code. Developers only write business logic in hook functions. Nothing is AI-generated at runtime; all output is template-driven and reproducible.

The motivation is token reduction for AI-assisted development. AI writes a compact YAML spec. Stencil generates all structural code deterministically. AI then only implements small hook functions containing business logic. Token usage drops by an estimated 75-85% for typical backend development.

### The Core Idea in One Paragraph

A backend service has two concerns: *infrastructure plumbing* (connect to a database, call Stripe, publish to Kafka, cache in Redis) and *business logic* (hash the password, check if the account is a business account, decide whether a CRM failure is fatal). Stencil generates all the plumbing. Developers write all the logic. The separation point is a **shared context object** that flows through every step of an API execution, carrying request data, control flags, infra results, and the response. Developer hooks read and write this context. Generated code reads control flags and executes infra calls. Generated code never makes decisions.

---

## 2. The Two-Layer Architecture

### Layer 1 — Core Infrastructure

Core infra blocks declare everything the service talks to. They are independent of any API. The tool generates fully typed interfaces for each.

| Block | What it declares | What the tool generates |
|-------|-----------------|----------------------|
| `tables:` | DB tables — fields, queries, indexes, state machines, soft delete | Migration SQL, typed model struct, repository interface + default impl |
| `types:` | Custom value objects (Money, Address) | Typed struct + `Validate()` + `sql.Scanner`/`Valuer` |
| `transactions:` | Multi-step atomic DB operations | `BEGIN`/`COMMIT`/`ROLLBACK` orchestrator with typed params |
| `externals:` | Outbound HTTP — third-party APIs and inter-service calls | Typed client interface + default impl with retry + mock |
| `messaging:` | Kafka producers and consumers | Typed publish functions, consumer listener + DLQ |
| `cache:` | Redis typed cache interfaces | `Get`/`Set`/`Delete`/`Invalidate` with key templates |
| `auth:` | JWT config, roles, permissions | Auth middleware, login/logout/refresh endpoints |

### Layer 2 — API Resources

API resources declare what is exposed externally. A resource is **not** a table — it is an API group. A set of related endpoints around a domain concept. Each endpoint declares which core infra it touches, in what order. The tool generates the shared context, service executor, and hook interface for each API.

```yaml
resources:
  - group: UserAPIs
    base_path: /users
    auth: jwt
    apis:
      - name: CreateUser
        method: POST
        path: /
        auth: public
        steps:
          - id: writeUser
            touch: {table: users, op: create}
            flag: RunTableUsersCreate
            default: true
            fatal: true
          - id: publishEvent
            touch: {message: UserCreated}
            flag: RunMessageUserCreated
            default: true
            fatal: false
```

Key point: `ChargeCard` touches Stripe and Kafka but no DB table. `GetUser` touches cache first, then DB. `CreateUser` touches the DB, a CRM external, and Kafka. APIs are not constrained to one infra system.

---

## 3. The Execution Model

Every API execution follows the same pattern. This is the heart of Stencil.

### 3.1 Shared Context

One typed struct per API. Created at request entry. Flows through every generated step and every developer hook. Holds the final response.

```go
type CreateUserContext struct {
    // Request — the parsed, validated incoming request
    Request                CreateUserRequest

    // Control flags — one per step, defaults from spec
    RunTableUsersCreate    bool  // default: true
    RunMessageUserCreated  bool  // default: true

    // Step results — populated by generated code after each step runs
    WriteUserInput         *WriteUserInput
    WriteUserOutput        *WriteUserOutput
    WriteUserError         error

    PublishEventInput      *PublishEventInput
    PublishEventOutput     *PublishEventOutput

    // Response — developer sets in BeforeResponse hook
    Response               *UserResponse
}
```

### 3.2 Execution Flow

```
Request arrives
  → Validate request struct tags
  → Create shared context (flags set to spec defaults)
  → HOOK: BeforeCreateUser — developer inspects request, sets flags, mutates request
  → For each step in order:
      → if shared.Run{Flag} == true:
          → Mapper builds typed step input from shared context
          → Generated code calls infra (DB / HTTP / Kafka / Redis)
          → Result stored in shared.{Step}Output
          → HOOK: After{Step} — developer reads result, sets next flags
  → HOOK: BeforeResponse — developer builds shared.Response
  → If shared.Response is nil, fallback: auto-map from step output
  → Return shared.Response
```

### 3.3 Control Flags

This is the decision mechanism. Every step has a boolean flag in the shared context. `default: true` means the step runs unless a hook disables it. `default: false` means the step is skipped unless a hook enables it.

Example — cache read-through:
```go
// BeforeGetUser hook
if cached != nil {
    shared.RunTableUsersGet = false   // skip DB — cache hit
    shared.RunCacheWrite    = false   // no need to re-write cache
    shared.Response         = cached  // set response directly
}
```

Example — conditional CRM sync:
```go
// AfterTableUsersCreate hook
if shared.WriteUserOutput.AccountType == "business" {
    shared.RunCRMSync = true  // enable the CRM step (default was false)
}
```

### 3.4 Hook Interface

Generated per API. Every hook receives the full shared context. All hooks are optional — nil check before call.

```go
type CreateUserHooks struct {
    BeforeCreateUser       func(ctx context.Context, shared *CreateUserContext) error
    AfterTableUsersCreate  func(ctx context.Context, shared *CreateUserContext) error
    AfterMessageUserCreated func(ctx context.Context, shared *CreateUserContext) error
    BeforeResponse         func(ctx context.Context, shared *CreateUserContext) error
}
```

### 3.5 Mappers

Generated per API with a default implementation. The tool infers field mappings by name+type matching. Fields it cannot infer get `MUST_OVERRIDE` bodies. Developer embeds the default and overrides only what is needed.

```go
// Generated interface
type CreateUserMappers interface {
    MapWriteUserInput(ctx context.Context, shared *CreateUserContext) (*WriteUserInput, error)
    MapResponse(ctx context.Context, shared *CreateUserContext) (*UserResponse, error)
}

// Generated default — infers what it can
type DefaultCreateUserMappers struct{}
func (d *DefaultCreateUserMappers) MapWriteUserInput(ctx context.Context, shared *CreateUserContext) (*WriteUserInput, error) {
    return &WriteUserInput{
        FirstName: shared.Request.FirstName,  // inferred: same name + type
        Email:     shared.Request.Email,      // inferred: same name + type
    }, nil
}

// Developer overrides only what the default can't handle
type MyCreateUserMappers struct {
    DefaultCreateUserMappers  // embed — all inferred mappings for free
}
func (m *MyCreateUserMappers) MapWriteUserInput(ctx context.Context, shared *CreateUserContext) (*WriteUserInput, error) {
    input, _ := m.DefaultCreateUserMappers.MapWriteUserInput(ctx, shared)
    input.PasswordHash = hash(shared.Request.Password)  // can't be inferred
    return input, nil
}
```

### 3.6 DAG-Based Execution (Complex APIs)

For APIs where steps have parallel or conditional relationships, developers write a `BuildGraph()` function using the `stencil-go` graph API:

```go
func (h *ProcessOrderHooks) BuildGraph() *stencil.Graph {
    return stencil.NewGraph().
        Parallel("validateUser", "checkInventory").       // run concurrently
        Then("chargePayment").                             // sequential
        After("validateUser", "checkInventory").           // wait for both
        When(func(ctx context.Context, shared *ProcessOrderContext) bool {
            return shared.CheckInventoryOutput.Available   // conditional
        }).
        Then("createOrder").
        Then("notifyUser").
        Respond()
}
```

The flat `steps:` list declares *what* infra calls exist. The graph declares *when and in what order* they run.

---

## 4. Interface-Everywhere Override Pattern

Every generated concrete type has an interface. `wire.go` registers the default implementation. Developer registers an override in `hooks/register.go`. Container uses last-registration-wins.

```go
// generated/wire.go — registers defaults first
c.Provide(func(db *sql.DB) UsersRepository {
    return NewDefaultUsersRepository(db)
})

// hooks/register.go — developer override, registered after
c.Provide(func(db *sql.DB, m *Metrics) UsersRepository {
    return NewInstrumentedUsersRepository(db, m)
})
// Container resolves UsersRepository → InstrumentedUsersRepository
// All generated services that depend on UsersRepository get the override automatically
```

This works for every generated type: repositories, external clients, caches, mappers, producers, transactions. Developer embeds the default, overrides specific methods, registers in `hooks/register.go`. Zero changes to generated code.

---

## 5. File Ownership Model

| Directory | Owner | On regenerate |
|-----------|-------|---------------|
| `generated/` | Tool | Wiped and fully rewritten every time. `chmod 444`. |
| `hooks/` | Developer | Never read, never modified by tool. |
| `config/` | Developer | Generated once (scaffold), then developer-owned. |
| `main.go` | Developer | Generated once (scaffold), then developer-owned. |

### Generated File Structure

```
generated/
  config_types.go                    ← Config struct
  types/
    money.go                         ← Custom value objects
  tables/
    users/
      model.go                       ← User struct, enums, param structs
      queries.sql                    ← sqlc input SQL
      repository.go                  ← UsersRepository interface + default
      errors.go                      ← ErrUsersNotFound, ErrUsersEmailTaken
  transactions/
    placeorder.go                    ← PlaceOrderTx interface + default
  externals/
    stripe_client.go                 ← StripeClient interface + default + types
    stripe_client_mock.go            ← MockStripeClient for tests
  messaging/
    events.go                        ← Event payload structs
    producers.go                     ← Typed publish functions
    consumers.go                     ← Consumer listener + DLQ
  cache/
    user_cache.go                    ← UserCache interface + default Redis impl
  auth/
    middleware.go                    ← JWT middleware
    handler.go                       ← Login/logout/refresh endpoints
  apis/
    createuser/
      dto.go                         ← Request + response structs
      context.go                     ← Shared context struct + constructor
      steps.go                       ← Typed step input/output structs
      mappers.go                     ← Mapper interface + default impl
      graph.go                       ← Graph builder interface
      service.go                     ← Executor (reads flags, calls infra)
      handler.go                     ← HTTP handler (bind, call service, respond)
  migration/
    001_create_users.sql
  routes.go                          ← All route registration
  wire.go                            ← DI container provider registrations
```

---

## 6. Internal Architecture of the Stencil Tool

Stencil is a Go CLI. Single static binary, no runtime dependencies. Five-stage pipeline:

```
stencil.yaml  →  Parse  →  Validate  →  Resolve  →  Plan  →  Generate  →  Files on disk
                  │          │            │           │          │
                  SpecAST    []Error      ResolvedSpec  DAG      []File
```

### 6.1 Parser

Reads YAML bytes. Produces `SpecAST` — a direct mirror of the YAML structure. No defaults filled, no inference. Parser never validates.

### 6.2 Validator

All semantic checks in one pass. Never stops at first error. Returns `[]ValidationError` with line numbers and machine-readable codes.

Checks: required fields present, field types resolve, enum fields have values, FK references exist, touch references resolve (table/external/cache/message/transaction names exist), no duplicate flag names per API, auth roles exist, `owner: true` only on APIs touching tables with `user_id`.

### 6.3 Resolver — The Core Transform

Takes `SpecAST`, produces `ResolvedSpec`. Fills all defaults, derives all implicit values, plans all shared context structs and hook interfaces. This is the most important component.

**Three levels + import resolution:**

```
Level 1:   Build all ResolvedObjects (types, table models, DTOs, shared contexts)
Level 1A:  ImportResolver pass → attaches Imports map to each object

Level 2:   Build all ResolvedInterfaces (repos, hooks, services, caches, externals)
Level 2A:  ImportResolver pass → attaches Imports map to each interface

Level 3:   Build all ResolvedImplementations + DI graph
Level 3A:  ImportResolver pass → attaches Imports map to each implementation
```

**Level 1 — Objects:**

| Source AST | Kind | Object Name | Generated Path |
|---|---|---|---|
| `TypeAST` | TypeObject | `{TypeAST.Name}` | `generated/types/` |
| `TableAST` | TableModel | `{pascal(TableAST.Name)}` | `generated/tables/{t}/model.go` |
| `APIAST.Request` | RequestDTO | `{API.Name}Request` | `generated/apis/{api}/dto.go` |
| `APIAST.Response` | ResponseDTO | `{API.Name}Response` | `generated/apis/{api}/dto.go` |
| `APIAST` | SharedContext | `{API.Name}Context` | `generated/apis/{api}/context.go` |
| `APIAST.Step` | StepInput | `{API.Name}{StepID}Input` | `generated/apis/{api}/steps.go` |
| `APIAST.Step` | StepOutput | `{API.Name}{StepID}Output` | `generated/apis/{api}/steps.go` |

**Level 2 — Interfaces:**

| Source AST | Kind | Interface Name |
|---|---|---|
| `TableAST` | RepositoryInterface | `{Table}Repository` |
| `APIAST` | HookInterface | `{API.Name}Hooks` |
| `APIAST` | MapperInterface | `{API.Name}Mappers` |
| `ResourceGroupAST` | ServiceInterface | `{Group}Service` |
| `CacheAST` | CacheInterface | `{Interface.Name}` |
| `ExternalAST` | ExternalInterface | `{External.Name}` |

**Level 3 — Implementations:**

| Source AST | Kind | Impl Name |
|---|---|---|
| `TableAST` | RepositoryImpl | `Default{Table}Repository` |
| `APIAST` | DefaultMapperImpl | `Default{API}Mappers` |
| `ResourceGroupAST` | ServiceImpl | `{Group}ServiceImpl` |
| `TransactionAST` | TransactionImpl | `{Tx.Name}Tx` |
| `CacheAST` | CacheImpl | `Default{Interface}` |
| `ExternalAST` | ExternalImpl | `Default{External}` |
| `ExternalAST` | ExternalMockImpl | `Mock{External}` |

**Import Resolver:** Separate component. Runs after each level. Maps `TypeKind` → import path for the active language. When Go targets, `TypeDecimal` → `"github.com/shopspring/decimal"`. When Java targets, `TypeDecimal` → `"java.math.BigDecimal"`. Adding a new language means adding a `case` block here — nothing else changes.

**TypeDescriptor:** Language-agnostic canonical type representation. Every resolved field carries one.

```go
type TypeDescriptor struct {
    Kind     TypeKind   // TypeStr | TypeInt | TypeDecimal | TypeUUID | TypeEnum | TypeCustom
    GoType   string     // "string", "decimal.Decimal", "uuid.UUID", "*Money"
    JavaType string     // "String", "BigDecimal", "UUID", "Money"
    DBType   string     // "VARCHAR(255)", "NUMERIC", "UUID", "JSONB"
    IsCustom bool
    IsEnum   bool
}
```

### 6.4 Diff Planner and DAG

Compares `ResolvedSpec` against `.stencil.lock` (a snapshot of the previous spec). Produces a `GenerationPlan` — a set of `Task`s organized into parallel execution tiers.

Builds a DAG from generator dependencies and runs **Kahn's algorithm** for topological sort:

```
Tier 1 (no deps):      sql.migration, go.types, go.errors, go.config
Tier 2 (types):         go.table.model, go.api.dto, go.cache
Tier 3 (model+dto):     go.table.repo, go.api.mapper, go.api.context, go.api.hooks
Tier 4 (repo+mapper):   go.api.service, go.external, go.messaging, go.tx, go.auth
Tier 5 (service):       go.api.handler, go.routes
Tier 6 (everything):    go.wire, go.mod
```

Tasks within a tier run in parallel. Tiers run sequentially. Cycle detection is built in.

### 6.5 Generator System

Every generator implements a single interface:

```go
type Generator interface {
    ID() string
    Generate(ctx GeneratorContext) ([]File, error)
}
```

Generators are pure functions. No filesystem I/O, no global state. They receive the `ResolvedSpec` (or a subset of it) and return `[]File`. The emitter writes to disk.

Each generator:
1. Calls an import collector to build the exact `ImportSet`
2. Builds a template data struct from the resolved spec
3. Renders a Go `text/template` with a custom `FuncMap`
4. Returns the rendered bytes as `File` objects

**Template FuncMap** provides: `toPascalCase`, `toCamelCase`, `toSnakeCase`, `toPlural`, `toPackageName`, `typeMap`, `dbType`, `zeroValue`, `jsonTag`, `validateTag`, `renderImports`, and more.

### 6.6 Emitter

Staged writes with rollback. Files are staged in memory during generation. `Flush()` writes to disk atomically. If any write fails, rollback restores from backup. `generated/` is never left in a partial state. All generated files get `chmod 444`.

### 6.7 CLI Commands

| Command | Behaviour |
|---------|-----------|
| `stencil generate` | First run — generate all files from spec |
| `stencil update` | Diff spec vs lock, regenerate changed files only |
| `stencil diff` | Print what would change without writing anything |
| `stencil validate` | Parse and validate spec, report all errors |
| `stencil hooks scaffold <api>` | Generate empty hooks file for one API |

---

## 7. The stencil-go Runtime Library

Published Go module that generated code imports. Not copied — it is a dependency.

```
github.com/stencil-run/stencil-go/
  container/     DI container (Provide, MustResolve, OnStart, OnStop, Start, Stop)
  errors/        ValidationError, DomainError, NewNotFound, NewForbidden, NewConflict
  handler/       Bind[T](c), WriteCreated, WriteOK, WriteNoContent
  pagination/    CursorPage[T], OffsetPage[T], NewCursorPage, ParseCursorParams
  middleware/    RequestID, StructuredLogger, Recovery, CORS, RateLimit
  cache/         BaseCache (embedded by generated cache impls)
  tx/            RunTx(ctx, db, fn) — BEGIN/COMMIT/ROLLBACK wrapper
  messaging/     BaseProducer, BaseConsumer, Message
  graph/         Graph, Parallel, Then, After, When, Respond (DAG execution)
```

Currently lives in `lib/` in the monorepo with a `replace` directive in `go.mod`.

---

## 8. How To Build This — Implementation Order

### Phase 1: Foundation (Parser → Validator → Resolver)

This is the data pipeline. Everything downstream depends on it.

**Step 1: SpecAST types (`internal/spec/ast.go`)**
Define every AST struct to mirror the YAML schema exactly. No inference, no defaults. This is already partially implemented.

**Step 2: Parser (`internal/spec/parser/parser.go`)**
YAML bytes → `SpecAST`. Use `gopkg.in/yaml.v3`. Two sub-steps: unmarshal into generic map, then map to typed AST. Parser never validates. Already partially implemented.

**Step 3: Validator (`internal/spec/validator/`)**
Single-pass semantic validation. Never stops at first error. Returns `[]ValidationError` with line numbers. Check: required fields, type resolution, enum values, FK references, touch reference resolution, flag uniqueness, auth role existence. Already partially implemented.

**Step 4: ResolvedSpec types (`internal/spec/resolved.go`)**
Define `ResolvedObject`, `ResolvedInterface`, `ResolvedImplementation`, `TypeDescriptor`, `ResolvedField`, `ResolvedFunction`, `ResolvedTouch`, and all supporting types. Already partially implemented — needs the gap audit items added (MapperInterface, StepInput/StepOutput ObjectKinds, DefaultMapperImpl, field mapping algorithm).

**Step 5: Resolver (`internal/spec/resolver/`)**
The most important component. Three levels + import resolution. Must correctly:
- Build `TypeDescriptor` for every field (typemap.go)
- Derive repository functions from query declarations
- Plan shared context structs from step declarations
- Derive hook interfaces from steps
- Derive mapper interfaces and run field-matching algorithm for defaults
- Build DI dependency graph
- Validate import hierarchy (no cycles)

**Step 6: Import Resolver (`internal/spec/resolver/import_resolver.go`)**
Runs after each level. Maps `TypeKind` → import path per language.

### Phase 2: Planning (DAG + Diff)

**Step 7: DAG (`internal/plan/dag.go`)**
Kahn's algorithm for topological sort. Cycle detection. Already implemented.

**Step 8: Planner (`internal/plan/planner.go`)**
Build tasks from `ResolvedSpec`. Wire generator dependencies. Produce `GenerationPlan` with parallel tiers. Already implemented.

**Step 9: Diff/Lock (`internal/diff/lock.go`)**
Spec hashing for `stencil update`. Compare resolved spec vs `.stencil.lock` to find changed entities. Produce `[]SpecChange` for incremental regeneration.

### Phase 3: Generators (Template-Driven Code Generation)

Build generators in dependency order (matching the DAG tiers). Each generator follows the same pattern: import collector → template data struct → template render → `[]File`.

**Tier 1 generators:**
- `go.types` — custom value object structs (Money, Address)
- `go.config` — typed Config struct from `config:` block
- `sql.migration` — `CREATE TABLE` SQL from table definitions
- `go.errors` — domain error vars per table

**Tier 2 generators:**
- `go.table.model` — DB model struct, enums, param structs per table
- `go.api.dto` — request + response DTO structs per API
- `go.cache` — typed cache interface + Redis default impl

**Tier 3 generators:**
- `go.table.repo` — repository interface + default impl (sqlc/gorm/sqlx/raw variants)
- `go.api.context` — shared context struct per API
- `go.api.hooks` — hook interface per API
- `go.api.mapper` — mapper interface + default impl per API
- `go.api.steps` — typed step input/output structs per API

**Tier 4 generators:**
- `go.api.service` — **the most important generator**. Reads steps, renders flag checks, infra calls, hook callsites. Template contains zero conditional logic.
- `go.external` — typed HTTP client interface + default + mock
- `go.messaging` — producer publish functions + consumer listeners
- `go.tx` — transaction orchestrator

**Tier 5 generators:**
- `go.api.handler` — HTTP handler (bind, validate, call service, respond)
- `go.auth` — JWT middleware + auth endpoints
- `go.routes` — all route registration + middleware application

**Tier 6 generators:**
- `go.wire` — DI container provider registrations
- `go.mod` — `go.mod` management

### Phase 4: Orchestration and Emission

**Step 10: Template Engine (`internal/template/engine.go`)**
Go `text/template` with custom FuncMap. Loads templates from `templates/` (embedded via `embed.go`). Already partially implemented.

**Step 11: Generator Registry + Orchestrator (`internal/generator/`)**
Registry holds all generators by ID. Orchestrator runs them in DAG tier order with `sync.WaitGroup` for parallelism. Already implemented.

**Step 12: Emitter (`internal/emitter/emitter.go`)**
Staged file writes with rollback. `chmod 444` on all generated files. Lock file update. Already implemented.

**Step 13: CLI (`cmd/stencil/main.go`)**
Cobra commands: `generate`, `update`, `diff`, `validate`, `hooks scaffold`. Already partially implemented.

### Phase 5: Runtime Library

**Step 14: DI Container (`lib/container/`)**
Reflect-based. `Provide()` registers constructors. `MustResolve()` wires. Last-registration-wins for interface overrides. `OnStart`/`OnStop` for lifecycle. Already implemented.

**Step 15: Graph Executor (`lib/graph/`)**
`Parallel`, `Then`, `After`, `When`, `Respond`. Runtime manages goroutines, WaitGroups, error propagation, non-fatal handling. Already partially implemented.

**Step 16: Handler Utilities (`lib/handler/`)**
`Bind[T]` for request parsing + validation. `WriteCreated`, `WriteOK`, `WriteNoContent` for responses. Already partially implemented.

**Step 17: Error Types (`lib/errors/`)**
`ValidationError`, `DomainError`, `MapValidationError`, `NewNotFound`, `NewForbidden`, `NewConflict`. Already partially implemented.

---

## 9. Template Data Flow

Each template receives a specific data struct. The generator builds this struct from the `ResolvedSpec`. The template mechanically renders it. Templates never compute — all logic is in the generator.

```
ResolvedSpec
  │
  ├─ go.types generator
  │    reads: spec.ObjectsOfKind(TypeObject)
  │    builds: TypeTemplateData{Name, Fields, Imports}
  │    renders: templates/go/types/type.go.tmpl
  │    outputs: generated/types/money.go
  │
  ├─ go.table.model generator
  │    reads: spec.ObjectsOfKind(TableModel)
  │    builds: ModelTemplateData{ModelName, Fields, Enums, StateTransitions, ...}
  │    renders: templates/go/table/model.go.tmpl
  │    outputs: generated/tables/users/model.go
  │
  ├─ go.api.service generator (THE CRITICAL ONE)
  │    reads: spec.ImplOfKind(ServiceImpl)
  │    builds: ServiceTemplateData{Steps[], Dependencies[], HooksType, ...}
  │    renders: templates/go/api/service.go.tmpl
  │    outputs: generated/apis/createuser/service.go
  │
  └─ ... (one generator per file type)
```

### Service Template — The Heart of the System

The service template is the most important. It reads flags and calls infra. It never decides. All branching is in the template data.

```go
// For each step in ServiceTemplateData.Steps:
if shared.{{ .FlagName }} {
    input, err := s.mappers.Map{{ .ID | toPascalCase }}Input(ctx, shared)
    // ... store input on context ...
    result, err := {{ .InfraCall }}
    // ... store result on context ...
    if {{ .Fatal }} && err != nil {
        return nil, err
    }
    // ... call AfterHook if present ...
}
```

The template contains zero `if/else` of its own. `Steps` is pre-ordered. `Fatal` is pre-determined. `InfraCall` is pre-rendered. The template is a mechanical substitution layer only.

---

## 10. Import Hierarchy

Strict layering. Violations = build error. Validated by the resolver.

```
Level 0  generated/types/              → stdlib only
Level 1  generated/tables/*/model.go   → types/ + stdlib
         generated/apis/*/dto.go
Level 2  generated/tables/*/repo.go    → model + stencil-go + DB libs
         generated/externals/          → types/ + HTTP libs
         generated/cache/              → types/ + Redis libs
Level 3  generated/apis/*/context.go   → dto + types
         generated/apis/*/steps.go
Level 4  generated/apis/*/mappers.go   → context + steps + dto
Level 5  generated/apis/*/service.go   → repo + externals + messaging + cache + tx + mappers + hooks
Level 6  generated/apis/*/handler.go   → service + dto + stencil-go/handler
Level 7  generated/routes.go          → ALL handlers + auth
Level 8  generated/wire.go + main.go  → everything + stencil-go/container
```

`hooks/` imports Level 2-4 interfaces ONLY — never imports generated services.

---

## 11. Testing Strategy

Five layers, from fastest to most comprehensive:

**Layer 1 — Unit tests** (runs every commit)
Table-driven tests for Parser, Validator, Resolver, DAG. No filesystem. The resolver is the most critical — verify that shared contexts and hook interfaces are correctly built from step declarations.

**Layer 2 — Golden file tests** (runs every commit)
One test per generator, multiple scenarios. The `-update` flag regenerates golden files. The git diff is the code review.

Key scenarios:
| Scenario | What it tests |
|----------|--------------|
| `createuser_basic` | Table step only. Flag default true. |
| `createuser_conditional` | Table + external with `default: false`. |
| `getuser_cache_readthrough` | Cache read → table → cache write. Short-circuit on cache hit. |
| `chargecard_no_table` | External only. No DB involvement. |
| `placeorder_multi_step` | Transaction + external + message. |

**Layer 3 — Compile test** (runs on PR merge)
Generate from test spec, run `go build ./...` on output. If it doesn't compile, the generator has a bug.

**Layer 4 — Runtime test** (runs nightly)
Generate orders service, run against real Postgres, hit HTTP endpoints. Verify: private fields excluded from response, cache hit skips DB, conditional external not called for personal accounts, state machine rejects invalid transitions, non-fatal errors don't abort.

**Layer 5 — Update regression** (runs on PR merge)
`stencil update` from spec v1 to spec v2. Verify exactly which files changed and which didn't. Adding a touch should change `context.go`, `hooks.go`, `service.go`, `wire.go` but NOT `dto.go` or `model.go`.

---

## 12. Current State and What's Left

### Already implemented (partial):
- SpecAST types and parser
- Validator (basic checks)
- Resolver (3-level pipeline, typemap, import resolver)
- DAG with Kahn's algorithm
- Generator interface, registry, orchestrator
- Emitter with staged writes
- Template engine with FuncMap
- Several generators (context, dto, handler, hooks, service, wire, model, repo)
- Templates for the above generators
- DI container (lib/container)
- Graph executor (lib/graph)
- CLI with parse/validate/resolve/plan/generate commands
- Test fixtures (minimal, complex, resolver suite, error suite, auth)

### Gaps from the architecture audit:
- **MapperInterface** — not yet in Level 2 interface mapping
- **DefaultMapperImpl** — not yet in Level 3, no field-matching algorithm
- **StepInput/StepOutput** ObjectKinds — not yet in Level 1
- **ExecutionModel** (Sequential vs GraphBased) on ResolvedMethod — not yet modeled
- **CacheMockImpl** — mentioned but not formally defined as a Kind
- **Field mapping algorithm** for default mapper inference — not defined

### Missing generators:
- `go.api.steps` — step input/output struct generator
- `go.api.mapper` — mapper interface + default impl generator
- `go.api.graph` — graph builder interface generator
- `go.external` — external client generator (partially exists)
- `go.messaging` — producer/consumer generator
- `go.tx` — transaction orchestrator generator
- `go.auth` — auth middleware + handler generator
- `go.config` — config struct generator
- `sql.migration` — SQL migration generator

### Missing runtime library pieces:
- `lib/pagination/` — cursor + offset pagination
- `lib/middleware/` — recovery, CORS, rate limit, request ID
- `lib/cache/` — BaseCache for embedding
- `lib/tx/` — RunTx wrapper
- `lib/messaging/` — BaseProducer, BaseConsumer
- `lib/observability/` — metrics, tracing

---

## 13. Key Design Decisions

| Decision | What we chose | Why |
|----------|-------------|-----|
| Resources are not tables | Resources = API groups. Tables = infra. | Real APIs touch multiple infra systems. 1:1 table-API constraint doesn't reflect reality. |
| Shared context per API | One typed struct flows end-to-end | Makes all state explicit. Eliminates hidden dependencies between hooks. |
| Control flags | Boolean fields control step execution | Clean separation: biz logic sets flags, generated code reads flags. No conditional logic in templates. |
| Mapper with defaults | Name+type field matching, embed+override | Most mappings are mechanical. Developer overrides only exceptions. |
| DAG execution | Steps declare existence, graph declares topology | Parallel steps, conditional branches, wait conditions — real APIs need this. |
| Interface everywhere | Every type has an interface, last-registration-wins | Type-safe overrides with zero changes to generated code. |
| Service template has zero logic | All branching pre-computed in template data | Templates stay testable and maintainable. Generator bugs, not template bugs. |
| Staged writes with rollback | Files staged in memory, atomic flush | `generated/` is never left in a partial state. |
| Non-fatal errors | Error stored in context, hook decides severity | CRM failure should not abort user creation. Developer decides. |
| Import hierarchy | Strict layering, validated by resolver | Prevents circular imports between generated packages. |

---

## 14. Repository Structure

```
stencil/
  cmd/stencil/              ← CLI entrypoint (cobra commands)
  internal/
    spec/                   ← SpecAST, ResolvedSpec, parser, validator, resolver
    plan/                   ← GenerationPlan, Task, DAG, topological sort
    generator/              ← Generator interface, registry, orchestrator
    generators/
      go/                   ← Go-specific generators (table, api, infra)
    imports/                ← ImportSet, ImportCollector, hierarchy validator
    template/               ← Engine, FuncMap, postprocess
    emitter/                ← File writer, chmod, lock file
    diff/                   ← Lock schema, spec comparison
  templates/
    go/                     ← .go.tmpl files per generator
  lib/                      ← stencil-go runtime library (DI, graph, errors, handler)
  testdata/                 ← YAML fixtures for testing
  docs/                     ← Specifications and design documents
  go.mod
```
