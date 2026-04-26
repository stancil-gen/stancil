# Stencil Platform Refactoring Plan

## Problem Statement

The resolver, which should be language-agnostic, is deeply contaminated with Go-specific concerns: 40+ hardcoded `.go` file paths, 60+ Go type renderings (`GoType`, `GoPointer`, `GoStructTag`), Go naming conventions (`GetXxxByYyy`, `*Type`, `[]*Type`), and Go import paths. This makes multi-language support impossible and creates bugs when language concerns leak across boundaries (e.g., `db:"amount"` tags on API types).

The generators are also not platformized — 12 individual generator files with custom data structs, duplicated `toSnakeCase` in 5 places, and no shared infrastructure.

---

## Goals

1. Resolver outputs pure domain descriptions — no Go types, no file paths, no struct tags
2. Adding a new language (Java, Python) requires only: templates + a LanguageAdapter implementation
3. Adding a new generator requires only: a manifest entry + a template file
4. One place to fix naming, one place to fix types, one place to fix paths
5. Zero TODO comments in generated output — ever

---

## Three Approaches

### Approach A: Language Adapter Interface (Recommended)

**Core idea:** Extract all language-specific logic into a `LanguageAdapter` interface. The resolver outputs pure domain. The generator layer uses the adapter to render language-specific code.

**Architecture:**
```
YAML → Parser → Validator → Resolver (domain-only) → ResolvedSpec
                                                          ↓
                                           LanguageAdapter (Go/Java/Python)
                                                          ↓
                                           Generator (generic, manifest-driven)
                                                          ↓
                                           Template → Emitter → Files
```

**The adapter interface:**
```go
type LanguageAdapter interface {
    // File system
    FileExtension() string                              // ".go", ".java"
    ModelFilePath(tableName string) string               // "tables/users/model.go"
    RepoFilePath(tableName string) string                // "tables/users/repository.go"
    APIFilePath(group, file string) string               // "apis/user_apis/service.go"
    ExternalFilePath(name string) string                 // "externals/stripe_client.go"
    InfraFilePath(file string) string                    // "wire.go"
    
    // Naming
    ModelName(tableName string) string                   // "users" → "User"
    FieldName(yamlName string) string                    // "first_name" → "FirstName" (Go) / "firstName" (Java)
    PackageName(name string) string                      // "UserAPIs" → "user_apis" (Go) / "userapIs" (Java)
    RepoFuncName(query ResolvedQuery, model string) string  // FindBy[email] → "GetUsersByEmail"
    ConstructorName(typeName string) string              // "UserRepo" → "NewUserRepo" (Go) / just class name (Java)
    
    // Types
    RenderType(kind TypeKind, customName string) string  // Decimal → "decimal.Decimal"
    RenderNullableType(kind TypeKind, name string) string // Decimal, nullable → "*decimal.Decimal"
    RenderSliceType(kind TypeKind, name string) string   // Custom "User" → "[]*User"
    ErrorTypeName() string                               // "error" (Go) / "Exception" (Java)
    ContextTypeName() string                             // "context.Context" (Go) / "" (Java)
    
    // Struct metadata
    RenderFieldTag(f ResolvedField, context TagContext) string  // json tag + validate tag (no db tag for DTOs)
    RenderAnnotation(f ResolvedField) string              // Java: @NotNull, Go: "" (uses tags)
    
    // Imports
    ImportsForType(kind TypeKind) []string                // UUID → ["github.com/google/uuid"]
    ImportsForInfra(infra string) []string                // "db" → ["gorm.io/gorm"]
    RuntimeImportPath(module, pkg string) string          // "orders/generated/lib/container"
    
    // Formatting
    FormatFile(path string, content []byte) ([]byte, error)  // goimports for Go, google-java-format for Java
}
```

**TagContext** tells the adapter what kind of struct the field belongs to:
```go
type TagContext int
const (
    TagContextModel    TagContext = iota  // table model → json + db + validate tags
    TagContextDTO                         // request/response DTO → json + validate tags only
    TagContextExternal                    // external I/O type → json tags only
    TagContextType                        // custom value type → json tags only
)
```

This solves the `db` tag leaking into DTOs problem — the adapter knows not to add `db` tags when context is `TagContextDTO`.

**Pros:**
- Clean separation — resolver is pure domain, adapter is pure language
- Type-safe — compile-time errors if adapter method missing
- Testable — mock the adapter to test generators without Go specifics
- Adding Java = implement the interface + write templates

