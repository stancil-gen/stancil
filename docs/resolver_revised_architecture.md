# Stencil Resolver — Revised Architecture
## Three Levels + Import Resolution

---

## What Changed From Previous Doc

Two structural corrections before the full design:

**1. No language-specific import fields on structs.** `GoImports []string` and `JavaImports []string` as hardcoded struct fields breaks every time a new language is added. Instead, imports are resolved by a shared `ImportResolver` component that runs after each level completes. The resolved structs carry only the raw data (field types, TypeDescriptors). The ImportResolver reads those and emits the right imports for whichever language the generator targets.

**2. Level 3 constructor is derived, not declared.** The `Constructor ResolvedFunction` field is removed. The constructor is mechanically derivable from `Dependencies []ResolvedDependency` at generation time — there's no need to pre-compute it in the resolver.

---

## The Six Steps

```
Level 1:   Build all ResolvedObjects
Level 1A:  ImportResolver pass over all ResolvedObjects

Level 2:   Build all ResolvedInterfaces
Level 2A:  ImportResolver pass over all ResolvedInterfaces

Level 3:   Build all ResolvedImplementations + DI graph
Level 3A:  ImportResolver pass over all ResolvedImplementations
```

Each "A" step is the same ImportResolver running on different input. The resolver itself is shared code.

---

## The ImportResolver

### Why it's separate

Imports are a language-target concern. A `ResolvedField` with `TypeDescriptor{Kind: TypeDecimal}` means `import "github.com/shopspring/decimal"` in Go and `import java.math.BigDecimal` in Java. The struct doesn't encode either — it just holds the canonical `TypeKind`. The ImportResolver converts that to the right import for the active language.

### Design

```go
// internal/spec/resolver/imports/import_resolver.go

// ImportSet is what gets attached to resolved structs after an ImportResolver pass.
// It is language-specific: one ImportSet per target language.
type ImportSet struct {
    Lang    Lang
    Paths   []string   // sorted, deduplicated import paths for this language
}

// ImportResolver is stateless. Call it after each resolver level completes.
type ImportResolver struct {
    lang   Lang
    module string   // Go module path, e.g. "github.com/acme/orders-service"
}

func NewImportResolver(lang Lang, module string) *ImportResolver

// ForObject collects all imports needed to declare a ResolvedObject in the target language.
// Walks all fields, looks at each TypeDescriptor, collects language-specific import paths.
func (r *ImportResolver) ForObject(obj *ResolvedObject) ImportSet

// ForInterface collects all imports needed to declare a ResolvedInterface.
// Walks all function params and return types.
func (r *ImportResolver) ForInterface(iface *ResolvedInterface) ImportSet

// ForImplementation collects all imports needed to write a ResolvedImplementation.
// Walks dependencies and function bodies.
func (r *ImportResolver) ForImplementation(impl *ResolvedImplementation) ImportSet
```

### What it does internally

For each `TypeDescriptor` it encounters, it maps `Kind → import path` for the active language:

```go
func (r *ImportResolver) importForType(t TypeDescriptor) string {
    if t.IsCustom {
        // custom type lives in the generated module
        return r.module + "/generated/types"
    }
    switch r.lang {
    case LangGo:
        switch t.Kind {
        case TypeDecimal:   return "github.com/shopspring/decimal"
        case TypeUUID:      return "github.com/google/uuid"
        case TypeTimestamp: return "time"
        case TypeDate:      return "time"
        case TypeJSON:      return "encoding/json"
        default:            return ""  // primitives need no import
        }
    case LangJava:
        switch t.Kind {
        case TypeDecimal:   return "java.math.BigDecimal"
        case TypeUUID:      return "java.util.UUID"
        case TypeTimestamp: return "java.time.Instant"
        case TypeDate:      return "java.time.LocalDate"
        default:            return ""
        }
    }
    return ""
}
```

When a new language is added, you add a `case LangRust:` block here. Nothing else changes.

### Where ImportSets live on structs

After each A-step, the ImportResolver attaches an `ImportSet` to each resolved struct:

```go
type ResolvedObject struct {
    // ... all fields ...
    Imports map[Lang]ImportSet   // populated by ImportResolver in Level 1A
}

type ResolvedInterface struct {
    // ... all fields ...
    Imports map[Lang]ImportSet   // populated by ImportResolver in Level 2A
}

type ResolvedImplementation struct {
    // ... all fields ...
    Imports map[Lang]ImportSet   // populated by ImportResolver in Level 3A
}
```

