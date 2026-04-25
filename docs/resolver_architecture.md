# Stencil Resolver — Architecture & Implementation Spec
## `internal/spec/resolver.go`

---

## 1. What the Resolver Is

The Resolver sits between the Validator and the Generators in the Stencil pipeline:

```
Parser → SpecAST → Validator → (valid SpecAST) → Resolver → ResolvedSpec → Generators
```

**Input:** A `SpecAST` that has already passed validation — no bad references, no type errors, no circular types.

**Output:** A `ResolvedSpec` — every field filled, every type derived, every implicit value made explicit. Generators receive this and do nothing but render templates. They must not do any logic, any inference, any derivation.

**Contract:** If validation passes, resolution must succeed. The Resolver never returns validation errors. If something goes wrong, it's a bug in the Resolver.

---

## 2. The 12 Steps (From the Tech Spec)

The Tech Spec defines the resolver steps in exact order. This is the authoritative sequence:

| Step | Name | What it does |
|------|------|-------------|
| 1 | Build TypeRegistry | Index all custom types by name for O(1) lookup during field resolution |
| 2 | Resolve lang/db/fw | Convert string enums to typed Go constants (`LangGo`, `DBPostgres`, `FrameworkGin`) |
| 3 | Resolve tables | Fill `GoType`/`JavaType`/`DBType` per field. Derive indexes from queries. Wire relations. Expand `soft_delete` and timestamps |
| 4 | Resolve externals | Fill typed request/response structs. Derive error mappings. Plan mock interface |
| 5 | Resolve cache | Fill typed `Get`/`Set`/`Delete`/`Invalidate` signatures from `value_type` |
| 6 | Resolve transactions | Plan typed params structs and result types for each step |
| 7 | Resolve touches | For each touch: resolve reference to infra item. Fill flag name (`Run{Kind}{Name}{Op}`). Confirm default |
| 8 | Plan shared context | For each API: build `ResolvedSharedContext` — all flag fields, all result fields, all error fields, request field, response field |
| 9 | Plan hook interface | For each API: derive all hook points. Entry (`BeforeAPI`), one Before+After per touch, `BeforeResponse`. Add `ComputeXxx` hooks for `compute: true` DTO fields |
| 10 | Resolve DTOs | Inline DTOs expanded. External DTO references validated. Mappers planned |
| 11 | Derive DI graph | For each API service: determine constructor params based on touches |
| 12 | Validate import DAG | Check no circular imports. Error if cycle detected |

The Resolver **must execute these in order**. Each step depends on the previous one.

---

## 3. Input: SpecAST

What the Resolver receives. These are the key structures from `internal/spec/ast.go`:

```go
type SpecAST struct {
    Version      int
    Project      string
    Lang         string          // "go" | "java"
    Framework    string          // "gin" | "echo" | "fiber" | "spring"
    DB           string          // "postgres" | "mysql" | "mongo"
    Config       []ConfigVarAST
    Tables       []TableAST
    Types        []CustomTypeAST
    Transactions []TransactionAST
    Externals    []ExternalAST
    Messaging    *MessagingAST
    Cache        *CacheAST
    Auth         *AuthAST
    Resources    []ResourceGroupAST
}

type TableAST struct {
    Name        string
    Fields      []FieldAST
    Queries     []QueryAST
    SoftDelete  bool
    BulkCreate  bool
    States      *StateMachineAST
    Errors      []string
}

type FieldAST struct {
    Name     string
    Type     string     // primitive string or custom type name
    Required bool
    Unique   bool
    Nullable bool
    Private  bool
    Default  interface{}
    Values   []string   // for enum types
    Rules    []RuleAST
}

type APIAST struct {
    Name     string
    Method   string
    Path     string
    Auth     string
    Roles    []string
    Owner    bool
    Request  string       // DTO name
    Response string       // DTO name
    DTOs     *DTOBlockAST
    Touches  []TouchAST
}

type TouchAST struct {
    // exactly one of these is set:
    Table       string
    External    string
    Message     string
    Cache       string
    Transaction string
    Storage     string
    // common:
    Op      string
    Method  string
    Flag    string    // override default flag name
    Default bool      // initial value of the flag
}
```

---

## 4. Output: ResolvedSpec

What the Generators receive. Every field must be filled. No generator should ever compute or derive anything from this.