**Cons:**
- Large interface (15+ methods) — could become unwieldy
- One-time cost to extract all logic from resolver into adapter
- Templates still need to call adapter methods via FuncMap (slight indirection)

---

### Approach B: Render Pipeline with Middleware

**Core idea:** Instead of a single adapter interface, build a pipeline of render stages. Each stage transforms the domain model toward the target language.

**Architecture:**
```
ResolvedSpec → Stage 1: Name Resolution → Stage 2: Type Resolution → Stage 3: Path Resolution → Stage 4: Tag Resolution → RenderableSpec → Template → File
```

Each stage is a function:
```go
type RenderStage func(spec *ResolvedSpec, lang Lang) *RenderableSpec

// Stages are composable
pipeline := []RenderStage{
    ResolveNames,      // applies naming conventions
    ResolveTypes,      // maps TypeKind → language type strings
    ResolvePaths,      // computes output file paths
    ResolveTags,       // builds struct tags / annotations
    ResolveImports,    // collects import paths
}
```

The `RenderableSpec` is a language-specific mirror of `ResolvedSpec` that templates consume directly:

```go
type RenderableSpec struct {
    Tables []RenderableTable
    APIs   []RenderableAPI
    // ...
}

type RenderableTable struct {
    Domain    *ResolvedTable    // original domain data
    ModelName string            // "User" (Go) / "User" (Java)
    Package   string            // "users" (Go) / "com.example.model" (Java)
    FilePath  string            // "tables/users/model.go"
    Fields    []RenderableField
}

type RenderableField struct {
    Domain   *ResolvedField
    Name     string     // "FirstName" (Go) / "firstName" (Java)
    Type     string     // "string" (Go) / "String" (Java)
    Tag      string     // `json:"first_name" validate:"required"` (Go) / "" (Java)
    Annotation string   // "" (Go) / "@NotNull" (Java)
}
```

**Pros:**
- Each stage is independently testable
- Stages can be composed differently per language
- Can add stages (e.g., "framework adaptation" stage for Gin vs Echo)
- The RenderableSpec is what templates receive — no adapter calls in templates

**Cons:**
- More structs — RenderableSpec mirrors ResolvedSpec but with strings
- Pipeline ordering matters — stages have implicit dependencies
- Harder to understand the full transformation in one place
- More memory — copies data at each stage

---

### Approach C: Generic Generator with Manifest

**Core idea:** Eliminate individual generator Go files entirely. Define generators as manifest entries (YAML or Go struct registry). One generic generator reads the manifest, queries the ResolvedSpec, creates a language adapter, and renders the template.

**Architecture:**
```
ResolvedSpec + Manifest + LanguageAdapter → GenericGenerator → Template → File
```

**Manifest (as Go struct registry):**
```go
var GoGenerators = []GeneratorDef{
    {
        ID:       "table.model",
        Source:   SourceTableModel,             // enum: what to iterate from ResolvedSpec
        Template: "go/table/model.go.tmpl",
        Output:   "tables/{{TableName}}/model.go",  // path template
    },
    {
        ID:       "table.repo",
        Source:   SourceTableModel,
        Needs:    []string{"RepositoryInterface", "RepositoryImpl"},  // auto-lookup from spec
        Template: "go/table/repo.go.tmpl",
        Output:   "tables/{{TableName}}/repository.go",
        DependsOn: []string{"table.model"},
    },
    {
        ID:       "api.service",
        Source:   SourceServiceImpl,
        Template: "go/api/service.go.tmpl",
        Output:   "apis/{{PackageName}}/service.go",
        DependsOn: []string{"api.hooks", "table.repo", "external"},
    },
    // ...
}
```

**The GenericGenerator:**
```go
func (g *GenericGenerator) Generate(ctx GeneratorContext) ([]emitter.File, error) {
    // 1. Manifest tells us what to iterate
    items := g.querySpec(ctx.Spec, g.def.Source)
    
    // 2. For each item, build render context
    var files []emitter.File
    for _, item := range items {
        rc := RenderContext{
            Spec:    ctx.Spec,
            Item:    item,
            Adapter: g.adapter,  // LanguageAdapter
            Module:  ctx.Spec.Module,
        }
        
        // 3. Auto-lookup related objects if Needs is specified
        for _, need := range g.def.Needs {
            rc.Related[need] = g.lookupRelated(ctx.Spec, item, need)
        }
        
        // 4. Render template
        content, err := g.engine.Render(g.def.Template, rc)
        
        // 5. Compute output path
        path := g.adapter.ResolvePath(g.def.Output, item)
        
        files = append(files, emitter.File{Path: path, Content: content})
    }
    return files, nil
}
```

