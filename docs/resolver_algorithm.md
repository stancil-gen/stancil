# Resolver Algorithm Design
## Phase 1: Brute Force | Phase 2: Advanced

---

## The Core Problem

The Resolver has one fundamental constraint: **forward references**.

A `ResolvedField` carries a `TypeRef *ResolvedObject`. That pointer can only be set after the target `ResolvedObject` has been built. But the order in which the spec author declares things is arbitrary:

```yaml
tables:
  - name: orders
    fields:
      - name: total   type: Money    # Money doesn't exist yet in this pass
types:
  - name: Money
    fields:
      - name: amount  type: decimal
```

Within each level, and across levels, things reference things that may not be built yet. The algorithm must handle this.

The same problem exists for the ImportResolver: a `ResolvedObject` containing a `TypeRef` pointing to another `ResolvedObject` must be walked without re-visiting nodes or entering infinite loops when custom types reference each other.

---

## Phase 1: Brute Force Algorithm

### Philosophy

Process everything in a fixed, hardcoded order that happens to satisfy dependencies in the common case. No graph. No topological sort. Just sequential loops with one retry pass.

### Resolver — Phase 1

```
PROCEDURE Resolve(ast: SpecAST) → ResolvedSpec

  spec := new ResolvedSpec

  // ── Level 1: Objects ────────────────────────────────────────────────────

  // 1a. Types first — they are leaves, nothing references them first
  FOR each type in ast.Types:
    obj := buildTypeObject(type)
    spec.Objects.append(obj)

  // 1b. Tables — may reference types via field types
  FOR each table in ast.Tables:
    obj := buildTableModel(table, spec.Objects)
    spec.Objects.append(obj)

  // 1c. API types — may reference types or table models
  FOR each group in ast.Resources:
    FOR each api in group.APIs:
      IF api.Request != "":
        spec.Objects.append(buildRequestDTO(api, spec.Objects))
      IF api.Response != "":
        spec.Objects.append(buildResponseDTO(api, spec.Objects))
      spec.Objects.append(buildSharedContext(api, spec.Objects))

  // 1d. External types
  FOR each external in ast.Externals:
    FOR each method in external.Methods:
      IF method.Body != "":
        spec.Objects.append(buildExternalInput(external, method, spec.Objects))
      IF method.Response != "":
        spec.Objects.append(buildExternalOutput(external, method, spec.Objects))

  // 1e. Transaction params
  FOR each tx in ast.Transactions:
    FOR each step in tx.Steps:
      spec.Objects.append(buildTransactionParams(tx, step, spec.Objects))

  // Level 1A: ImportResolver pass
  ir := NewImportResolver(spec.Lang, spec.Module)
  FOR each obj in spec.Objects:
    obj.Imports[spec.Lang] = ir.ForObject(obj)

  // ── Level 2: Interfaces ─────────────────────────────────────────────────

  // 2a. Repository interfaces — only depend on Level 1 objects (table models)
  FOR each table in ast.Tables:
    iface := buildRepositoryInterface(table, spec.Objects)
    spec.Interfaces.append(iface)

  // 2b. Cache interfaces
  IF ast.Cache != nil:
    FOR each cacheIface in ast.Cache.Interfaces:
      spec.Interfaces.append(buildCacheInterface(cacheIface, spec.Objects))

  // 2c. External interfaces
  FOR each external in ast.Externals:
    spec.Interfaces.append(buildExternalInterface(external, spec.Objects))

  // 2d. Hook interfaces — depend on SharedContext objects from Level 1
  FOR each group in ast.Resources:
    FOR each api in group.APIs:
      spec.Interfaces.append(buildHookInterface(api, spec.Objects))

  // 2e. Service interfaces — one per ResourceGroup
  FOR each group in ast.Resources:
    spec.Interfaces.append(buildServiceInterface(group, spec.Objects))

  // Level 2A: ImportResolver pass
  FOR each iface in spec.Interfaces:
    iface.Imports[spec.Lang] = ir.ForInterface(iface)

  // ── Level 3: Implementations ────────────────────────────────────────────

  // 3a. Repository implementations
  FOR each table in ast.Tables:
    repoInterface := spec.Interfaces.find(RepositoryInterface, table.Name)
    spec.Implementations.append(buildRepositoryImpl(table, repoInterface, spec.Objects))

  // 3b. Cache implementations
  IF ast.Cache != nil:
    FOR each cacheIface in ast.Cache.Interfaces:
      ci := spec.Interfaces.find(CacheInterface, cacheIface.Name)
      spec.Implementations.append(buildCacheImpl(cacheIface, ci, spec.Objects))

  // 3c. External implementations + mocks
  FOR each external in ast.Externals:
    ei := spec.Interfaces.find(ExternalInterface, external.Name)
    spec.Implementations.append(buildExternalImpl(external, ei, spec.Objects))
    spec.Implementations.append(buildExternalMock(external, ei, spec.Objects))

  // 3d. Transaction implementations
  FOR each tx in ast.Transactions:
    spec.Implementations.append(buildTransactionImpl(tx, spec.Objects, spec.Interfaces))

  // 3e. Service implementations — most complex, depend on everything above
  FOR each group in ast.Resources:
    si := spec.Interfaces.find(ServiceInterface, group.Name)
    spec.Implementations.append(buildServiceImpl(group, si, spec.Objects, spec.Interfaces))

  // Level 3A: ImportResolver pass
  FOR each impl in spec.Implementations:
    impl.Imports[spec.Lang] = ir.ForImplementation(impl)

  RETURN spec
END
```