```go
type ResolvedSpec struct {
    Project      string
    Module       string         // Go module path
    Lang         Lang           // typed constant, not string
    Framework    Framework
    DB           DBDriver
    Config       []ResolvedConfigVar
    Tables       []ResolvedTable
    Types        TypeRegistry   // map[string]ResolvedCustomType
    Transactions []ResolvedTransaction
    Externals    []ResolvedExternal
    Messaging    *ResolvedMessaging
    Cache        *ResolvedCache
    Auth         *ResolvedAuth
    Resources    []ResolvedResourceGroup
    Overrides    ResolvedOverrides
}

type ResolvedResourceGroup struct {
    Group    string
    BasePath string
    Auth     ResolvedAuth
    APIs     []ResolvedAPI
}

type ResolvedAPI struct {
    Name          string
    Method        string
    FullPath      string              // base_path + path concatenated
    Auth          ResolvedAuth
    RequestDTO    *ResolvedDTO
    ResponseDTO   *ResolvedDTO
    Touches       []ResolvedTouch    // same order as YAML
    SharedContext ResolvedSharedContext
    HookInterface ResolvedHookInterface
    Mappers       []ResolvedMapper
}

type ResolvedTouch struct {
    Kind           TouchKind   // Table | External | Message | Cache | Transaction | Storage
    Name           string      // resolved name
    Op             string
    Flag           string      // control flag field name in SharedContext
    Default        bool        // initial value of the flag
    // resolved pointers:
    TableRef       *ResolvedTable
    ExternalRef    *ResolvedExternal
    TransactionRef *ResolvedTransaction
    CacheRef       *ResolvedCacheInterface
}

type ResolvedSharedContext struct {
    TypeName string        // e.g. "CreateUserContext"
    Fields   []ContextField
}

type ContextField struct {
    Name    string
    GoType  string
    Kind    ContextFieldKind  // Request | Flag | Result | Error | Response
    Default interface{}       // for flags: bool
}

type ResolvedHookInterface struct {
    TypeName string
    Hooks    []ResolvedHook
}

type ResolvedHook struct {
    Name      string    // e.g. "BeforeCreateUser", "AfterTableUsersCreate"
    Signature string    // full Go function signature
    When      HookWhen  // Before | After
    TouchIdx  int       // -1 for entry/exit hooks
}
```

---

## 5. Step-by-Step Implementation

### Step 1: Build TypeRegistry

Index all custom types into a map for O(1) lookup in all subsequent steps.

```go
func (r *Resolver) buildTypeRegistry() TypeRegistry {
    registry := make(TypeRegistry)
    for _, t := range r.ast.Types {
        registry[t.Name] = ResolvedCustomType{
            Name:   t.Name,
            Fields: t.Fields,  // raw fields, resolved in step 3
        }
    }
    return registry
}
```

**Why first:** Steps 3 (tables), 4 (externals), 10 (DTOs) all need to look up custom types by name. If TypeRegistry isn't built first, those steps require O(n) scans.

---

### Step 2: Resolve Lang/DB/Framework

Convert string enums to typed constants. Done once, used everywhere.

```go
func (r *Resolver) resolveLang() Lang {
    switch r.ast.Lang {
    case "go":   return LangGo
    case "java": return LangJava
    default:     panic("validator should have caught unknown lang")
    }
}

func (r *Resolver) resolveDB() DBDriver {
    switch r.ast.DB {
    case "postgres": return DBPostgres
    case "mysql":    return DBMySQL
    case "mongo":    return DBMongo
    default:         panic("validator should have caught unknown db")
    }
}
```

These become `r.resolved.Lang`, `r.resolved.DB`, `r.resolved.Framework`.

---

### Step 3: Resolve Tables

This is the most complex step. For each table, the Resolver must:

**3a. Resolve field types**

Each field's `Type` string gets resolved to fully-typed structs.

```go
// typemap.go handles this — DO NOT inline type logic in resolver
func resolveFieldType(typeName string, lang Lang, db DBDriver) ResolvedFieldType {
    switch typeName {
    case "str":
        return ResolvedFieldType{
            Kind:   TypeStr,
            GoType: "string",
            DBType: postgresDBType(typeName, db),  // "VARCHAR(255)"
        }
    case "int":
        return ResolvedFieldType{Kind: TypeInt, GoType: "int64", DBType: "BIGINT"}
    case "decimal":
        return ResolvedFieldType{Kind: TypeDecimal, GoType: "decimal.Decimal", DBType: "NUMERIC"}
    case "bool":
        return ResolvedFieldType{Kind: TypeBool, GoType: "bool", DBType: "BOOLEAN"}
    case "timestamp":
        return ResolvedFieldType{Kind: TypeTimestamp, GoType: "time.Time", DBType: "TIMESTAMP"}
    case "date":
        return ResolvedFieldType{Kind: TypeDate, GoType: "time.Time", DBType: "DATE"}
    case "uuid":
        return ResolvedFieldType{Kind: TypeUUID, GoType: "uuid.UUID", DBType: "UUID"}
    case "json":
        return ResolvedFieldType{Kind: TypeJSON, GoType: "json.RawMessage", DBType: "JSONB"}
    case "enum":
        return ResolvedFieldType{Kind: TypeEnum, GoType: "string", DBType: "TEXT"}
    default:
        // custom type — already validated that it exists
        return ResolvedFieldType{Kind: TypeCustom, GoType: "*" + typeName, DBType: "JSONB"}
    }
}
```

**Note:** `typemap.go` already exists in the project. The Resolver must call it, not duplicate its logic.

**3b. Derive indexes from queries**

```go
// query: find_by: [email] → creates an index on (email)
// query: find_by: [status, created_at] → creates a composite index on (status, created_at)
func deriveIndexesFromQueries(queries []QueryAST) []ResolvedIndex {
    indexes := []ResolvedIndex{}
    for _, q := range queries {
        if q.FindBy != nil {
            indexes = append(indexes, ResolvedIndex{
                Fields: q.FindBy,
                Unique: false,
            })
        }
    }
    return indexes
}
```