The generator reads `obj.Imports[LangGo].Paths` and renders the import block. It never computes imports itself.

---

## Level 1: ResolvedObject

### AST → ResolvedObject mapping

Every `ResolvedObject` has a source AST node. This table is authoritative — it defines exactly what the resolver produces from each AST type.

| Source AST | Kind | Object Name | Generated Path |
|---|---|---|---|
| `TypeAST` | `TypeObject` | `{TypeAST.Name}` | `generated/types/types.go` (all types share one file) |
| `TableAST` | `TableModel` | `{pascal(TableAST.Name)}` e.g. `User` | `generated/repo/{table_name}/model.go` |
| `APIAST.Request` | `RequestDTO` | `{APIAST.Name}Request` | `generated/handler/{api_name}/types.go` |
| `APIAST.Response` | `ResponseDTO` | `{APIAST.Name}Response` | `generated/handler/{api_name}/types.go` |
| `APIAST` (shared ctx) | `SharedContext` | `{APIAST.Name}Context` | `generated/handler/{api_name}/types.go` |
| `ExternalAST.Method.Body` | `ExternalInput` | `{Method.Body}` | `generated/external/{external_name}/types.go` |
| `ExternalAST.Method.Response` | `ExternalOutput` | `{Method.Response}` | `generated/external/{external_name}/types.go` |
| `TransactionAST.Step.Params` | `TransactionParams` | `{Tx.Name}{Step.Name}Params` | `generated/tx/{tx_name}/types.go` |

**Key rules:**
- All types from `types:` block go into one shared file — they are small value objects and don't warrant individual files.
- Table models each get their own directory under `generated/repo/` because the repo, model, and errors all live together.
- All per-API types (request, response, shared context) live in one `types.go` inside the handler package.
- External types live in the external package they belong to.

### Struct

```go
// internal/spec/resolved.go

type ResolvedObject struct {
    // Identity
    Name string      // PascalCase: "Money", "User", "CreateUserRequest", "CreateUserContext"
    Path string      // file path this object is generated into (see table above)
    Kind ObjectKind

    // Fields in declaration order
    Fields []ResolvedField

    // Validation rules per field name
    // Key: field Name (PascalCase). Value: ordered list of rules.
    Rules map[string][]ResolvedRule

    // DB metadata — only populated when Kind == TableModel
    TableName  string          // snake_case: "users"
    PrimaryKey string          // field Name of the PK: "ID"
    SoftDelete bool
    Indexes    []ResolvedIndex

    // Populated by ImportResolver in Level 1A
    Imports map[Lang]ImportSet
}

type ObjectKind int

const (
    TypeObject     ObjectKind = iota // from types: block
    TableModel                        // from tables: block
    RequestDTO                        // from api.request
    ResponseDTO                       // from api.response
    SharedContext                     // per-API execution context
    ExternalInput                     // from externals[*].calls[*].body
    ExternalOutput                    // from externals[*].calls[*].response
    TransactionParams                 // from transactions[*].steps[*].params
)
```

### ResolvedField

```go
type ResolvedField struct {
    Name     string         // PascalCase in Go/Java. "Amount", "CreatedAt", "UserID"
    DBColumn string         // snake_case for DB. "amount", "created_at", "user_id"

    Type TypeDescriptor     // canonical type — source of truth, language-agnostic

    // When Type.IsCustom == true, this points to the resolved object for that type.
    // Generators use this to traverse nested type structure.
    // Nil when Type is a primitive.
    TypeRef *ResolvedObject

    // Constraints
    Required bool
    Unique   bool
    Nullable bool
    Private  bool           // excluded from all DTOs
    Default  interface{}    // typed default, or nil
    Compute  bool           // value is computed in a hook, not from DB

    // For enum fields only
    Values []string         // ["pending", "confirmed", "shipped"]

    // FK — only when this field is a foreign key
    FK *ResolvedForeignKey

    // Pre-rendered tags/annotations — generator writes these verbatim
    // Avoids every generator having to re-derive struct tag logic
    GoStructTag    string   // `json:"amount" validate:"required,min=0"`
    JavaAnnotation string   // "@NotNull @DecimalMin("0")"
}

type ResolvedForeignKey struct {
    TableRef  *ResolvedObject // the resolved TableModel being referenced
    FieldName string          // which field in that table (usually "ID")
}

type ResolvedRule struct {
    Type  string      // "min_length", "max_length", "email", "min", "max", "regex"
    Param interface{} // 3, "^[a-z]+$", etc.
}

type ResolvedIndex struct {
    Fields []string
    Unique bool
    Name   string    // "idx_users_email"
}
```