### buildTypeObject (illustrative detail)

```
PROCEDURE buildTypeObject(type: TypeAST) → ResolvedObject
  obj := ResolvedObject{
    Name: type.Name,
    Path: "generated/types/types.go",
    Kind: TypeObject,
  }
  FOR each field in type.Fields:
    obj.Fields.append(ResolvedField{
      Name:    pascal(field.Name),
      Type:    MapType(field.Type, lang, db, field.Nullable),
      TypeRef: nil,           // set in a second pass — brute force limitation
      ...
    })
  RETURN obj
END
```

**The brute force limitation:** `TypeRef` on a field cannot be set during the first pass if the referenced type hasn't been built yet. In Phase 1, we set TypeRef to nil during construction and fill it in a single second pass after all objects are built:

```
PROCEDURE linkTypeRefs(objects: []ResolvedObject)
  index := map[string]*ResolvedObject
  FOR each obj in objects:
    index[obj.Name] = &obj

  FOR each obj in objects:
    FOR each field in obj.Fields:
      IF field.Type.IsCustom:
        field.TypeRef = index[field.Type.GoType without "*"]
        // if nil: this is a bug caught by the Validator, not the Resolver
END
```

This is called once after all Level 1 objects are built, before Level 1A.

### ImportResolver — Phase 1

```
PROCEDURE ForObject(obj: *ResolvedObject) → ImportSet
  seen   := set<string>         // dedup
  result := []string

  FOR each field in obj.Fields:
    imp := importForType(field.Type)
    IF imp != "" AND imp NOT IN seen:
      seen.add(imp)
      result.append(imp)

    // If field references a custom type, add that type's package
    IF field.TypeRef != nil:
      customPkg := packageForObject(field.TypeRef)
      IF customPkg != "" AND customPkg NOT IN seen:
        seen.add(customPkg)
        result.append(customPkg)

  sort(result)
  RETURN ImportSet{Lang: lang, Paths: result}
END

// NOTE: Phase 1 does NOT walk into TypeRef.Fields recursively.
// This means imports for nested custom types (Money inside Invoice) 
// are missed in Phase 1. Acceptable for brute force.
```

### What Phase 1 gets wrong (known gaps)

1. **Ordering sensitivity** — Phase 1 relies on processing `types` before `tables`. If you run it in a different order it breaks. It works only because the hardcoded order happens to match the dependency order in simple specs.

2. **TypeRef is set in a second pass** — not during construction. This means during `buildTypeObject`, you can't use `TypeRef` yet.

