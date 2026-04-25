# Resolver Architecture — Gap Audit
## Against the Mapper, Graph Executor, and Override Pattern Discussions

---

## What the Two Pasted Documents Introduced

The discussion introduced four concepts that are entirely absent from the current resolver architecture:

1. **MapperInterface** — a generated typed interface with one `MapXxxInput()` method per step plus a `MapResponse()` method. Developer implements it to express how data flows between steps.

2. **DefaultMapperImpl** — a generated default implementation of the mapper interface that infers field mappings by name-and-type matching. Developer embeds it and only overrides methods the default cannot handle.

3. **Graph Executor** — a runtime component (`stencil.Graph`) that replaces the flat sequential `touches:[]` model for complex APIs. Developer calls `BuildGraph()` to express step topology: parallel execution, conditional branches, wait conditions.

4. **Interface-everywhere with last-registration-wins override** — every generated concrete type (repository, cache, external, mapper) has a corresponding interface and a generated default implementation. Developer overrides by registering their own implementation in `hooks/register.go` after `wire.Register()` runs. Container resolves by interface — last write wins.

---

## Gap Analysis: What Is and Isn't in the Architecture

### Concept 1: MapperInterface

**Status: Completely missing.**

The current docs have no `MapperInterface`, no `ResolvedMapper`, no per-step typed input objects, no `MapResponse`, and no mapper path in the Level 2 interface mapping table.

What needs to be added:

**Level 1 (Objects):** Per-step typed input structs. Each step in an API gets a `StepInput` object:

```
APIAST step "validateUser" touching ExternalAST(UserServiceClient.GetUser)
  → StepInputObject{Name: "ValidateUserInput", Kind: StepInput, Path: "generated/handler/{api}/types.go"}
    Fields derived from the external method's input type
```

This belongs in the Level 1 AST → Object mapping table as a new row:

| Source AST | Kind | Object Name | Generated Path |
|---|---|---|---|
| `APIAST.Step` | `StepInput` | `{APIName}{StepID}Input` | `generated/handler/{api_name}/types.go` |
| `APIAST.Step` | `StepOutput` | `{APIName}{StepID}Output` | `generated/handler/{api_name}/types.go` |

**Level 2 (Interfaces):** MapperInterface needs a new `InterfaceKind`:

```
APIAST → MapperInterface
  Name: "{APIName}Mappers" e.g. "ProcessOrderMappers"
  Path: "generated/handler/{resource_name}/mappers.go"
  Functions: [
    MapValidateUserInput(ctx, shared *ProcessOrderContext) (*ValidateUserInput, error),
    MapCheckInventoryInput(ctx, shared *ProcessOrderContext) (*CheckInventoryInput, error),
    ...
    MapResponse(ctx, shared *ProcessOrderContext) (*ProcessOrderResponse, error),
  ]
```

New entry in Level 2 mapping table:

| Source AST | Kind | Interface Name | Generated Path |
|---|---|---|---|
| `APIAST` | `MapperInterface` | `{APIAST.Name}Mappers` | `generated/handler/{resource_name}/mappers.go` |

New `InterfaceKind` value: `MapperInterface`.

**Level 3 (Implementations):** Two new implementation kinds:

| Source AST | Kind | Impl Name | Generated Path |
|---|---|---|---|
| `APIAST` | `DefaultMapperImpl` | `Default{APIName}Mappers` | `generated/handler/{resource_name}/mappers_default.go` |

`DefaultMapperImpl` has no `Touches`. Instead it carries a `FieldMappings []ResolvedFieldMapping` — the Resolver's output of the field-matching algorithm:

```go
type ResolvedFieldMapping struct {
    MethodName  string              // "MapValidateUserInput"
    TargetField string              // "UserID" — field on StepInput
    SourcePath  string              // "shared.Request.UserID" — where it comes from
    Inferred    bool                // true = default can handle it
    MustOverride bool               // true = default emits panic/error body
    Reason      string              // why it can't be inferred (for generated comment)
}
```

The Resolver runs field-matching at resolution time. The Generator reads `FieldMappings` and renders the default implementation verbatim — either a real mapping or an error body with a clear message.

---

### Concept 2: DefaultMapperImpl Field-Matching Algorithm

**Status: Completely missing — no algorithm defined anywhere.**

This is a resolver-time computation. The algorithm needs to be defined:

```
For each StepInput field F:
  1. Find field with same Name and compatible Type on Request → map from Request
  2. Find field with same Name and compatible Type on any StepOutput already available
     (i.e. from a step that runs before this one in any execution order) → map from that output
  3. Apply name conventions: user_id → UserID, stripe_customer_id → StripeCustomerID → map
  4. Cannot infer → MustOverride = true, generate error body

For MapResponse:
  Same algorithm, source pool is all StepOutputs + Request
  Compute fields (Compute: true on ResponseDTO field) → always MustOverride
```

The Resolver needs a new sub-procedure: `resolveFieldMappings(api APIAST, steps []ResolvedStep) []ResolvedFieldMapping`. This runs during Level 3 when building `DefaultMapperImpl`.

The "steps that run before" part of rule 2 is where ordering matters. In the flat `touches:[]` model this is trivially the previous touches in list order. With the graph model it's any step that is an ancestor in the DAG.

---

### Concept 3: Graph Executor

**Status: Partially present but not fully modeled.**

The current architecture has `ResolvedTouch` with `Flag`, `Default`, `FatalError`, and `ResultField`/`ErrorField`. This covers the flat sequential model from the original tech spec.

What is missing is the DAG model for complex APIs. The two pasted docs describe a `stencil.Graph` runtime type that the developer uses to express topology. This sits in `stencil-go` (the runtime library), not in the resolver. However, the resolver needs to know which model an API uses, so generators can produce the right service body.

What needs to be added to the resolver:

**On `ResolvedMethod` (in ServiceImpl):** An `ExecutionModel` field:

```go
type ResolvedMethod struct {
    FunctionName  string
    Touches       []ResolvedTouch   // used when ExecutionModel == Sequential
    SharedContext *ResolvedObject

    // ExecutionModel tells the generator which service body pattern to render
    ExecutionModel ExecutionModel   // Sequential | GraphBased
    
    // MapperRef — points to the MapperInterface for this API
    // Used by both Sequential and GraphBased models
    MapperRef *ResolvedInterface   // the ProcessOrderMappers interface
}

type ExecutionModel int

const (
    Sequential  ExecutionModel = iota  // flat touches:[] model, renders flag/infra/hook pattern
    GraphBased                          // developer writes BuildGraph(), renders graph.Execute() body
)
```

When `ExecutionModel == Sequential`: generator renders the existing flag-check / infra-call / hook pattern from touches.
When `ExecutionModel == GraphBased`: generator renders a `graph.Execute()` call with a `StepFn` map — one entry per declared step.

How does the resolver decide which model? From the YAML. If the API uses `steps:` (the new keyword from the discussion), it's `GraphBased`. If it uses `touches:` (the existing keyword), it's `Sequential`. Both coexist.

**New `APIAST` field needed:** `Steps []StepAST` alongside the existing `Touches []TouchAST`. The validator ensures an API uses one or the other, not both.

---

### Concept 4: Interface-Everywhere + Last-Registration-Wins Override

**Status: Partially present, not complete.**

The current docs have `ExternalMockImpl` as a `Kind` variant, which shows awareness of the pattern. But the override model is not defined for repositories, caches, or mappers.

What needs to be added:

**In Level 2:** Every concrete generated type needs a corresponding interface that it satisfies. The current architecture already does this for `ExternalInterface`, `CacheInterface`, and `RepositoryInterface`. The gap is that the architecture doesn't explicitly state that `DefaultMapperImpl` satisfies `MapperInterface`, or that `DefaultUsersRepository` satisfies `UsersRepository` as an interface.

**In Level 3:** Two new `ImplementationKind` values for the cache mock:

```go
CacheMockImpl   // missing — currently only ExternalMockImpl exists
RepositoryMockImpl  // optionally — for test doubles
```

The current doc mentions "Also generates a mock implementation" for cache but doesn't define it as a proper `ImplementationKind`.

**In `ResolvedSpec`:** The DI resolution order needs to be explicit. The current architecture mentions `wire.go` registers all defaults, but doesn't model the override mechanism:

```go
type ResolvedDIRegistration struct {
    InterfaceType string   // "UsersRepository"
    DefaultImpl   string   // "DefaultUsersRepository" — registered first by wire.go
    // If developer provides an override, container uses last registration.
    // This is a runtime behavior of stencil-go/container, not a resolver concern.
    // The resolver just needs to confirm every interface has a default registered.
}
```