### TypeDescriptor (in typemap.go)

Language-agnostic canonical type representation. The ImportResolver reads `.Kind` to determine imports. Generators read `.GoType` or `.JavaType` to render the actual type name.

```go
type TypeDescriptor struct {
    Kind TypeKind    // TypeStr | TypeInt | TypeDecimal | TypeBool | TypeDate
                     // TypeTimestamp | TypeUUID | TypeJSON | TypeEnum | TypeCustom

    // Rendered type names per language
    GoType   string  // "string", "decimal.Decimal", "uuid.UUID", "*Money"
    JavaType string  // "String", "BigDecimal", "UUID", "Money"

    // DB
    DBType     string  // "VARCHAR(255)", "NUMERIC", "UUID", "JSONB"
    DBNullable bool

    // Pointer/nullable
    GoPointer bool   // true when nullable=true or Kind==TypeCustom

    // Metadata
    IsCustom   bool
    IsEnum     bool
    EnumValues []string
}
```

---

## Level 2: ResolvedInterface

### AST → ResolvedInterface mapping

| Source AST | Kind | Interface Name | Generated Path |
|---|---|---|---|
| `TableAST` | `RepositoryInterface` | `{pascal(table)}Repository` e.g. `UsersRepository` | `generated/repo/{table_name}/repo.go` |
| `APIAST` (hook points) | `HookInterface` | `{APIAST.Name}Hooks` e.g. `CreateUserHooks` | `generated/handler/{resource_name}/hooks.go` |
| `ResourceGroupAST` (service contracts) | `ServiceInterface` | `{Resource.Group}Service` e.g. `UserApisService` | `generated/handler/{resource_name}/service.go` |
| `CacheAST.Interface` | `CacheInterface` | `{Interface.Name}` e.g. `UserCache` | `generated/cache/{interface_name}/cache.go` |
| `ExternalAST` | `ExternalInterface` | `{External.Name}` e.g. `StripeClient` | `generated/external/{external_name}/client.go` |

**Key rule on ServiceInterface:** We generate one service interface per `ResourceGroupAST` (not per API). This matches the idiomatic Go pattern you described — one `order.go` file with the `OrderService` interface listing `CreateOrder`, `UpdateOrder`, `GetOrder` as methods. The implementation lives in `order_impl.go` and each method's body in `create_order.go`, `update_order.go`, etc. The `ResolvedInterface` captures the interface contract; `ResolvedImplementation` captures the struct and wiring.

### Struct

```go
type ResolvedInterface struct {
    Name string
    Path string
    Kind InterfaceKind

    Functions []ResolvedFunction   // ordered list of method signatures

    // Populated by ImportResolver in Level 2A
    Imports map[Lang]ImportSet
}

type InterfaceKind int

const (
    RepositoryInterface InterfaceKind = iota
    HookInterface
    ServiceInterface
    CacheInterface
    ExternalInterface
)
```

### ResolvedFunction

```go
type ResolvedFunction struct {
    Name    string

    Params  []ResolvedParam
    Returns []ResolvedReturn

    // Pre-rendered full signature — generator writes this verbatim
    // "GetUserByEmail(ctx context.Context, email string) (*User, error)"
    GoSignature   string
    JavaSignature string
}

type ResolvedParam struct {
    Name    string          // "ctx", "email", "id"
    Type    TypeDescriptor

    // When Type.IsCustom == true, points to the full ResolvedObject.
    // This is how a generator knows the shape of a custom type param
    // without having to look it up separately.
    TypeRef *ResolvedObject
}

type ResolvedReturn struct {
    // No Name field — return values in interfaces are type-only
    Type    TypeDescriptor

    // Same as params: when Type.IsCustom == true, points to the ResolvedObject
    TypeRef *ResolvedObject
}
```