**Templates receive a `RenderContext`** and call the adapter via FuncMap:
```
package {{ call .Adapter.PackageName .Item.Name }}

type {{ call .Adapter.ModelName .Item.Name }} struct {
{{ range .Item.Fields }}
    {{ call $.Adapter.FieldName .Name }} {{ call $.Adapter.RenderType .Type }} {{ call $.Adapter.RenderFieldTag . $.TagContextModel }}
{{ end }}
}
```

**The DAG is auto-derived from manifest DependsOn fields** — no hand-written planner.

**Pros:**
- Adding a generator = manifest entry + template file (no Go code)
- DAG auto-derived — no planner to maintain
- Templates are the single source of truth for code shape
- Eliminates 12 generator files, 12 data struct definitions
- Adding Java = new adapter + new manifest + new templates

**Cons:**
- Templates are harder to read with `call .Adapter.Xxx` everywhere
- The generic generator is a complex piece of code to write and debug
- Manifest is a new DSL to learn — meta-complexity
- Less type safety — manifest entries are strings, not checked at compile time
- Debugging template errors is harder when everything goes through one generic path

---

## Recommended Approach: A + C Hybrid

Use **Approach A (Language Adapter)** for the language abstraction and **Approach C (Manifest)** for the generator infrastructure. Skip Approach B (too many intermediate structs).

**Concretely:**

1. **Language Adapter interface** (from Approach A) — clean, type-safe, one implementation per language
2. **Generator manifest** (from Approach C) — eliminates 12 generator files, auto-derives DAG
3. **Templates receive resolved domain types directly** — no intermediate data structs
4. **Adapter methods exposed via FuncMap** — templates call them naturally

This gives you the best of both worlds: type-safe language abstraction + zero-boilerplate generator addition.

---

## Refactoring Plan — Phase by Phase

### Phase 1: Clean ResolvedSpec (Domain-Only)

**Goal:** Remove all language-specific fields from the resolver output.

**1a. Simplify TypeDescriptor**

Remove:
```go
// DELETE these fields
GoType   string
JavaType string
GoPointer bool
DBType   string
DBNullable bool
```

Keep only:
```go
type TypeDescriptor struct {
    Kind       TypeKind    // Str, Int, UUID, Decimal, Timestamp, Date, JSON, Enum, Custom
    CustomName string      // "Money" — only when Kind == Custom
    Nullable   bool        // was GoPointer + DBNullable
    IsArray    bool        // for []*Type returns
    IsEnum     bool
    EnumValues []string
}
```

**1b. Simplify ResolvedField**

Remove:
```go
// DELETE these fields
DBColumn       string
GoStructTag    string
JavaAnnotation string
```

Keep:
```go
type ResolvedField struct {
    Name     string         // "first_name" — original from YAML
    Type     TypeDescriptor
    Required bool
    Unique   bool
    Nullable bool
    Private  bool
    Default  interface{}
    Compute  bool
    Values   []string       // enum values
    Rules    []ResolvedRule  // validation rules (language-agnostic)
}
```

The generator uses the adapter to derive:
- `FieldName("first_name")` → "FirstName" (Go) / "firstName" (Java)
- `ColumnName("first_name")` → "first_name" (same for all languages — DB is language-neutral)
- `RenderFieldTag(field, TagContextModel)` → `` `json:"first_name" db:"first_name" validate:"required"` ``
- `RenderFieldTag(field, TagContextDTO)` → `` `json:"first_name" validate:"required"` `` (no db tag)

**1c. Remove Path from all resolved types**

`ResolvedObject.Path`, `ResolvedInterface.Path`, `ResolvedImplementation.Path` — all removed. The generator/adapter computes paths at render time.

**1d. Remove pre-rendered function signatures**

Instead of:
```go
ResolvedParam{Name: "ctx", Type: TypeDescriptor{GoType: "context.Context"}}
```

The resolver outputs:
```go
ResolvedQuery{Kind: FindBy, Fields: ["email"]}
```

The adapter derives:
```go
// Go: GetUsersByEmail(ctx context.Context, email string) ([]*User, error)
// Java: List<User> findByEmail(String email)
```

**1e. Remove import resolver from the resolver package**

`import_resolver.go` moves entirely to the language adapter layer. The resolver does not compute imports.