**3c. Expand soft_delete**

If `soft_delete: true`, inject `deleted_at *time.Time` into fields and ensure all generated queries get `WHERE deleted_at IS NULL`.

```go
if table.SoftDelete {
    resolvedTable.Fields = append(resolvedTable.Fields, ResolvedField{
        Name:     "deleted_at",
        Type:     ResolvedFieldType{Kind: TypeTimestamp, GoType: "*time.Time", Nullable: true},
        Nullable: true,
    })
    // flag so repo generator adds WHERE deleted_at IS NULL everywhere
    resolvedTable.SoftDelete = true
}
```

**3d. Expand timestamps**

If `timestamps: true` (or default), inject `created_at` and `updated_at`.

**3e. Wire relations**

For each field that is a FK, resolve the referenced table.

```go
for _, rel := range table.Relations {
    // rel.Table is guaranteed to exist — validator checked this
    referenced := r.resolved.Tables[indexByName(rel.Table)]
    resolvedTable.Relations = append(resolvedTable.Relations, ResolvedRelation{
        Field:      rel.Field,
        TableRef:   &referenced,
        ForeignKey: rel.ForeignKey,
    })
}
```

---

### Step 4: Resolve Externals

For each external, fill in the typed request/response structs for each method.

```go
func (r *Resolver) resolveExternal(ext ExternalAST) ResolvedExternal {
    resolved := ResolvedExternal{
        Name:    ext.Name,
        BaseURL: ext.BaseURL,  // still a config var reference like "${STRIPE_URL}"
        Timeout: ext.Timeout,
        Retry: ResolvedRetryPolicy{
            Attempts: ext.Attempts,
            Backoff:  ext.Backoff,
        },
    }
    for _, method := range ext.Methods {
        resolvedMethod := ResolvedExternalMethod{
            Name:     method.Name,
            HTTP:     method.Method,
            Path:     method.Path,
        }
        if method.Body != "" {
            // look up DTO in types or inline DTOs
            resolvedMethod.RequestType = r.lookupDTO(method.Body)
        }
        if method.Response != "" && method.Response != "void" {
            resolvedMethod.ResponseType = r.lookupDTO(method.Response)
        }
        // status → error mappings
        for _, s := range method.OnStatus {
            resolvedMethod.StatusErrors = append(resolvedMethod.StatusErrors, ResolvedStatusError{
                Status: s.Status,
                Error:  s.Error,
            })
        }
        resolved.Methods = append(resolved.Methods, resolvedMethod)
    }
    return resolved
}
```

**Mock interface:** Also plan the mock for testing. The generator (`go.external`) creates both a real client and a mock client. The Resolver records that both need generating.

---

### Step 5: Resolve Cache

For each cache interface, fill in the typed method signatures.

```go
func (r *Resolver) resolveCache(cache CacheAST) *ResolvedCache {
    if cache == nil { return nil }
    resolved := &ResolvedCache{
        Provider: cache.Provider,  // "redis"
        URL:      cache.URL,       // "${REDIS_URL}"
        Prefix:   cache.Prefix,
    }
    for _, iface := range cache.Interfaces {
        // value_type must be a known DTO or custom type — validator confirmed this
        valueType := r.lookupDTO(iface.ValueType)
        resolvedIface := ResolvedCacheInterface{
            Name:        iface.Name,            // "UserCache"
            KeyTemplate: iface.KeyTemplate,     // "user:{id}"
            ValueType:   valueType,             // *ResolvedDTO or *ResolvedCustomType
            DefaultTTL:  parseDuration(iface.DefaultTTL),
            Methods:     iface.Methods,         // ["Get", "Set", "Delete", "Invalidate"]
        }
        resolved.Interfaces = append(resolved.Interfaces, resolvedIface)
    }
    return resolved
}
```

The `value_type` determines the Go type for generated `Get`/`Set` signatures. For example, `value_type: UserResponse` → `Get(ctx, id int64) (*UserResponse, bool, error)`.

---

### Step 6: Resolve Transactions

For each transaction, plan the typed params struct and result type.

```go
func (r *Resolver) resolveTransaction(tx TransactionAST) ResolvedTransaction {
    resolved := ResolvedTransaction{
        Name: tx.Name,  // "PlaceOrder"
        Type: tx.Type,  // "local"
    }
    for i, step := range tx.Steps {
        resolvedStep := ResolvedTransactionStep{
            Name:  step.Name,
            SQL:   step.SQL,
        }
        // resolve params
        for _, param := range step.Params {
            resolvedStep.Params = append(resolvedStep.Params, ResolvedParam{
                Name:   param.Name,
                GoType: resolveFieldType(param.Type, r.resolved.Lang, r.resolved.DB).GoType,
            })
        }
        // error condition
        if step.ErrorIfRows != nil {
            resolvedStep.ErrorIfRows = step.ErrorIfRows
            resolvedStep.ErrorName = step.Error  // "OrderCreationFailed"
        }
        resolved.Steps = append(resolved.Steps, resolvedStep)
    }
    return resolved
}
```