**Why `TypeRef` on both Param and Return:** When a function takes or returns a custom type (e.g. `CreateOrder(params PlaceOrderParams)`), the generator needs to know the full structure of `PlaceOrderParams` to generate correct code — serialization, logging, validation, etc. The `TypeRef` pointer gives direct access without a second lookup.

### How repository functions are derived

The Resolver builds one `ResolvedFunction` per declared query in the table. The query shorthand maps to a function signature:

```
find_by: [email]          → GetUserByEmail(ctx context.Context, email string) (*User, error)
find_by: [status]         → GetUsersByStatus(ctx context.Context, status string) ([]*User, error)
  op: eq
exists: [email]           → UserExistsByEmail(ctx context.Context, email string) (bool, error)
count: [role]             → CountUsersByRole(ctx context.Context, role string) (int64, error)
paginate: cursor          → ListUsers(ctx context.Context, p pagination.CursorParams) (*pagination.CursorPage[User], error)
soft_delete: true         → SoftDeleteUser(ctx context.Context, id uuid.UUID) error
bulk_create: true         → BatchCreateUsers(ctx context.Context, users []*User) error
custom: GetWithOrderCount → GetWithOrderCount(ctx context.Context, id uuid.UUID) ([]*User, error)
  returns: many
```

The function name derivation rule: `{verb}{ModelName}By{fields joined in PascalCase}`. Verb depends on the query kind: `Get` for `find_by`, `List` for `paginate`, `Count` for `count`, `Exists` for `exists`, `SoftDelete` for soft_delete, `BatchCreate` for bulk_create.

### How hook functions are derived

Each touch produces a Before + After pair. Plus two fixed hooks at the API level.

```
API: CreateUser
  touches:
    - table: users    op: create     → BeforeTableUsersCreate, AfterTableUsersCreate
    - external: CRM   method: Sync   → BeforeCRMSync, AfterCRMSync

  fixed hooks:
    BeforeCreateUser    (entry — inspect request, set initial flags)
    BeforeResponse      (exit — build response from context)
```

Every hook function has the same signature: `func(ctx context.Context, shared *CreateUserContext) error`. The `shared *CreateUserContext` param TypeRef points to the SharedContext `ResolvedObject` for this API.

### How service interface functions are derived

One function per `APIAST` in the `ResourceGroupAST`. The function takes the request DTO and returns the response DTO:

```
ResourceGroup: UserAPIs
  apis:
    - CreateUser    → CreateUser(ctx context.Context, req CreateUserRequest) (*CreateUserResponse, error)
    - GetUser       → GetUser(ctx context.Context, req GetUserRequest) (*GetUserResponse, error)
    - ListUsers     → ListUsers(ctx context.Context, req ListUsersRequest) (*ListUsersResponse, error)
```

---

## Level 3: ResolvedImplementation

### AST → ResolvedImplementation mapping

| Source AST | Kind | Impl Name | Generated Path(s) |
|---|---|---|---|
| `TableAST` | `RepositoryImpl` | `{pascal(table)}RepositoryImpl` | `generated/repo/{table_name}/repo_impl.go` + one file per query |
| `ResourceGroupAST` | `ServiceImpl` | `{Resource.Group}ServiceImpl` | `generated/handler/{resource_name}/{resource_name}_impl.go` + one file per API |
| `TransactionAST` | `TransactionImpl` | `{Tx.Name}Tx` | `generated/tx/{tx_name}/tx_impl.go` |
| `CacheAST.Interface` | `CacheImpl` | `{Interface.Name}Impl` | `generated/cache/{interface_name}/cache_impl.go` |
| `ExternalAST` | `ExternalImpl` | `{External.Name}Impl` | `generated/external/{external_name}/client_impl.go` |
| `ExternalAST` | `ExternalMockImpl` | `{External.Name}Mock` | `generated/external/{external_name}/client_mock.go` |

### Struct