**1f. Remove utils.go Go helpers**

Delete: `ctxParam()`, `uuidParam()`, `ptrParam()`, `slicePtrParam()`, `errReturn()`, `ptrReturn()`, `slicePtrReturn()`, `primitiveParam()`, `primitiveReturn()`, `deriveGoStructTag()`, `renderKeyFunc()`.

Keep: `toPascalCase()`, `toSnakeCase()` (these are domain naming, not language-specific — table names use snake_case universally).

**Impact:** `resolved.go` shrinks significantly. `typemap.go` becomes trivial (just maps DSL type strings to TypeKind enum). `level1_objects.go`, `level2_interfaces.go`, `level3_impls.go` simplify dramatically — no more Go type rendering.

**Risk:** All existing tests break. Must rewrite all resolver tests to assert domain properties, not Go-specific output.

---

### Phase 2: Language Adapter

**Goal:** Create the Go language adapter that generators and templates use.

**2a. Define the interface** — `internal/lang/adapter.go`

```go
type LanguageAdapter interface {
    // Naming
    ModelName(tableName string) string
    FieldName(yamlName string) string
    PackageName(name string) string
    QueryFuncName(query ResolvedQuery, modelName string) string
    ConstructorName(typeName string) string
    HookSuffix(touchKind, name, op string) string
    
    // Types
    RenderType(td TypeDescriptor) string
    RenderSliceType(td TypeDescriptor) string
    ContextParamType() string     // "context.Context" for Go
    ErrorType() string             // "error" for Go
    
    // Tags / Annotations
    RenderFieldMeta(f ResolvedField, ctx TagContext) string
    
    // Paths
    OutputPath(category, name, file string) string
    
    // Imports
    ImportsForTypes(fields []ResolvedField) []string
    ImportsForInfra(kind string) []string
    RuntimeImport(module, pkg string) string
    
    // Formatting
    FormatSource(path string, src []byte) ([]byte, error)
    
    // File
    FileExtension() string
    TemplateDir() string  // "go" — for looking up templates under templates/{lang}/
}
```

**2b. Implement GoAdapter** — `internal/lang/go/adapter.go`

Contains all the logic currently scattered across:
- `typemap.go` (type mapping)
- `utils.go` (Go param/return helpers, struct tags)
- `import_resolver.go` (import resolution)
- `level1_objects.go` (file paths)
- `generators/go/*/helpers.go` (naming conventions)
- `template/funcmap.go` (case conversion — shared, but Go-specific acronym handling stays here)

**2c. Wire adapter into FuncMap**

```go
func BuildFuncMap(adapter lang.LanguageAdapter) template.FuncMap {
    return template.FuncMap{
        "modelName":   adapter.ModelName,
        "fieldName":   adapter.FieldName,
        "packageName": adapter.PackageName,
        "renderType":  adapter.RenderType,
        "fieldTag":    adapter.RenderFieldMeta,
        "runtimeImport": adapter.RuntimeImport,
        // ... plus generic helpers like toLower, join, etc.
    }
}
```

Templates become:
```
package {{ packageName .Name }}

type {{ modelName .Name }} struct {
{{ range .Fields }}
    {{ fieldName .Name }} {{ renderType .Type }} {{ fieldTag . "model" }}
{{ end }}
}
```

Clean, readable, no `call .Adapter.Xxx` noise.

---

### Phase 3: Generic Generator + Manifest

**Goal:** Replace 12 individual generator files with one generic generator and a manifest.

**3a. Define manifest format** — `internal/generator/manifest.go`

```go
type GeneratorDef struct {
    ID        string          // "table.model"
    Source    SourceKind       // what to iterate from ResolvedSpec
    Template  string          // template path relative to lang dir: "table/model"
    OutputFn  OutputFunc      // function to compute output path
    DependsOn []string        // other generator IDs
    Filter    FilterFunc      // optional: skip items that don't match
}

type SourceKind int
const (
    SourceTypes        SourceKind = iota  // iterate all custom types (single batch)
    SourceTable                           // iterate per table
    SourceExternal                        // iterate per external
    SourceServiceGroup                    // iterate per resource group
    SourceGlobal                          // single invocation, no iteration
)
```

**3b. Register Go generators** — `internal/generators/go/registry.go`