3. **Import collection is shallow** — `ForObject` does not recurse into `TypeRef.Fields`. If `User` has a field of type `Money`, and `Money` has a field of type `decimal`, `ForObject(User)` will not collect the `shopspring/decimal` import. The generator may produce missing imports.

4. **No cycle protection in import collection** — if two custom types somehow reference each other (should be caught by Validator, but defensively), `ForObject` would loop.

5. **No extension point** — adding a new AST type (e.g. `StorageAST`) means modifying the Resolve procedure. There is no registration mechanism.

---

## Phase 2: Advanced Algorithm

### What changes

| Problem | Phase 1 | Phase 2 |
|---------|---------|---------|
| Ordering within each level | Hardcoded sequence | Topological sort on dependency graph |
| TypeRef linking | Second pass after all objects built | Set during construction via registry lookup |
| Import collection depth | Shallow (no recursion into TypeRef) | DFS with visited set — collects transitively |
| Cycle protection in imports | None | Visited set in DFS |
| Adding a new AST type | Modify Resolve() | Register a Builder |
| Adding a new language | Modify importForType() switch | Register a LanguageAdapter |

### Phase 2 Resolver Algorithm

#### Key data structure: the Registry

```
Registry is a lookup table built incrementally as objects are resolved.
It allows any builder to look up already-resolved objects by name.

Registry {
  objects:    map[string]*ResolvedObject        // name → object
  interfaces: map[string]*ResolvedInterface     // name → interface
  impls:      map[string]*ResolvedImplementation
}

Registry.lookupObject(name) → *ResolvedObject | nil
Registry.registerObject(obj *ResolvedObject)
// ... same for interfaces and impls
```

#### Phase 2 Level 1 Algorithm

```
PROCEDURE ResolveLevel1(ast: SpecAST, registry: Registry) → []ResolvedObject

  // Step 1: Build dependency graph across all Level 1 sources
  graph := DependencyGraph{}

  FOR each type in ast.Types:
    node := graph.addNode("type:" + type.Name, type)
    FOR each field in type.Fields:
      IF !isPrimitive(field.Type):
        graph.addEdge(node, "type:" + field.Type)   // type depends on another type

  FOR each table in ast.Tables:
    node := graph.addNode("table:" + table.Name, table)
    FOR each field in table.Fields:
      IF !isPrimitive(field.Type):
        graph.addEdge(node, "type:" + field.Type)   // table depends on a type

  FOR each group in ast.Resources:
    FOR each api in group.APIs:
      node := graph.addNode("api:" + api.Name, api)
      // api DTOs may reference types
      FOR each dtoField in api.DTOFields():
        IF !isPrimitive(dtoField.Type):
          graph.addEdge(node, "type:" + dtoField.Type)

  // Step 2: Topological sort → process in dependency order
  ordered := graph.TopologicalSort()  // Kahn's algorithm (same as what the DAG already uses)
  // If cycle detected: panic — Validator should have caught this

  // Step 3: Build ResolvedObjects in topological order
  result := []ResolvedObject{}

  FOR each node in ordered:
    builder := BuilderRegistry.get(node.SourceKind)  // SourceKind: TypeAST|TableAST|APIAST|...
    obj := builder.Build(node.Source, registry)
    registry.registerObject(obj)  // immediately available for subsequent builds
    result.append(obj)

  RETURN result
END
```

**Why topological sort enables TypeRef during construction:**

When processing in topological order, any type that a field references has already been built and is in the registry. So when building `orders` (which has a field `total: Money`), `Money` is already in `registry.lookupObject("Money")`. TypeRef can be set during `Build()`, not in a second pass.

#### The Builder pattern (extension point)