---

### Step 7: Resolve Touches

This step is critical. For each touch in each API, the Resolver must:
1. Determine which kind of touch it is
2. Resolve the reference to the actual infra item
3. Compute the flag name
4. Record the default

```go
func (r *Resolver) resolveTouch(touch TouchAST) ResolvedTouch {
    resolved := ResolvedTouch{
        Default: touch.Default,
    }

    switch {
    case touch.Table != "":
        resolved.Kind = TouchKindTable
        resolved.Name = touch.Table
        resolved.Op   = touch.Op
        // look up resolved table
        tableRef := r.tableByName(touch.Table)
        resolved.TableRef = tableRef
        // compute flag name
        if touch.Flag != "" {
            resolved.Flag = touch.Flag  // developer override
        } else {
            // default: Run{Table}{PascalCase(Op)}
            // e.g. touch.Table="users", touch.Op="create" → "RunTableUsersCreate"
            resolved.Flag = "Run" + "Table" + pascal(touch.Table) + pascal(touch.Op)
        }

    case touch.External != "":
        resolved.Kind = TouchKindExternal
        resolved.Name = touch.External
        resolved.Op   = touch.Method
        extRef := r.externalByName(touch.External)
        resolved.ExternalRef = extRef
        // e.g. "RunCRMClientCreateContact"
        resolved.Flag = "Run" + pascal(touch.External) + pascal(touch.Method)

    case touch.Cache != "":
        resolved.Kind = TouchKindCache
        resolved.Name = touch.Cache
        resolved.Op   = touch.Op
        cacheRef := r.cacheInterfaceByName(touch.Cache)
        resolved.CacheRef = cacheRef
        // e.g. "RunUserCacheGet"
        resolved.Flag = "Run" + pascal(touch.Cache) + pascal(touch.Op)

    case touch.Transaction != "":
        resolved.Kind = TouchKindTransaction
        resolved.Name = touch.Transaction
        txRef := r.transactionByName(touch.Transaction)
        resolved.TransactionRef = txRef
        // e.g. "RunPlaceOrderTx"
        resolved.Flag = "Run" + pascal(touch.Transaction) + "Tx"

    case touch.Message != "":
        resolved.Kind = TouchKindMessage
        resolved.Name = touch.Message
        resolved.Op   = "Publish"
        // e.g. "RunUserCreatedPublish"
        resolved.Flag = "Run" + pascal(touch.Message) + "Publish"
    }

    return resolved
}
```

**Flag naming convention:** `Run{Kind}{Name}{Op}` in PascalCase. The validator already confirmed that no two touches in the same API share a flag name.

---

### Step 8: Plan Shared Context

This is the step that generates everything generators need for `context.go`.

The shared context has fields in this exact order:
1. `Request` field — the incoming request DTO
2. One flag field per touch
3. One result field per touch  
4. One error field per touch
5. `Response` field — the response DTO

```go
func (r *Resolver) planSharedContext(api APIAST, touches []ResolvedTouch) ResolvedSharedContext {
    ctx := ResolvedSharedContext{
        TypeName: api.Name + "Context",  // "CreateUserContext"
    }

    // 1. Request field
    if api.Request != "" {
        ctx.Fields = append(ctx.Fields, ContextField{
            Name:   "Request",
            GoType: "*" + api.Request,  // "*CreateUserRequest"
            Kind:   ContextFieldKindRequest,
        })
    }

    // 2. Flag fields (one per touch)
    for _, touch := range touches {
        ctx.Fields = append(ctx.Fields, ContextField{
            Name:    touch.Flag,    // "RunTableUsersCreate"
            GoType:  "bool",
            Kind:    ContextFieldKindFlag,
            Default: touch.Default, // true or false
        })
    }

    // 3 & 4. Result and error fields (one pair per touch)
    for _, touch := range touches {
        // result field
        resultName, resultType := r.touchResultField(touch)
        ctx.Fields = append(ctx.Fields, ContextField{
            Name:   resultName,   // "TableUsersResult"
            GoType: resultType,   // "*tables.User"
            Kind:   ContextFieldKindResult,
        })
        // error field
        errorName := r.touchErrorField(touch)
        ctx.Fields = append(ctx.Fields, ContextField{
            Name:   errorName,   // "TableUsersError"
            GoType: "error",
            Kind:   ContextFieldKindError,
        })
    }

    // 5. Response field
    if api.Response != "" {
        ctx.Fields = append(ctx.Fields, ContextField{
            Name:   "Response",
            GoType: "*" + api.Response,  // "*CreateUserResponse"
            Kind:   ContextFieldKindResponse,
        })
    }

    return ctx
}

func (r *Resolver) touchResultField(touch ResolvedTouch) (name string, goType string) {
    switch touch.Kind {
    case TouchKindTable:
        // TableUsersResult → *tables.User (or *User, depending on generated package)
        modelName := pascal(singularize(touch.Name))  // "users" → "User"
        return "Table" + pascal(touch.Name) + "Result", "*" + modelName
    case TouchKindExternal:
        method := touch.ExternalRef.MethodByName(touch.Op)
        if method.ResponseType == "" || method.ResponseType == "void" {
            return pascal(touch.Name) + pascal(touch.Op) + "Result", "interface{}"
        }
        return pascal(touch.Name) + pascal(touch.Op) + "Result", "*" + method.ResponseType.GoType
    case TouchKindCache:
        return pascal(touch.Name) + pascal(touch.Op) + "Result", touch.CacheRef.ValueType.GoType
    case TouchKindTransaction:
        return pascal(touch.Name) + "Result", touch.TransactionRef.ResultType
    case TouchKindMessage:
        // messages only produce an error, no result
        return pascal(touch.Name) + "PublishResult", "struct{}"
    }
    return "UnknownResult", "interface{}"
}

func (r *Resolver) touchErrorField(touch ResolvedTouch) string {
    switch touch.Kind {
    case TouchKindTable:
        return "Table" + pascal(touch.Name) + "Error"
    case TouchKindExternal:
        return pascal(touch.Name) + pascal(touch.Op) + "Error"
    case TouchKindCache:
        return pascal(touch.Name) + pascal(touch.Op) + "Error"
    case TouchKindTransaction:
        return pascal(touch.Name) + "Error"
    case TouchKindMessage:
        return pascal(touch.Name) + "PublishError"
    }
    return "UnknownError"
}
```