```go
var Generators = []generator.GeneratorDef{
    {ID: "types",         Source: SourceTypes,        Template: "types/types",       OutputFn: typesOutput},
    {ID: "table.model",   Source: SourceTable,        Template: "table/model",       OutputFn: tableOutput("model")},
    {ID: "table.errors",  Source: SourceTable,        Template: "table/errors",      OutputFn: tableOutput("errors"), Filter: hasErrors},
    {ID: "table.repo",    Source: SourceTable,        Template: "table/repo",        OutputFn: tableOutput("repository"), DependsOn: []string{"table.model"}},
    {ID: "external",      Source: SourceExternal,     Template: "external/external", OutputFn: externalOutput},
    {ID: "api.dto",       Source: SourceServiceGroup, Template: "api/dto",           OutputFn: apiOutput("dto")},
    {ID: "api.context",   Source: SourceServiceGroup, Template: "api/context",       OutputFn: apiOutput("context"),  DependsOn: []string{"api.dto"}},
    {ID: "api.hooks",     Source: SourceServiceGroup, Template: "api/hooks",         OutputFn: apiOutput("hooks"),    DependsOn: []string{"api.context"}},
    {ID: "api.service",   Source: SourceServiceGroup, Template: "api/service",       OutputFn: apiOutput("service"),  DependsOn: []string{"api.hooks", "table.repo", "external"}},
    {ID: "api.handler",   Source: SourceServiceGroup, Template: "api/handler",       OutputFn: apiOutput("handler"),  DependsOn: []string{"api.service"}},
    {ID: "routes",        Source: SourceGlobal,       Template: "infra/routes",      OutputFn: infraOutput("routes"), DependsOn: []string{"api.handler"}},
    {ID: "wire",          Source: SourceGlobal,       Template: "infra/wire",        OutputFn: infraOutput("wire"),   DependsOn: []string{"routes"}},
}
```

**3c. GenericGenerator** — `internal/generator/generic.go`

One generator type that handles all manifest entries:
```go
type GenericGenerator struct {
    def     GeneratorDef
    engine  *template.Engine
    adapter lang.LanguageAdapter
}

func (g *GenericGenerator) Generate(ctx GeneratorContext) ([]emitter.File, error) {
    items := g.querySpec(ctx.Spec, g.def.Source)
    if g.def.Filter != nil {
        items = g.def.Filter(items)
    }
    
    var files []emitter.File
    for _, item := range items {
        tmplPath := g.adapter.TemplateDir() + "/" + g.def.Template + g.adapter.TemplateExtension()
        content, err := g.engine.Render(tmplPath, item)
        outputPath := g.def.OutputFn(item, g.adapter)
        files = append(files, emitter.File{Path: outputPath, Content: content})
    }
    return files, nil
}
```

**3d. Delete 12 individual generator files.** Replace with one manifest + one generic generator.

---

### Phase 4: Auto-Derived DAG

**Goal:** Planner reads the manifest's `DependsOn` fields and builds the DAG automatically.

**4a. Rewrite planner** — `internal/plan/planner.go`

```go
func Build(spec *ResolvedSpec, generators []GeneratorDef, adapter LanguageAdapter) (*Plan, error) {
    g := NewGraph()
    
    for _, gen := range generators {
        items := querySpec(spec, gen.Source)
        for _, item := range items {
            nodeID := gen.ID + ":" + itemSlug(item)
            g.AddNode(&Node{ID: nodeID, Generator: gen.ID, Payload: item})
            
            // Auto-add dependency edges from DependsOn
            for _, depGenID := range gen.DependsOn {
                depItems := querySpec(spec, findDef(depGenID).Source)
                for _, depItem := range depItems {
                    depNodeID := depGenID + ":" + itemSlug(depItem)
                    g.AddEdge(nodeID, depNodeID)
                }
            }
        }
    }
    
    return g.Sort()
}
```

No hand-written dependency logic. The manifest declares it, the planner enforces it.

**4b. Remove "go." prefix from node IDs.** Node IDs become `"table.model:users"`, `"api.service:order_apis"`, etc. The language is determined by which manifest is loaded, not by the node ID.

---

### Phase 5: Emitter Language Awareness

**Goal:** The emitter doesn't hardcode Go formatting.

**5a. Move formatting to the adapter**

```go
// Before (emitter.go line 65)
if strings.HasSuffix(f.Path, ".go") {
    formatted, err := imports.Process(fullPath, f.Content, nil)
}

// After
formatted, err := adapter.FormatSource(f.Path, f.Content)
```

The `GoAdapter.FormatSource` runs `goimports`. The `JavaAdapter.FormatSource` runs `google-java-format`. The emitter doesn't know or care.