```
// Adding a new AST type in Phase 2: implement ObjectBuilder and register it.

INTERFACE ObjectBuilder {
  SourceKind() ASTKind        // which AST type this builder handles
  Dependencies(src AST) []string  // what names must be resolved before this builder runs
  Build(src AST, registry Registry) ResolvedObject
}

BuilderRegistry {
  Register(builder ObjectBuilder)
  Get(kind ASTKind) ObjectBuilder
}

// Built-in builders registered at startup:
BuilderRegistry.Register(TypeObjectBuilder{})
BuilderRegistry.Register(TableModelBuilder{})
BuilderRegistry.Register(RequestDTOBuilder{})
BuilderRegistry.Register(ResponseDTOBuilder{})
BuilderRegistry.Register(SharedContextBuilder{})
BuilderRegistry.Register(ExternalInputBuilder{})
BuilderRegistry.Register(ExternalOutputBuilder{})
BuilderRegistry.Register(TransactionParamsBuilder{})

// Adding StorageAST support later:
BuilderRegistry.Register(StorageObjectBuilder{})
// Nothing else changes.
```

The `Dependencies()` method is what feeds the dependency graph. The graph builder calls `Dependencies()` on every source AST node to construct edges before sorting.

#### Phase 2 ImportResolver Algorithm

```
PROCEDURE ForObject(obj: *ResolvedObject) → ImportSet

  collector := ImportCollector{lang: lang, module: module}
  visited   := set<string>     // object names already walked — prevents cycles

  walkObject(obj, collector, visited)

  RETURN ImportSet{Lang: lang, Paths: collector.sorted()}
END

PROCEDURE walkObject(obj: *ResolvedObject, collector, visited)
  IF obj.Name IN visited: RETURN     // already walked — stop
  visited.add(obj.Name)

  FOR each field in obj.Fields:
    // Collect import for this field's primitive type
    imp := importForType(field.Type, lang)
    IF imp != "": collector.add(imp)

    // If custom type, recurse into it
    IF field.TypeRef != nil:
      // Collect the package where the custom type lives
      pkg := packageForPath(field.TypeRef.Path, module)
      IF pkg != "": collector.add(pkg)
      // Recurse to collect transitive imports
      walkObject(field.TypeRef, collector, visited)
END
```

**Why visited set is essential:**

Consider `User` → has field `Address`, `Address` → has field `Country`, `Country` → has field `Region`. Without a visited set, nothing loops. But if `Address` and `User` both have a field of type `Money`, without a visited set we walk `Money` twice. More importantly, the Validator prevents cycles in custom types, but the ImportResolver shouldn't rely on that — it protects itself.

```
// Transitive import collection example:
// User has: name (str), address (Address), wallet (Money)
// Address has: city (str), country (Country)
// Country has: code (str)
// Money has: amount (decimal), currency (str)

walkObject(User, collector, visited={})
  visited = {User}
  field name: str → no import
  field address: custom → package for Address → walkObject(Address, collector, visited={User})
    visited = {User, Address}
    field city: str → no import
    field country: custom → package for Country → walkObject(Country, collector, visited={User, Address})
      visited = {User, Address, Country}
      field code: str → no import
      RETURN
    RETURN
  field wallet: custom → package for Money → walkObject(Money, collector, visited={User, Address, Country})
    visited = {User, Address, Country, Money}
    field amount: decimal → "github.com/shopspring/decimal"
    field currency: str → no import
    RETURN

Result: ["github.com/shopspring/decimal"]  (and the module-local packages for Address, Country, Money)
```

Phase 1 would have returned only the top-level imports and missed `shopspring/decimal` inside `Money`.

#### The LanguageAdapter pattern (extension point for imports)

```
// Adding a new language in Phase 2: implement LanguageAdapter and register it.

INTERFACE LanguageAdapter {
  Lang() Lang
  ImportForType(kind TypeKind) string          // primitive type → import path
  ImportForCustomType(path string) string      // custom type's generated path → import
  PackageForPath(generatedPath string) string  // file path → package identifier
}

LanguageAdapterRegistry {
  Register(adapter LanguageAdapter)
  Get(lang Lang) LanguageAdapter
}

// Built-in:
LanguageAdapterRegistry.Register(GoAdapter{module: module})
LanguageAdapterRegistry.Register(JavaAdapter{groupId: groupId})

// Adding Rust later:
LanguageAdapterRegistry.Register(RustAdapter{crateRoot: crateRoot})
// importForType() becomes: get adapter for lang, call ImportForType(kind)
```