---

### Step 9: Plan Hook Interface

This step generates everything generators need for `hooks.go`.

```go
func (r *Resolver) planHookInterface(api APIAST, touches []ResolvedTouch) ResolvedHookInterface {
    ctxType := api.Name + "Context"  // "CreateUserContext"
    hooks := ResolvedHookInterface{
        TypeName: api.Name + "Hooks",  // "CreateUserHooks"
    }

    // Entry hook — runs before everything
    hooks.Hooks = append(hooks.Hooks, ResolvedHook{
        Name:      "Before" + api.Name,
        Signature: fmt.Sprintf("func(ctx context.Context, shared *%s) error", ctxType),
        When:      HookBefore,
        TouchIdx:  -1,
    })

    // One Before + one After per touch
    for i, touch := range touches {
        touchDesc := touchDescription(touch)  // e.g. "table users create"
        
        before := ResolvedHook{
            Name:     "Before" + hookName(touch),
            Signature: fmt.Sprintf("func(ctx context.Context, shared *%s) error", ctxType),
            When:     HookBefore,
            TouchIdx: i,
        }
        after := ResolvedHook{
            Name:     "After" + hookName(touch),
            Signature: fmt.Sprintf("func(ctx context.Context, shared *%s) error", ctxType),
            When:     HookAfter,
            TouchIdx: i,
        }
        hooks.Hooks = append(hooks.Hooks, before, after)
    }

    // Exit hook — runs before response is set
    hooks.Hooks = append(hooks.Hooks, ResolvedHook{
        Name:      "BeforeResponse",
        Signature: fmt.Sprintf("func(ctx context.Context, shared *%s) error", ctxType),
        When:      HookBefore,
        TouchIdx:  -1,
    })

    return hooks
}

// hookName derives the hook suffix from a touch
// Table touch: "users", op "create" → "TableUsersCreate"
// External touch: "CRMClient", method "CreateContact" → "CRMClientCreateContact"
func hookName(touch ResolvedTouch) string {
    switch touch.Kind {
    case TouchKindTable:
        return "Table" + pascal(touch.Name) + pascal(touch.Op)
    case TouchKindExternal:
        return pascal(touch.Name) + pascal(touch.Op)
    case TouchKindCache:
        return pascal(touch.Name) + pascal(touch.Op)
    case TouchKindTransaction:
        return pascal(touch.Name) + "Tx"
    case TouchKindMessage:
        return pascal(touch.Name) + "Publish"
    }
    return "Unknown"
}
```

---

### Step 10: Resolve DTOs

Inline DTOs declared under each API's `dtos:` block must be expanded. External DTO references must be validated (validator already did this, but resolution fills in types).

```go
func (r *Resolver) resolveDTOs(api APIAST) (request *ResolvedDTO, response *ResolvedDTO) {
    if api.Request != "" && api.DTOs != nil {
        dto := api.DTOs.Find(api.Request)
        if dto != nil {
            request = r.expandDTO(dto)
        }
    }
    if api.Response != "" && api.DTOs != nil {
        dto := api.DTOs.Find(api.Response)
        if dto != nil {
            response = r.expandDTO(dto)
        }
    }
    return request, response
}

func (r *Resolver) expandDTO(dto *DTODecl) *ResolvedDTO {
    resolved := &ResolvedDTO{Name: dto.Name}
    for _, field := range dto.Fields {
        resolved.Fields = append(resolved.Fields, ResolvedDTOField{
            Name:     field.Name,
            Type:     resolveFieldType(field.Type, r.resolved.Lang, r.resolved.DB),
            Required: field.Required,
            Private:  field.Private,
            Compute:  field.Compute,
        })
    }
    return resolved
}
```