**5b. Remove runtime library embedding from orchestrator**

Currently the orchestrator walks `lib.FS` and copies Go runtime files. This is Go-specific. Move to the GoAdapter:

```go
func (a *GoAdapter) RuntimeFiles() []emitter.File {
    // Walk the embedded stencil-go FS and return files
}
```

The orchestrator calls `adapter.RuntimeFiles()` and stages them. The Java adapter returns Java runtime files.

---

### Phase 6: Template Restructuring

**Goal:** Templates receive domain types + adapter via FuncMap.

**6a. Template data is always a known struct:**

```go
type TemplateContext struct {
    Spec     *ResolvedSpec            // full spec for cross-references
    Table    *ResolvedTable           // when rendering a table (nil otherwise)
    External *ResolvedExternal        // when rendering an external (nil otherwise)
    Group    *ResolvedResourceGroup   // when rendering an API group (nil otherwise)
    Module   string
}
```

Every template receives the same context type. Fields are nil when not relevant. Templates access what they need:

```
{{ range .Table.Fields }}
    {{ fieldName .Name }} {{ renderType .Type }} {{ fieldTag . "model" }}
{{ end }}
```

**6b. Move templates to `templates/{lang}/`**

```
templates/
  go/
    table/model.go.tmpl
    table/repo.go.tmpl
    table/errors.go.tmpl
    api/dto.go.tmpl
    api/context.go.tmpl
    api/hooks.go.tmpl
    api/service.go.tmpl
    api/handler.go.tmpl
    external/external.go.tmpl
    infra/routes.go.tmpl
    infra/wire.go.tmpl
    types/types.go.tmpl
  java/       ← future
    table/Model.java.tmpl
    ...
```

The adapter's `TemplateDir()` returns `"go"` or `"java"`, and the generic generator prepends it.

---

### Phase 7: CLI and Configuration

**Goal:** The CLI supports language selection and the configuration is clean.

**7a. Language flag on CLI:**
```bash
stencil generate --lang go stencil.yaml
stencil generate --lang java stencil.yaml  # future
```

**7b. Main.go becomes:**
```go
adapter := lang.NewAdapter(spec.Lang)  // factory from "go"/"java" string
generators := lang.Generators(spec.Lang)  // manifest for this language
engine := template.NewEngine(adapter)
plan := plan.Build(resolved, generators, adapter)
orch := generator.NewOrchestrator(engine, emitter, adapter)
orch.Run(resolved, plan)
```

No explicit generator registration. The manifest IS the registry.

---

### Phase 8: Resolver Restructuring (Optional, Deeper)

**Goal:** Resolver output mirrors YAML structure instead of generation artifacts.

This is the structural change from the earlier conversation — replacing flat `Objects[]`/`Interfaces[]`/`Implementations[]` lists with domain-oriented:

```go
type ResolvedSpec struct {
    Project   string
    Module    string
    Lang      string       // just the string, not a typed constant
    Framework string
    DB        string
    Config    []ResolvedConfigVar
    Types     []ResolvedType
    Tables    []ResolvedTable
    Externals []ResolvedExternal
    Resources []ResolvedResourceGroup
}
```

Each domain concept is self-contained. `ResolvedTable` has fields, queries, errors, state machine — everything. No cross-referencing by string name.

**This is the biggest change** and could be done independently of Phases 1-7. Phases 1-7 can work with the current resolver structure (just stripping Go-specific fields). Phase 8 is a deeper restructuring that changes the resolver's output shape.

---

## Migration Path

The phases are designed to be incremental. Each phase can be merged independently:

```
Phase 1 (clean resolved types)     → breaks all tests, must be done with Phase 2
Phase 2 (language adapter)         → provides the replacement for what Phase 1 removes
Phase 3 (generic generator)        → eliminates boilerplate, can be done after 1+2
Phase 4 (auto DAG)                 → follows from Phase 3 manifest
Phase 5 (emitter)                  → small, independent
Phase 6 (templates)                → follows from Phase 2 adapter
Phase 7 (CLI)                      → follows from Phase 3 manifest
Phase 8 (resolver restructure)     → independent, can be done first or last
```

**Minimum viable refactor:** Phases 1 + 2 + 6. This gives you a language-agnostic resolver and adapter-based templates, while keeping the existing generator structure temporarily.

**Full platform:** All 8 phases. After this, adding a new language is: implement the adapter interface + write templates + define a manifest. No new Go code in the generator layer.