The resolver doesn't need to know about overrides — that's the container's job. But the resolver must verify that every `ResolvedInterface` has a corresponding `ResolvedImplementation` with `Kind == *Impl` (not mock). This is a completeness check: if a `CacheInterface` exists but no `CacheImpl` was built, that's a resolver bug.

---

## Summary: What Needs to Be Added

### New ObjectKinds (Level 1)
- `StepInput` — per-step typed input struct for GraphBased APIs
- `StepOutput` — per-step typed output struct for GraphBased APIs

### New InterfaceKind (Level 2)
- `MapperInterface` — one per APIAST, with `MapXxxInput()` per step + `MapResponse()`

### New ImplementationKinds (Level 3)
- `DefaultMapperImpl` — generated field-matching default, embeddable by developer
- `CacheMockImpl` — currently mentioned but not formally defined as a Kind

### New field on ResolvedMethod
- `ExecutionModel ExecutionModel` — `Sequential` or `GraphBased`
- `MapperRef *ResolvedInterface` — points to this API's mapper interface

### New sub-procedure in Resolver
- `resolveFieldMappings()` — runs during Level 3, produces `[]ResolvedFieldMapping` for `DefaultMapperImpl`

### New field on ResolvedImplementation (for DefaultMapperImpl)
- `FieldMappings []ResolvedFieldMapping` — replaces `Touches` for this kind

### New APIAST field (DSL change)
- `Steps []StepAST` — parallel to existing `Touches []TouchAST`; triggers `GraphBased` execution model

### Existing things that are correct and don't need changing
- `ExternalMockImpl` — already defined
- Sequential touch model with flags/results/errors — unchanged, still valid for simple APIs
- `HookInterface` derivation — unchanged
- All Level 1 objects for the sequential model — unchanged
- All Level 2 interfaces for repo/cache/external — unchanged
- `ResolvedTouch` for all non-service impl kinds — unchanged

---

## Updated Level 2 Mapping Table

| Source AST | Kind | Interface Name | Generated Path |
|---|---|---|---|
| `TableAST` | `RepositoryInterface` | `{pascal(table)}Repository` | `generated/repo/{table_name}/repo.go` |
| `APIAST` | `HookInterface` | `{APIAST.Name}Hooks` | `generated/handler/{resource_name}/hooks.go` |
| `APIAST` | **`MapperInterface`** ← NEW | `{APIAST.Name}Mappers` | `generated/handler/{resource_name}/mappers.go` |
| `ResourceGroupAST` | `ServiceInterface` | `{Resource.Group}Service` | `generated/handler/{resource_name}/service.go` |
| `CacheAST.Interface` | `CacheInterface` | `{Interface.Name}` | `generated/cache/{interface_name}/cache.go` |
| `ExternalAST` | `ExternalInterface` | `{External.Name}` | `generated/external/{external_name}/client.go` |

## Updated Level 3 Mapping Table

| Source AST | Kind | Impl Name | Generated Path |
|---|---|---|---|
| `TableAST` | `RepositoryImpl` | `{pascal(table)}RepositoryImpl` | `generated/repo/{table_name}/repo_impl.go` |
| `APIAST` | **`DefaultMapperImpl`** ← NEW | `Default{APIName}Mappers` | `generated/handler/{resource_name}/mappers_default.go` |
| `ResourceGroupAST` | `ServiceImpl` | `{Resource.Group}ServiceImpl` | `generated/handler/{resource_name}/{r}_impl.go` |
| `TransactionAST` | `TransactionImpl` | `{Tx.Name}Tx` | `generated/tx/{tx_name}/tx_impl.go` |
| `CacheAST.Interface` | `CacheImpl` | `{Interface.Name}Impl` | `generated/cache/{interface_name}/cache_impl.go` |
| `CacheAST.Interface` | **`CacheMockImpl`** ← NEW | `{Interface.Name}Mock` | `generated/cache/{interface_name}/cache_mock.go` |
| `ExternalAST` | `ExternalImpl` | `{External.Name}Impl` | `generated/external/{external_name}/client_impl.go` |
| `ExternalAST` | `ExternalMockImpl` | `{External.Name}Mock` | `generated/external/{external_name}/client_mock.go` |