**Mappers:** For each API, plan which table model fields map to which DTO fields. The generator uses this to produce `mapper.go`.

```go
func (r *Resolver) planMappers(api ResolvedAPI) []ResolvedMapper {
    // figure out which table the response DTO maps from
    // usually the first table touch
    var primaryTable *ResolvedTable
    for _, touch := range api.Touches {
        if touch.Kind == TouchKindTable {
            primaryTable = touch.TableRef
            break
        }
    }
    if primaryTable == nil || api.ResponseDTO == nil {
        return nil
    }
    mapper := ResolvedMapper{
        FromType: primaryTable.Name,
        ToType:   api.ResponseDTO.Name,
    }
    for _, dtoField := range api.ResponseDTO.Fields {
        if !dtoField.Private && !dtoField.Compute {
            // find matching table field by name
            for _, tableField := range primaryTable.Fields {
                if tableField.Name == dtoField.Name && !tableField.Private {
                    mapper.Fields = append(mapper.Fields, ResolvedMapperField{
                        From: tableField.Name,
                        To:   dtoField.Name,
                    })
                    break
                }
            }
        }
    }
    return []ResolvedMapper{mapper}
}
```

---

### Step 11: Derive DI Graph

For each API service, determine what constructor params it needs. The generator uses this to build `NewCreateUserService(repo *UsersRepository, cache *UserCache, hooks *CreateUserHooks) *CreateUserService`.

```go
func (r *Resolver) deriveDIGraph(api ResolvedAPI) ResolvedDIGraph {
    graph := ResolvedDIGraph{}
    for _, touch := range api.Touches {
        switch touch.Kind {
        case TouchKindTable:
            graph.Repos = append(graph.Repos, touch.TableRef.Name + "Repository")
        case TouchKindExternal:
            graph.Externals = append(graph.Externals, touch.ExternalRef.Name)
        case TouchKindCache:
            graph.Caches = append(graph.Caches, touch.CacheRef.Name)
        case TouchKindTransaction:
            graph.Transactions = append(graph.Transactions, touch.TransactionRef.Name)
        case TouchKindMessage:
            graph.Producers = append(graph.Producers, "Producer")
        }
    }
    graph.HooksType = api.Name + "Hooks"
    return graph
}
```

---

### Step 12: Validate Import DAG

After all resolution, check that the generated package imports form a valid DAG (no cycles). The import hierarchy from the Tech Spec:

```
generated/types/          → stdlib only
generated/tables/{t}/     → types/
generated/cache/          → types/
generated/externals/      → types/
generated/apis/{api}/dto  → types/
generated/apis/{api}/ctx  → dto, types/
generated/apis/{api}/hooks → context
generated/apis/{api}/mapper → model, dto
generated/apis/{api}/service → repo, mapper, hooks, ctx, cache, externals, messaging, tx
generated/apis/{api}/handler → service, dto
generated/wire.go         → all services
```

```go
func (r *Resolver) validateImportDAG() error {
    dag := buildImportDAG(r.resolved)
    cycle := dag.FindCycle()
    if cycle != nil {
        return fmt.Errorf("import cycle detected: %s", cycle)
    }
    return nil
}
```

---

## 6. Complete Resolver Struct

```go
// internal/spec/resolver.go

type Resolver struct {
    ast      *SpecAST
    resolved *ResolvedSpec
}

func NewResolver(ast *SpecAST) *Resolver {
    return &Resolver{ast: ast}
}

func (r *Resolver) Resolve() (*ResolvedSpec, error) {
    r.resolved = &ResolvedSpec{}

    // Step 1
    r.resolved.Types = r.buildTypeRegistry()

    // Step 2
    r.resolved.Lang      = r.resolveLang()
    r.resolved.DB        = r.resolveDB()
    r.resolved.Framework = r.resolveFramework()
    r.resolved.Project   = r.ast.Project
    r.resolved.Module    = r.deriveModule()
    r.resolved.Config    = r.resolveConfig()

    // Step 3
    for _, table := range r.ast.Tables {
        r.resolved.Tables = append(r.resolved.Tables, r.resolveTable(table))
    }

    // Step 4
    for _, ext := range r.ast.Externals {
        r.resolved.Externals = append(r.resolved.Externals, r.resolveExternal(ext))
    }

    // Step 5
    r.resolved.Cache = r.resolveCache(r.ast.Cache)

    // Step 6
    for _, tx := range r.ast.Transactions {
        r.resolved.Transactions = append(r.resolved.Transactions, r.resolveTransaction(tx))
    }

    // Step 7 + 8 + 9 + 10 + 11 — resolve all resource groups
    for _, group := range r.ast.Resources {
        r.resolved.Resources = append(r.resolved.Resources, r.resolveResourceGroup(group))
    }

    // Step 12
    if err := r.validateImportDAG(); err != nil {
        return nil, err
    }

    return r.resolved, nil
}

func (r *Resolver) resolveResourceGroup(group ResourceGroupAST) ResolvedResourceGroup {
    resolved := ResolvedResourceGroup{
        Group:    group.Group,
        BasePath: group.BasePath,
        Auth:     r.resolveAuth(group.Auth),
    }
    for _, api := range group.APIs {
        resolved.APIs = append(resolved.APIs, r.resolveAPI(api, group))
    }
    return resolved
}

func (r *Resolver) resolveAPI(api APIAST, group ResourceGroupAST) ResolvedAPI {
    resolved := ResolvedAPI{
        Name:     api.Name,
        Method:   api.Method,
        FullPath: group.BasePath + api.Path,
        Auth:     r.resolveAPIAuth(api, group),
    }

    // Step 7: resolve touches
    for _, touch := range api.Touches {
        resolved.Touches = append(resolved.Touches, r.resolveTouch(touch))
    }

    // Step 8: plan shared context
    resolved.SharedContext = r.planSharedContext(api, resolved.Touches)

    // Step 9: plan hook interface
    resolved.HookInterface = r.planHookInterface(api, resolved.Touches)

    // Step 10: resolve DTOs
    resolved.RequestDTO, resolved.ResponseDTO = r.resolveDTOs(api)
    resolved.Mappers = r.planMappers(resolved)

    // Step 11: derive DI graph
    resolved.DI = r.deriveDIGraph(resolved)

    return resolved
}
```

