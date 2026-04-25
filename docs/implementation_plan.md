# Stencil Incremental Implementation Plan

This plan breaks down the development of the Stencil CLI into small, reviewable, and immediately testable milestones. We will avoid large "big bang" PRs and instead build the compiler pipeline piece-by-piece, writing unit tests for each component. 

## Proposed Changes

---
### Phase 1: CLI Skeleton & Foundation
**Goal**: Initialize the Go workspace and create an executable command line app that can accept subcommands.

#### [NEW] `go.mod`
- Initialize `stencil` as the root Go module.

#### [NEW] `testdata/minimal.yaml`
- A minimal valid Stencil specification file for testing the pipeline end-to-end.

#### [NEW] `cmd/stencil/main.go`
- Sets up Cobra/CLI entry points (`stencil build`).
- For now, it will simply print "Hello from Stencil!"

---
### Phase 2: AST Data Structures
**Goal**: Define the exact memory representation of the DSL before we write any parsing logic.

#### [NEW] `internal/spec/ast.go`
- Translates the Product Spec DSL into standard Go structs (`SpecAST`, `ResourceAST`, `FieldAST`, `EndpointAST`). No logic, just typed data containers.

---
### Phase 3: YAML Parser
**Goal**: Read a YAML file off disk and map it safely into our AST.

#### [NEW] `internal/spec/parser.go`
- Loads `[]byte` via `gopkg.in/yaml.v3`. Parses data into a generic `map[string]interface{}` and maps it explicitly to `SpecAST` to prevent unknown field errors.

#### [NEW] `internal/spec/parser_test.go`
- Unit tests that explicitly load `testdata/minimal.yaml` and verify the `SpecAST` population works correctly without crashing.

---
### Phase 4: Validations
**Goal**: Catch user errors in the DSL early by doing batch semantic checks.

#### [NEW] `internal/spec/validator.go`
- Implements rules like "Check all required fields are present", "Check relation targets actually exist", "Ensure enums are mapped correctly". 
- Returns an array of formatted `ValidationError` structs.

#### [NEW] `internal/spec/validator_test.go`
- Unit tests with gracefully broken mocked YAMLs testing if the validator catches the expected errors.

---
### Phase 5: The Resolver
**Goal**: Expand the implicit features of the `SpecAST` into a finalized `ResolvedSpec` so generators don't have to guess defaults.

#### [NEW] `internal/spec/resolved.go`
- Definitions for `ResolvedSpec`, `ResolvedResource`, `ResolvedField` mirroring `ast.go` but vastly richer.

#### [NEW] `internal/spec/resolver.go`
- Injects missing defaults (e.g., if a table name is missing, infer snake_case from resource name). Converts "DSL string types" (e.g. `str`) to "Go Types" (`string`). Maps custom user `types:`.

#### [NEW] `internal/spec/resolver_test.go`
- Testing the resolution algorithm.

---
### Phase 6: DAG Planner
**Goal**: Determine correct concurrent execution order for generators.

#### [NEW] `internal/plan/plan.go` & `internal/plan/dag.go`
- Implementation of Kahn's Algorithm to sort generator dependencies into concurrent Tiers.
- Tests to ensure cycle detection works and that foundational files (like types) generate earlier than dependants (like logic services).

---
### Phase 7: Orchestrator & Emitter
**Goal**: Infrastructure to run generators and write files safely.

#### [NEW] `internal/generator/generator.go` & `internal/generator/registry.go`
- The `Generator` interface and central registry.

#### [NEW] `internal/generator/orchestrator.go` & `internal/emitter/emitter.go`
- The engine that executes Tiers in parallel, capturing generated context in memory, and running a single atomic `Flush()` to the disk formatting with `goimports`.

---
### Phase 8: First Code Generator
**Goal**: Prove the pipeline works by implementing the very first target generator (`go.model`).

#### [NEW] `templates/go/model.go.tmpl`
- The `text/template` HTML-style file for writing Go domain model structs.

#### [NEW] `internal/generators/go/model.go`
- Implements `Generator` interface, resolving fields to template outputs.

## Decisions Made
1. The CLI will be named `stencil` (`stencil build`).
2. The Go module will be `stencil`.
3. Code formatting will use standard Go imports inside the emitter phase implicitly.

## Verification Plan

### Automated Tests
1. `go test ./...` will be strictly enforced at each boundary. 
2. Specifically, parsing and resolving will have deeply mocked `_test.go` files, as this is the core engine's brain.

### Manual Verification
1. I will write a sample DSL using standard structures (like a `User` and `Order` system) in `testdata`.
2. As we implement Phase 8, we will manually run the CLI (`go run ./cmd/stencil`) and inspect the generated Go source files to verify visual and syntax correctness.