```go
type ResolvedImplementation struct {
    Name string
    Path string            // the *_impl.go file, e.g. "generated/handler/user/user_impl.go"
    Kind ImplementationKind

    // Which interface this satisfies
    Implements *ResolvedInterface

    // What this implementation needs injected (struct fields + constructor params)
    // Generator derives the constructor from these — no need to pre-declare it
    Dependencies []ResolvedDependency

    // One entry per method in the Implements interface, in the same order
    Methods []ResolvedMethod

    // Populated by ImportResolver in Level 3A
    Imports map[Lang]ImportSet
}

type ImplementationKind int

const (
    RepositoryImpl   ImplementationKind = iota
    ServiceImpl
    TransactionImpl
    CacheImpl
    ExternalImpl
    ExternalMockImpl
)

type ResolvedDependency struct {
    FieldName string   // struct field name: "db", "userCache", "hooks", "producer"
    TypeName  string   // Go type string: "*sql.DB", "*UserCache", "*CreateUserHooks"
    Import    string   // import path if needed: "database/sql"
}
```

### ResolvedMethod

```go
type ResolvedMethod struct {
    // Matches a function Name from the Implements interface
    FunctionName string

    // What infra this method touches, in declaration order.
    // The Generator uses this to build the function body — not pre-rendered statements.
    // This gives the Generator enough information to render correct ORM-specific code.
    Touches []ResolvedTouch

    // For ServiceImpl only: the shared context type for this method
    SharedContext *ResolvedObject
}
```

**This is the key design decision for Level 3:** Instead of pre-rendering statements (as proposed earlier), `ResolvedMethod` declares what the method *touches*. The Generator then knows what to render based on the ORM/framework config. This keeps the Resolver language-agnostic and puts ORM-specific rendering where it belongs — in the Generator.

The Resolver resolves `which` infra is touched, the `order`, and the `operation`. The Generator decides `how` to call that infra given the target ORM.

### ResolvedTouch

```go
// ResolvedTouch represents one infra interaction inside a method body.
// For ServiceImpl methods, these are the API's declared touches.
// For RepositoryImpl methods, there is exactly one touch (the query).
// For TransactionImpl, there are multiple touches (one per step).
type ResolvedTouch struct {
    Kind TouchKind   // Table | External | Cache | Message | Transaction | Query

    // Resolved references — exactly one is set depending on Kind
    TableRef       *ResolvedObject        // the table model
    QueryRef       *ResolvedFunction      // the specific repository function being called
    ExternalRef    *ResolvedInterface     // the external client interface
    ExternalMethod *ResolvedFunction      // the specific method on the external
    CacheRef       *ResolvedInterface     // the cache interface
    CacheMethod    *ResolvedFunction      // Get, Set, Delete, Invalidate
    MessageName    string                 // event name to publish

    // For ServiceImpl touches: control flag info
    Flag    string   // "RunTableUsersCreate"
    Default bool     // initial flag value

    // Result and error field names in SharedContext
    ResultField string   // "TableUsersResult"
    ErrorField  string   // "TableUsersError"
    FatalError  bool     // whether a non-nil error should abort the method
}
```

### The Go file layout for ServiceImpl

You described the exact idiomatic Go pattern. The Resolver models it like this:

```
ResourceGroup: UserAPIs
  → ResolvedImplementation {
      Name: "UserApisServiceImpl"
      Path: "generated/handler/user/user_impl.go"   ← struct definition + constructor
      Implements: → UserApisService interface
      Dependencies: [usersRepo, userCache, createUserHooks, getUserHooks, ...]
      Methods: [
        ResolvedMethod{FunctionName: "CreateUser", ...}   ← body in create_user.go
        ResolvedMethod{FunctionName: "GetUser", ...}      ← body in get_user.go
        ResolvedMethod{FunctionName: "ListUsers", ...}    ← body in list_users.go
      ]
    }
```

Each `ResolvedMethod` body gets its own file. The `Path` on `ResolvedImplementation` is the struct+constructor file. The Generator produces the per-method files by deriving their paths from the method name: `generated/handler/user/create_user.go`, `generated/handler/user/get_user.go`, etc.

The struct looks like this in `user_impl.go`:
```go
type UserApisServiceImpl struct {
    usersRepo  *UsersRepository
    userCache  *UserCache
    createUserHooks *CreateUserHooks
    getUserHooks    *GetUserHooks
}

func NewUserApisServiceImpl(
    usersRepo  *UsersRepository,
    userCache  *UserCache,
    createUserHooks *CreateUserHooks,
    getUserHooks    *GetUserHooks,
) *UserApisServiceImpl {
    return &UserApisServiceImpl{...}
}
```

The constructor is derived entirely from `Dependencies` — the Resolver doesn't need to pre-declare it.