---

## 7. What Each Generator Consumes

The Tech Spec defines exactly what each generator reads from `ResolvedAPI`. This table is the contract:

| Generator | Reads from ResolvedAPI |
|-----------|------------------------|
| `go.api.dto` | `RequestDTO`, `ResponseDTO` (compute fields) |
| `go.api.context` | `SharedContext.Fields` — all flags, results, errors, request, response |
| `go.api.hooks` | `HookInterface.Hooks` — all hook names and signatures |
| `go.api.mapper` | `Mappers` — which model fields map to which DTO fields |
| `go.api.service` | `Touches` (ordered), `SharedContext`, `HookInterface` |
| `go.api.handler` | `Method`, `FullPath`, `RequestDTO`, `ResponseDTO`, `Auth` |

The service generator is most important. It uses `Touches` in order to render one `ServiceStep` per touch:

```go
type ServiceStep struct {
    Flag        string         // "RunTableUsersCreate"
    InfraCall   string         // pre-rendered infra call
    ResultField string         // "TableUsersResult"
    ErrorField  string         // "TableUsersError"
    BeforeHook  *HookCallSite
    AfterHook   *HookCallSite
    FatalError  bool
}
```

The `InfraCall` field is **pre-rendered code**. The Resolver (or a service-step builder) must render this string — the template never decides how to call infra. Examples:

```go
// Table create
"shared.TableUsersResult, shared.TableUsersError = r.usersRepo.CreateUser(ctx, shared.Request)"

// External call
"shared.CRMClientCreateResult, shared.CRMClientCreateError = r.crmClient.CreateContact(ctx, crmReq)"

// Cache get
"shared.UserCacheGetResult, _, shared.UserCacheGetError = r.userCache.Get(ctx, shared.Request.ID)"

// Transaction
"shared.PlaceOrderResult, shared.PlaceOrderError = r.placeOrderTx.Execute(ctx, txParams)"

// Message publish
"shared.UserCreatedPublishError = r.producer.PublishUserCreated(ctx, evt)"
```

---

## 8. Key Naming Conventions

These are derived from the Tech Spec examples and must be applied consistently:

| Thing | Convention | Example |
|-------|-----------|---------|
| Context type | `{APIName}Context` | `CreateUserContext` |
| Hooks type | `{APIName}Hooks` | `CreateUserHooks` |
| Flag field | `Run{Kind}{Name}{Op}` | `RunTableUsersCreate`, `RunCRMClientCreateContact` |
| Result field | `{Kind}{Name}{Op}Result` | `TableUsersResult`, `CRMClientCreateContactResult` |
| Error field | `{Kind}{Name}{Op}Error` | `TableUsersError`, `CRMClientCreateContactError` |
| Entry hook | `Before{APIName}` | `BeforeCreateUser` |
| Touch hook | `Before{hookName}`, `After{hookName}` | `BeforeTableUsersCreate`, `AfterCRMClientCreateContact` |
| Exit hook | `BeforeResponse` | `BeforeResponse` |

Where `{Kind}` = `Table` | `Cache` | `Tx` (for transactions) | empty (for externals and messages).

---

## 9. Testing the Resolver

From the Tech Spec, the unit test pattern for the Resolver:

```go
func TestResolver_SharedContextFromTouches(t *testing.T) {
    cases := []struct{
        name        string
        touches     []TouchAST
        wantFlags   []ContextField
        wantResults []ContextField
        wantErrors  []ContextField
    }{
        {
            name: "table create touch",
            touches: []TouchAST{
                {Table: "users", Op: "create", Default: true},
            },
            wantFlags: []ContextField{
                {Name: "RunTableUsersCreate", GoType: "bool", Kind: ContextFieldKindFlag, Default: true},
            },
            wantResults: []ContextField{
                {Name: "TableUsersResult", GoType: "*User", Kind: ContextFieldKindResult},
            },
            wantErrors: []ContextField{
                {Name: "TableUsersError", GoType: "error", Kind: ContextFieldKindError},
            },
        },
        {
            name: "external touch default false",
            touches: []TouchAST{
                {External: "CRMClient", Method: "CreateContact", Default: false},
            },
            wantFlags: []ContextField{
                {Name: "RunCRMClientCreateContact", GoType: "bool", Kind: ContextFieldKindFlag, Default: false},
            },
            wantResults: []ContextField{
                {Name: "CRMClientCreateContactResult", GoType: "*ContactResponse", Kind: ContextFieldKindResult},
            },
            wantErrors: []ContextField{
                {Name: "CRMClientCreateContactError", GoType: "error", Kind: ContextFieldKindError},
            },
        },
        {
            name: "cache get touch",
            touches: []TouchAST{
                {Cache: "UserCache", Op: "get", Default: true},
            },
            wantFlags: []ContextField{
                {Name: "RunUserCacheGet", GoType: "bool", Kind: ContextFieldKindFlag, Default: true},
            },
        },
        {
            name: "transaction touch",
            touches: []TouchAST{
                {Transaction: "PlaceOrder", Default: true},
            },
            wantFlags: []ContextField{
                {Name: "RunPlaceOrderTx", GoType: "bool", Kind: ContextFieldKindFlag, Default: true},
            },
        },
        {
            name: "multi-touch api",
            touches: []TouchAST{
                {Table: "users", Op: "create", Default: true},
                {External: "CRMClient", Method: "CreateContact", Default: false},
            },
            wantFlags: []ContextField{
                {Name: "RunTableUsersCreate", Default: true},
                {Name: "RunCRMClientCreateContact", Default: false},
            },
            wantResults: []ContextField{
                {Name: "TableUsersResult", GoType: "*User"},
                {Name: "CRMClientCreateContactResult", GoType: "*ContactResponse"},
            },
        },
    }

    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            r := buildTestResolver(tc.touches) // helper builds minimal ResolvedSpec
            api := APIAST{Name: "CreateUser", Touches: tc.touches}
            resolvedTouches := resolveTouches(tc.touches)
            ctx := r.planSharedContext(api, resolvedTouches)

            flagFields := filterFields(ctx.Fields, ContextFieldKindFlag)
            resultFields := filterFields(ctx.Fields, ContextFieldKindResult)
            errorFields := filterFields(ctx.Fields, ContextFieldKindError)

            assert.Equal(t, tc.wantFlags, flagFields)
            assert.Equal(t, tc.wantResults, resultFields)
            assert.Equal(t, tc.wantErrors, errorFields)
        })
    }
}
```

**Golden file tests** — for full API resolution, use golden files under `testdata/golden/`:

```
testdata/golden/resolver/
├── createuser_basic/
│   ├── spec.yaml
│   └── resolved.json.golden    ← full ResolvedAPI serialized to JSON
├── createuser_with_conditional_external/
│   ├── spec.yaml
│   └── resolved.json.golden
├── checkout_multi_touch/
│   ├── spec.yaml
│   └── resolved.json.golden
└── getuser_cache_readthrough/
    ├── spec.yaml
    └── resolved.json.golden
```

Run with `-update` flag to regenerate, review diff as code review.

---

## 10. Common Mistakes to Avoid

**1. Ordering matters.** Step 3 (tables) must come before Step 7 (touches), because touch resolution needs `r.tableByName()` to work. Never reorder steps.

**2. The Resolver does not validate.** If a touch references a table that doesn't exist, that's a validator bug, not a resolver bug. The Resolver panics or returns an internal error — it never returns user-facing validation errors.

**3. `typemap.go` is the single source of truth for type mappings.** Never hardcode `"string"` or `"int64"` in the resolver. Call the type map function.

**4. Flag names are derived, not configurable.** The YAML `flag:` field is an override, not the default. Default is always `Run{Kind}{Name}{Op}`.

**5. `InfraCall` is a pre-rendered string.** The service generator template has zero logic. If the resolver doesn't pre-render the infra call string, the service generator cannot produce correct code. See Section 7 for examples.

**6. Every hook has a defined shape.** Before hooks and after hooks both take `(ctx context.Context, shared *{API}Context) error`. The Resolver must pre-compute the full signature string — generators do not compute signatures.

**7. Multi-touch flag ordering.** Flags in `SharedContext` must appear in the same order as their corresponding touches in the `Touches` slice. The service generator iterates `Steps` in order, and `Steps` is built from `Touches` in order.

---

## 11. File to Create

One file: `internal/spec/resolver.go`

The resolver uses `typemap.go` (already exists) and produces all resolved structs defined in `resolved.go` (already exists). No new files are needed unless the resolver gets large enough to split, in which case:

```
internal/spec/
├── resolver.go           ← main Resolve() function and struct
├── resolver_tables.go    ← resolveTable() and helpers
├── resolver_apis.go      ← resolveAPI(), planSharedContext(), planHookInterface()
├── resolver_infra.go     ← resolveExternal(), resolveCache(), resolveTransaction()
└── resolver_test.go      ← all unit tests
```