The ImportResolver's `importForType` function stops being a switch statement and becomes a single dispatch:

```
PROCEDURE importForType(t: TypeDescriptor, lang: Lang) → string
  adapter := LanguageAdapterRegistry.Get(lang)
  IF t.IsCustom:
    RETURN adapter.ImportForCustomType(t.GoType)  // or JavaType for Java
  RETURN adapter.ImportForType(t.Kind)
END
```

#### Phase 2 Level 2 and Level 3 Algorithms

Same structure as Level 1, but the dependency graph connects to Level 1 objects via the registry.

```
PROCEDURE ResolveLevel2(ast: SpecAST, registry: Registry) → []ResolvedInterface

  graph := DependencyGraph{}

  FOR each table in ast.Tables:
    node := graph.addNode("repo-iface:" + table.Name, table)
    // depends on TableModel existing in registry
    graph.addEdge(node, "obj:table:" + table.Name)

  FOR each group in ast.Resources:
    node := graph.addNode("svc-iface:" + group.Name, group)
    FOR each api in group.APIs:
      // depends on RequestDTO and ResponseDTO objects
      graph.addEdge(node, "obj:api-req:" + api.Name)
      graph.addEdge(node, "obj:api-res:" + api.Name)
      // hook interface depends on SharedContext
      hookNode := graph.addNode("hook-iface:" + api.Name, api)
      graph.addEdge(hookNode, "obj:ctx:" + api.Name)

  // Topological sort — cross-level edges are satisfied by registry (pre-populated from Level 1)
  ordered := graph.TopologicalSort()

  FOR each node in ordered:
    builder := InterfaceBuilderRegistry.get(node.SourceKind)
    iface := builder.Build(node.Source, registry)
    registry.registerInterface(iface)
    result.append(iface)

  RETURN result
END
```

Level 3 follows the same pattern. `ImplementationBuilderRegistry` handles it.

---

## Summary: What to Implement Now (Phase 1)

For the brute force implementation, you need:

**In `resolver.go`:**
```
Resolve()
  buildAllObjects()          → fixed order: types → tables → api-types → external-types → tx-params
  linkTypeRefs()             → second pass, set TypeRef on all custom-type fields
  runImportResolver(objects) → Phase 1 ImportResolver over all objects
  
  buildAllInterfaces()       → fixed order: repo → cache → external → hooks → service
  runImportResolver(ifaces)
  
  buildAllImplementations()  → fixed order: repo → cache → external → tx → service
  runImportResolver(impls)
```

**In `import_resolver.go`:**
```
ForObject(obj)    → shallow walk: iterate fields, collect importForType, collect packageForTypeRef
ForInterface(iface) → iterate function params and returns
ForImplementation(impl) → iterate dependencies
importForType(kind, lang) → switch on lang, switch on kind → return path or ""
```

**The one brute force assumption to document clearly:**

> Processing order is fixed: `types → tables → api-types → externals → transactions`. This works because the Validator has already confirmed no type references a table, and no table references an external. If that dependency invariant ever breaks (e.g. a future DSL feature allows it), Phase 1 will produce nil TypeRefs silently. Phase 2's topological sort handles arbitrary dependency order.

---

## Phase 2 Upgrade Path

When you're ready to upgrade:

1. Replace the fixed `buildAllObjects()` sequence with a dependency graph + topological sort.
2. Extract each `buildTypeObject()`, `buildTableModel()` etc. into `ObjectBuilder` implementations.
3. Register them in `BuilderRegistry`.
4. Replace the `importForType()` switch with `LanguageAdapterRegistry.Get(lang).ImportForType(kind)`.
5. Replace shallow `ForObject()` with DFS walk using a visited set.

Steps 4 and 5 (import resolver upgrade) can be done independently of steps 1–3. They have no shared state.