---

## The Complete Data Flow

```
SpecAST
  │
  ├─── Level 1: Resolve all ResolvedObjects
  │      TypeAST         → TypeObject (in generated/types/types.go)
  │      TableAST        → TableModel (in generated/repo/{t}/model.go)
  │      APIAST          → RequestDTO, ResponseDTO, SharedContext (in generated/handler/{api}/types.go)
  │      ExternalAST     → ExternalInput, ExternalOutput (in generated/external/{e}/types.go)
  │      TransactionAST  → TransactionParams (in generated/tx/{t}/types.go)
  │
  ├─── Level 1A: ImportResolver → attaches Imports map to each ResolvedObject
  │
  ├─── Level 2: Resolve all ResolvedInterfaces
  │      TableAST          → RepositoryInterface (in generated/repo/{t}/repo.go)
  │      ResourceGroupAST  → ServiceInterface (in generated/handler/{r}/service.go)
  │      APIAST            → HookInterface (in generated/handler/{r}/hooks.go)
  │      CacheAST          → CacheInterface (in generated/cache/{c}/cache.go)
  │      ExternalAST       → ExternalInterface (in generated/external/{e}/client.go)
  │
  ├─── Level 2A: ImportResolver → attaches Imports map to each ResolvedInterface
  │
  ├─── Level 3: Resolve all ResolvedImplementations
  │      TableAST          → RepositoryImpl (in generated/repo/{t}/repo_impl.go + per-query files)
  │      ResourceGroupAST  → ServiceImpl (in generated/handler/{r}/{r}_impl.go + per-api files)
  │      TransactionAST    → TransactionImpl (in generated/tx/{t}/tx_impl.go)
  │      CacheAST          → CacheImpl + mock (in generated/cache/{c}/...)
  │      ExternalAST       → ExternalImpl + mock (in generated/external/{e}/...)
  │      All               → DI graph (in generated/wire.go)
  │
  └─── Level 3A: ImportResolver → attaches Imports map to each ResolvedImplementation
```

---

## ResolvedSpec (final output)

```go
type ResolvedSpec struct {
    // Metadata
    Project   string
    Module    string
    Lang      Lang
    Framework Framework
    DB        DBDriver

    // Level 1 output
    Objects []ResolvedObject

    // Level 2 output
    Interfaces []ResolvedInterface

    // Level 3 output
    Implementations []ResolvedImplementation

    // Subsystems (resolved from AST but not represented as Objects/Interfaces/Impls)
    Messaging *ResolvedMessaging
    Auth      *ResolvedAuth
    Config    []ResolvedConfigVar
}
```

Generators query these slices by `Kind` to find what they need:

```go
// Go types generator
objects := spec.ObjectsOfKind(TypeObject)

// Go table model generator
model := spec.ObjectOfKind(TableModel, "users")

// Go repository generator
repoInterface := spec.InterfaceOfKind(RepositoryInterface, "users")
repoImpl      := spec.ImplOfKind(RepositoryImpl, "users")

// Go service generator
serviceImpl := spec.ImplOfKind(ServiceImpl, "UserAPIs")
```

---

## What Each Generator Sees

| Generator | Reads |
|---|---|
| `go.types` | `Objects` where Kind == TypeObject |
| `go.table.model` | `Objects` where Kind == TableModel |
| `go.table.repo` | `Interfaces` where Kind == RepositoryInterface + `Implementations` where Kind == RepositoryImpl |
| `go.api.dto` | `Objects` where Kind == RequestDTO \| ResponseDTO \| SharedContext |
| `go.api.hooks` | `Interfaces` where Kind == HookInterface |
| `go.api.service` | `Implementations` where Kind == ServiceImpl (reads Touches, not pre-rendered statements) |
| `go.api.handler` | `Interfaces` where Kind == ServiceInterface + RequestDTO + ResponseDTO objects |
| `go.cache` | `Interfaces` where Kind == CacheInterface + `Implementations` where Kind == CacheImpl |
| `go.external` | `Interfaces` where Kind == ExternalInterface + `Implementations` ExternalImpl + ExternalMockImpl |
| `go.wire` | All `Implementations.Dependencies` across all kinds |
| `sql.migration` | `Objects` where Kind == TableModel (reads TableName, Fields, DBColumn, DBType) |
