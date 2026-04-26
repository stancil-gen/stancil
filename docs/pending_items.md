# Pending Items & Technical Debt

This document tracks features, validations, and capabilities described in the product/tech specifications that have been deliberately deferred.

---

## Phase 2 — Scoped Out Subsystems

These subsystems have AST types defined (parser accepts them in YAML) but are skipped by the validator and resolver. Re-enabling requires uncommenting the deferred blocks in `checker.go` and `resolver.go`, plus writing generators and templates.

1. **Transactions** — `TransactionAST`, `buildTransactionParams`, `buildTransactionImpl` exist but are not wired into the resolver pipeline.
2. **Messaging** — `MessagingAST`, `resolveMessaging()` exist but are not called. Kafka producers/consumers not generated.
3. **Cache** — `CacheAST`, `buildCacheInterface`, `buildCacheImpl`, `buildCacheMock` exist but are not called.
4. **Auth** — `AuthAST`, `resolveAuth()` exist but are not called. JWT middleware, login/logout endpoints, role-based access control not generated. Validator `MISSING_AUTH_BLOCK` and `UNKNOWN_ROLE` checks are commented out.
5. **Storage** — `StorageAST` defined, no validator or resolver code.
6. **Observability** — `ObservabilityAST` defined, no validator or resolver code.
7. **Extensions / Overrides** — AST types defined, no pipeline code.

---

## Code Generation Bugs — Must Fix

These are defects in the current generators/templates that produce incorrect Go code.

### P0 — Broken output (won't compile)

1. **Double pointer bug in model generator** — Nullable fields render as `**time.Time` and `**string` instead of `*time.Time` and `*string`. The model generator prepends `*` when `GoPointer` is true, but the GoType already contains the `*` prefix. Fix: check `GoPointer` only, do not additionally prepend `*`, OR strip the existing `*` from GoType before prepending.
   - Files: `internal/generators/go/table/model_generator.go`, `templates/go/table/model.go.tmpl`

2. **Duplicate `ctx context.Context` params in repository interface** — Every repository function signature renders with `ctx context.Context` twice: `CreateOrder(ctx context.Context, ctx context.Context, data *Order)`. The generator is concatenating the params list from the resolver (which already includes ctx) with an additional ctx from the template.
   - Files: `internal/generators/go/table/repo_generator.go`, `templates/go/table/repo.go.tmpl`

3. **Duplicate error returns in repository interface** — Returns render as `(error, error)` or `(*Order, error, error)`. Same root cause as the params bug — double-concatenation.
   - Files: `internal/generators/go/table/repo_generator.go`, `templates/go/table/repo.go.tmpl`

### P1 — Incorrect but compiles

4. **Wire.go import paths use wrong module** — Wire imports reference `stencil/generated/...` (the tool's module) instead of `{project}/generated/...` (the generated project's module). The wire generator does not correctly derive the output module from `ctx.Spec.Module`.
   - File: `internal/generators/go/infra/wire_generator.go`

5. **External I/O types have `db` struct tags** — External request/response types (e.g., `ChargeRequest`) render with `db:"amount"` tags. These are HTTP API types, not database models — they should only have `json` tags, not `db` tags. The root cause is that `buildField()` in the resolver applies `db` tags to all fields regardless of ObjectKind.
   - Fix in: `internal/spec/resolver/level1_objects.go` (skip DB tags for ExternalInput/ExternalOutput kinds) or in the generator (strip db tags for external types)

6. **DTO types have `db` struct tags** — Same issue as above. Request/Response DTOs get `db:"..."` tags which are meaningless for API types.
   - Fix in: same as #5

### P2 — Cosmetic / quality

7. **Package naming produces `customer_ap_is`** — The `toSnakeCase("CustomerAPIs")` produces `customer_ap_is` instead of the expected `customer_apis`. The snake_case function splits on every uppercase-to-uppercase transition, breaking acronyms like "APIs" into "ap_is". Needs a special-case or different naming strategy.
   - Files: all `toSnakeCase` implementations across `template/funcmap.go`, `generators/go/api/helpers.go`, `generators/go/external/external_generator.go`, `plan/planner.go`

8. **Missing `Request` field on contexts without request DTO** — `ListOrdersContext` is missing its `Request` field because the API has no inline request DTO. The context generator should always add a Request field, even if the DTO is nil (use `interface{}` or omit gracefully).
   - File: `internal/generators/go/api/context_generator.go`

---

## Code Generation — Missing Functionality

These are features the generators must produce but currently do not.

### P0 — Critical for usable output

9. **No `go.mod` generation** — The generated project has no `go.mod` file. Without it, the generated code cannot be built. Need a `go.mod` generator that produces a valid module file with the project name from the spec and all required dependencies (gin, gorm, uuid, decimal, etc.).
   - New file: `internal/generators/go/infra/gomod_generator.go`
   - New template: `templates/go/infra/go.mod.tmpl`

10. **No `main.go` generation** — The generated project has no entry point. Need a scaffold generator that produces `main.go` with container setup, route registration, and server start. This should be a "generate once" file — not overwritten on regeneration.
    - New file: `internal/generators/go/infra/main_generator.go`

11. **Service infra calls are TODO stubs** — The service executor step functions contain `// TODO: wire mapper` comments instead of actual infra calls. Generated code must NEVER contain TODO comments. The generated code is frozen (`chmod 444`) and developers cannot edit it. The only way to change generated code is to edit the YAML spec and regenerate. Every infra call must be fully rendered:
    - Table create: `err := s.{repo}.Create{Model}(ctx, data)`
    - Table get: `result, err := s.{repo}.Get{Model}ByID(ctx, id)`
    - Table update: `err := s.{repo}.Update{Model}(ctx, id, data)`
    - Table delete: `err := s.{repo}.Delete{Model}(ctx, id)`
    - Table list: `results, err := s.{repo}.List{Model}s(ctx, params)`
    - External call: `result, err := s.{ext}.{Method}(ctx, req)`
    - Files: `internal/generators/go/api/service_generator.go`, `templates/go/api/service.go.tmpl`

12. **No TODO comments in generated code — ever** — This is a design principle, not just a bug. Generated files are read-only. Developers interact through hooks and the YAML spec. If a generator cannot produce a complete implementation for a step, it must produce a compilable no-op (e.g., `_ = shared` to suppress unused variable warnings) rather than a TODO comment. Audit all templates and remove every TODO.

13. **External client impl methods are empty stubs** — The external client methods return `nil, nil` with a TODO comment. They must render complete HTTP call bodies: build request, set headers, execute HTTP call, parse response, check status errors, return typed response. Or at minimum, produce a compilable placeholder that the mock can override.
    - File: `internal/generators/go/external/external_generator.go`, `templates/go/external/external.go.tmpl`

### P1 — Important for completeness

14. **No migration SQL generator** — Tables should produce `CREATE TABLE` migration SQL files. The resolver has all the field names, types, constraints, and indexes needed.
    - New generator: `go.sql.migration`

15. **No `config.go` generator** — The config block in the spec should generate a typed Config struct with environment variable loading and startup validation.
    - New generator: `go.config`

16. **No hooks scaffold generator** — `stencil hooks scaffold <api>` should generate empty hook files in `hooks/` that developers can fill in. Currently no hook scaffold generator exists.

17. **No `errors.go` at the API level** — API-specific domain errors (from external status errors, table errors referenced in steps) should be collected and re-exported or wrapped.

---

## Pending Validation Rules

1. **Config variable references** — Verify that every `${VAR}` used in external `base_url` fields actually exists in the global `config:` block. *(Partially implemented — works for externals, not checked for other blocks.)*

2. **State machine validation** — Verify that the state field is an enum type, all transitions reference valid enum values, and reachability from initial state. *(Implemented.)*

3. **Circular type dependencies** — Detect TypeA → TypeB → TypeA cycles. *(Implemented.)*

4. **Touch reference validation for scoped-out subsystems** — When transactions/messaging/cache are re-enabled, validate that step touches reference actually-defined infrastructure objects.

5. **DTO privacy guard** — Verify response DTOs don't expose fields marked `private: true` on the source table. *(Implemented.)*

6. **Auth role validation** — Verify roles on API endpoints exist in `auth.roles`. *(Deferred — auth block is scoped out.)*

---

## Technical Debt

1. **Language formatter interface** — Hardcoded `if/else` on `ast.Lang == "java"` for name formatting. Needs an injectable `NameFormatter` interface.

2. **Pointer optimization for custom types** — All custom types are raw values. Large nested structs in hot-path code should use pointers. The `MapType` function in `typemap.go` is the single place to change this.

3. **toSnakeCase is duplicated 4 times** — The snake_case function exists in `resolver/utils.go`, `template/funcmap.go`, `generators/go/api/helpers.go`, `generators/go/external/external_generator.go`, and `plan/planner.go`. Extract to a shared `internal/naming` package.

4. **Template FuncMap has no tests** — The `toPascalCase`, `toCamelCase`, `toSnakeCase` helpers in `funcmap.go` have no unit tests. These are critical for correctness — wrong casing produces non-compiling code.

5. **Generator tests are missing** — No golden file tests for any generator. Need: for each generator + fixture YAML, a golden `.go` file that the test compares against. The `-update` flag regenerates goldens.

6. **The `generated/` directory in the stencil repo** — Running `stencil generate` inside the stencil repo itself creates a `generated/` directory that interferes with `go build ./...` (the Go compiler tries to build the generated output as part of the stencil module). Need either a `.gitignore` rule or a separate output directory strategy.

7. **Emitter output directory is hardcoded** — `NewEmitter("generated", hash)` hardcodes the output directory. Should be configurable via CLI flag (`--output`).

8. **StencilLibImportPrefix constant is unused** — The `LibImportPath()` helper in `generator/constants.go` exists but generators build import paths manually. Wire them through the constant for single-point-of-change when the lib is published.
