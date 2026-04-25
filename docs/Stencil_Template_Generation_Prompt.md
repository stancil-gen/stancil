# Stencil — Template Generation Prompt

## What You Are Building

You are writing **Go text/template files** for **Stencil**, a deterministic backend code generation platform. Stencil reads a `stencil.yaml` spec file and generates 100% of structural Go code for a production backend service. Developers only write business logic in hook files. Nothing is AI-generated at runtime — all generation is template-driven and deterministic.

The tool is written in Go. It uses Go's `text/template` package with a custom FuncMap. Your job is to write the `.go.tmpl` files that the tool renders to produce real, production-quality Go source code.

---

## The Core Mental Model

Stencil has two layers:

**Layer 1 — Core Infrastructure** (declared in YAML, generates standalone typed components)
- `tables:` → DB model structs, repository interfaces + implementations, migration SQL
- `types:` → Custom value objects (Money, Address) stored as JSONB
- `transactions:` → Atomic multi-step DB operation orchestrators
- `externals:` → Outbound HTTP clients (third-party APIs, inter-service calls)
- `messaging:` → Kafka/RabbitMQ producers and consumers
- `cache:` → Redis typed cache interfaces
- `auth:` → JWT middleware and auth endpoints
- `observability:` → Prometheus metrics, OpenTelemetry tracing

**Layer 2 — API Resources** (groups of endpoints, each touching core infra)
- `resources:` → API groups (e.g. UserAPIs, OrderAPIs)
  - Each group has a `base_path` and contains multiple `apis:`
  - Each API has `steps:` — an ordered list of infra interactions
  - Each step declares which infra it touches (`table`, `external`, `message`, `cache`, `transaction`)
  - Each step has a `flag` (boolean control field) and a `default` (true/false)

**The Execution Model:**
```
Request → New{API}Context (flags set to defaults)
        → BeforeAPI hook (developer sets flags, mutates request)
        → For each step in graph order:
            → if shared.Run{StepFlag} is true:
                → mapper builds typed step input from shared context
                → executor calls infra (DB, HTTP, Kafka, Redis)
                → result stored in shared.{Step}Output
                → AfterStep hook (developer reads result, sets next flags)
        → BeforeResponse hook (developer builds shared.Response)
        → Return shared.Response
```

**The Shared Context** is a generated struct that flows through the entire execution:
```go
type CreateUserContext struct {
    Request              CreateUserRequest    // incoming request
    RunTableUsersCreate  bool                 // control flag, default: true
    RunCRMClientCreate   bool                 // control flag, default: false (conditional)
    RunMessageUserCreated bool                // control flag, default: true
    TableUsersResult     *User               // output of DB step
    CRMClientResult      *crm.Response       // output of external step
    TableUsersError      error               // error from DB step
    CRMClientError       error               // error from external step
    Response             *UserResponse       // set by developer in BeforeResponse
}
```

**The Hook Interface** is generated per API:
```go
type CreateUserHooks struct {
    BeforeCreateUser      func(ctx context.Context, shared *CreateUserContext) error
    AfterTableUsersCreate func(ctx context.Context, shared *CreateUserContext) error
    AfterCRMClientCreate  func(ctx context.Context, shared *CreateUserContext) error
    BeforeResponse        func(ctx context.Context, shared *CreateUserContext) error
}
```

**The Mapper Interface** is generated per API with a default implementation:
```go
type CreateUserMappers interface {
    MapTableUsersCreateInput(ctx context.Context, shared *CreateUserContext) (*WriteUserInput, error)
    MapResponse(ctx context.Context, shared *CreateUserContext) (*UserResponse, error)
}
// Default implementation uses name+type matching to infer mappings.
// Fields it cannot infer get MUST_OVERRIDE bodies that return descriptive errors.
// Developer embeds Default and overrides only what's needed.
type DefaultCreateUserMappers struct{}
```

---

## Key Design Principles

1. **Interfaces everywhere** — Every generated concrete type has an interface. Generated `wire.go` registers the default. Developer can override by registering a custom constructor in `hooks/register.go`. Container uses last-registration-wins.

2. **Generated code is read-only** — All files in `generated/` have `chmod 444`. Developer never edits them.

3. **Developer owns `hooks/`** — Tool never touches `hooks/`. Generated once (scaffold): `main.go`, `config/config.go`, `hooks/register.go`.

4. **The service executor never makes decisions** — It only reads control flags. All decisions are in developer hooks.

5. **Defaults with embed-and-override** — Every generated impl (DefaultUsersRepository, DefaultCreateUserMappers, etc.) is embeddable. Developer overrides specific methods only.

6. **Library selection** — The YAML has a `libs:` block:
   ```yaml
   libs:
     db:        sqlc     # sqlc | gorm | sqlx | raw
     cache:     go-redis
     messaging: kafka-go
     http:      resty
   ```
   Different `libs.db` values produce different repository and model templates.

---

## The Full YAML Reference Spec

```yaml
version: 1
project: orders-service
lang:    go
framework: gin       # gin | echo | fiber | chi

libs:
  db:        sqlc    # sqlc | gorm | sqlx | raw
  cache:     go-redis
  messaging: kafka-go
  http:      resty

config:
  - name: DATABASE_URL    type: str   required: true
  - name: REDIS_URL       type: str   required: true
  - name: JWT_SECRET      type: str   required: true   min_length: 32
  - name: STRIPE_URL      type: str   required: true
  - name: KAFKA_BROKER    type: str   required: true
  - name: PORT            type: int   default: 8080

types:
  - name: Money
    fields:
      - name: amount    type: decimal   required: true
      - name: currency  type: str       required: true
        rules:
          - type: min_length   value: 3
          - type: max_length   value: 3

tables:
  - name: users
    fields:
      - name: first_name     type: str    required: true
      - name: last_name      type: str    required: true
      - name: email          type: str    required: true   unique: true
      - name: password_hash  type: str    required: true   private: true
      - name: role           type: enum   values: [admin, user]   default: user
      - name: status         type: enum   values: [active, suspended]   default: active
    queries:
      - find_by:   [email]
      - find_by:   [status]
      - exists:    [email]
      - soft_delete: true
      - paginate:  offset
        order_by:  [{field: created_at, direction: desc}]
        default_limit: 20
    states:
      field: status
      transitions:
        - from: active     to: suspended
        - from: suspended  to: active
    errors: [NotFound, EmailTaken]

  - name: orders
    fields:
      - name: user_id   type: uuid    required: true
      - name: status    type: enum
        values: [pending, confirmed, shipped, delivered, cancelled]
        default: pending
      - name: total     type: Money   required: true
      - name: items     type: json
    queries:
      - find_by: [user_id]
      - find_by: [user_id, status]
      - soft_delete: true
      - paginate: cursor
        order_by: [{field: created_at, direction: desc}]
    states:
      field: status
      transitions:
        - from: pending    to: confirmed
        - from: confirmed  to: shipped
        - from: shipped    to: delivered
        - from: pending    to: cancelled
    errors: [NotFound, InvalidTransition]

transactions:
  - name: PlaceOrder
    type: local
    steps:
      - name:   CreateOrder
        sql: |
          INSERT INTO orders (user_id, total, status)
          VALUES ($1, $2, 'pending') RETURNING id
        params:
          - name: user_id   type: uuid
          - name: total     type: Money
        returns: [id]
      - name:   DeductInventory
        sql: |
          UPDATE inventory
          SET quantity = quantity - $1
          WHERE product_id = $2 AND quantity >= $1
        params:
          - name: quantity    type: int
          - name: product_id  type: uuid
        error_if_rows: 0
        error: InsufficientStock

externals:
  - name:     StripeClient
    type:     http
    base_url: ${STRIPE_URL}
    auth:     bearer_token
    timeout:  10s
    retry:
      attempts:  3
      backoff:   exponential
      on_status: [429, 502, 503]
    calls:
      - name:     ChargeCard
        method:   POST
        path:     /v1/charges
        body:     ChargeRequest
        response: ChargeResponse
        errors:
          - status: 402   error: CardDeclined
          - status: 429   error: RateLimited

messaging:
  broker:  kafka
  brokers: [${KAFKA_BROKER}]
  producers:
    - topic: user.created   event: UserCreated   key: user_id
    - topic: order.placed   event: OrderPlaced   key: order_id
  consumers:
    - topic:   payment.completed
      event:   PaymentCompleted
      group:   orders-service
      handler: OnPaymentCompleted
      retry:
        attempts: 5
        backoff:  exponential
        dlq:      payment.failed.dlq

cache:
  provider: redis
  url:      ${REDIS_URL}
  prefix:   orders-svc
  interfaces:
    - name:          UserCache
      key_template:  "user:{id}"
      value_type:    UserResponse
      default_ttl:   5m
      methods: [Get, Set, Delete, Invalidate]

auth:
  provider: jwt
  secret:   ${JWT_SECRET}
  expiry:   24h
  login:
    via:        [email, password]
    rate_limit: 5/min
  refresh_token:
    storage: redis
  roles: [admin, user]

middleware: [logging, recovery, cors, rate_limit: 100rpm]

resources:
  - group:     UserAPIs
    base_path: /users
    auth:      jwt

    apis:
      - name:   CreateUser
        method: POST
        path:   /
        auth:   public

        dtos:
          request:
            name: CreateUserRequest
            fields:
              - name: first_name   type: str   required: true
              - name: last_name    type: str   required: true
              - name: email        type: str   required: true
                rules: [{type: email}]
              - name: password     type: str   required: true
                rules: [{type: min_length, value: 8}]
          response:
            name: UserResponse
            fields:
              - name: id          type: uuid
              - name: first_name  type: str
              - name: last_name   type: str
              - name: email       type: str
              - name: role        type: str
              - name: created_at  type: timestamp

        steps:
          - id:      writeUser
            touch:   {table: users, op: create}
            flag:    RunTableUsersCreate
            default: true
            fatal:   true
          - id:      publishEvent
            touch:   {message: UserCreated}
            flag:    RunMessageUserCreated
            default: true
            fatal:   false

      - name:   GetUser
        method: GET
        path:   /:id
        auth:   jwt

        dtos:
          response:
            name: UserResponse
            fields:
              - name: id          type: uuid
              - name: first_name  type: str
              - name: last_name   type: str
              - name: email       type: str
              - name: role        type: str

        steps:
          - id:      readCache
            touch:   {cache: UserCache, op: get}
            flag:    RunCacheRead
            default: true
            fatal:   false
          - id:      readDB
            touch:   {table: users, op: get}
            flag:    RunTableUsersGet
            default: true
            fatal:   true
          - id:      writeCache
            touch:   {cache: UserCache, op: set}
            flag:    RunCacheWrite
            default: true
            fatal:   false

  - group:     OrderAPIs
    base_path: /orders
    auth:      jwt

    apis:
      - name:   PlaceOrder
        method: POST
        path:   /

        dtos:
          request:
            name: PlaceOrderRequest
            fields:
              - name: product_id      type: uuid   required: true
              - name: quantity        type: int    required: true
              - name: payment_token   type: str    required: true
          response:
            name: PlaceOrderResponse
            fields:
              - name: order_id    type: uuid
              - name: charge_id   type: str
              - name: status      type: str

        steps:
          - id:      validateUser
            touch:   {table: users, op: get}
            flag:    RunTableUsersGet
            default: true
            fatal:   true
          - id:      runTransaction
            touch:   {transaction: PlaceOrder}
            flag:    RunTxPlaceOrder
            default: true
            fatal:   true
          - id:      chargePayment
            touch:   {external: StripeClient, method: ChargeCard}
            flag:    RunStripeCharge
            default: true
            fatal:   true
          - id:      publishEvent
            touch:   {message: OrderPlaced}
            flag:    RunMessageOrderPlaced
            default: true
            fatal:   false
```

---

## Generated File Structure (What You Are Templating)

```
generated/                           ← TOOL OWNS — chmod 444, never edited
  config_types.go                    ← Config struct from config: block
  types/
    money.go                         ← Money struct + Validate() + sql.Scanner/Valuer
  tables/
    users/
      model.go                       ← User struct, UserRole/UserStatus enums, params structs
      queries.sql                    ← sqlc input SQL (only for libs.db: sqlc)
      repository.go                  ← UsersRepository interface + DefaultUsersRepository
      errors.go                      ← ErrUsersNotFound, ErrUsersEmailTaken + HTTPStatusFor()
    orders/
      model.go
      queries.sql
      repository.go
      errors.go
  transactions/
    placeorder.go                    ← PlaceOrderTx interface + DefaultPlaceOrderTx
  externals/
    stripe_client.go                 ← StripeClient interface + DefaultStripeClient + types
    stripe_client_mock.go            ← MockStripeClient for tests
  messaging/
    events.go                        ← UserCreated, OrderPlaced, PaymentCompleted structs
    producers.go                     ← PublishUserCreated(), PublishOrderPlaced()
    consumers.go                     ← OnPaymentCompleted listener + DLQ routing
  cache/
    user_cache.go                    ← UserCache interface + DefaultUserCache
  auth/
    middleware.go                    ← Auth(), RequireRoles(), OwnerOrRole(), GetCaller()
    handler.go                       ← login, logout, refresh endpoints
  apis/
    createuser/
      dto.go                         ← CreateUserRequest, UserResponse structs
      context.go                     ← CreateUserContext struct + New constructor
      steps.go                       ← WriteUserInput/Output, PublishEventInput/Output
      mappers.go                     ← CreateUserMappers interface + DefaultCreateUserMappers
      graph.go                       ← CreateUserGraphBuilder interface
      service.go                     ← CreateUserService + executor (reads flags, calls infra)
      handler.go                     ← HTTP handler (bind, call service, respond)
    getuser/
      dto.go
      context.go
      steps.go
      mappers.go
      graph.go
      service.go
      handler.go
    placeorder/
      dto.go
      context.go
      steps.go
      mappers.go
      graph.go
      service.go
      handler.go
  migration/
    001_create_users.sql
    002_create_orders.sql
    003_create_refresh_tokens.sql
  routes.go                          ← all route registration + middleware application
  wire.go                            ← all DI container provider registrations

config/
  config.go                          ← GENERATED ONCE — Load() function, developer edits

hooks/                               ← DEVELOPER OWNS ENTIRELY
  register.go                        ← GENERATED ONCE — developer adds Provide() calls
  user/
    createuser.mappers.go            ← implements CreateUserMappers (embed + override)
    createuser.graph.go              ← implements BuildGraph() for CreateUser
    createuser.hooks.go              ← BeforeCreateUser, AfterTableUsersCreate etc.
    getuser.mappers.go
    getuser.graph.go
    getuser.hooks.go
  order/
    placeorder.mappers.go
    placeorder.graph.go
    placeorder.hooks.go

main.go                              ← GENERATED ONCE — container assembly, developer edits
go.mod                               ← maintained by stencil
```

---

## Import Hierarchy (Strict — Violations = Build Error)

```
Level 0  generated/types/
           imports: stdlib only (encoding/json, database/sql/driver, fmt, strings)

Level 1  generated/tables/*/model.go
         generated/messaging/events.go
         generated/apis/*/dto.go
           imports: types/ + stdlib only

Level 2  generated/tables/*/repository.go
         generated/tables/*/errors.go
         generated/externals/*.go
         generated/cache/*.go
           imports: Level 1 + stencil-go library + third-party DB/Redis/HTTP libs

Level 3  generated/transactions/*.go
         generated/auth/middleware.go
         generated/apis/*/context.go
         generated/apis/*/steps.go
           imports: Level 2 + stencil-go/tx + stencil-go/middleware

Level 4  generated/apis/*/mappers.go
         generated/apis/*/graph.go
           imports: Level 3 (context.go, steps.go, dto.go)

Level 5  generated/apis/*/service.go
           imports: Level 4 + repo + externals + messaging + cache + transactions

Level 6  generated/apis/*/handler.go
         generated/auth/handler.go
           imports: service.go + dto.go + stencil-go/handler + stencil-go/errors

Level 7  generated/routes.go
           imports: ALL handlers + auth/middleware + stencil-go/middleware

Level 8  generated/wire.go
         main.go
           imports: everything + stencil-go/container

hooks/   imports Level 2-4 interfaces ONLY — never imports generated services
```

---

## The stencil-go Runtime Library

Generated code imports this library (not copied — it's a published Go module):
`github.com/stencil-run/stencil-go`

Packages:
```
container/     DI container (Provide, MustResolve, OnStart, OnStop, Start, Stop)
errors/        ValidationError, DomainError, WriteError, MapValidationError,
               NewNotFound, NewForbidden, NewConflict, NewUnprocessable
handler/       Bind[T](c) — parse + validate; WriteCreated, WriteOK, WriteNoContent
pagination/    CursorPage[T], OffsetPage[T], NewCursorPage, ParseCursorParams
middleware/    RequestID, StructuredLogger, Recovery, CORS, RateLimit
cache/         BaseCache (embedded by generated cache impls)
tx/            RunTx(ctx, db, fn) — BEGIN/COMMIT/ROLLBACK wrapper
messaging/     BaseProducer, BaseConsumer, Message
observability/ Metrics, StartSpan, DBInstrument
graph/         Graph, Parallel, Then, After, When, Respond (DAG execution)
```

---

## Template FuncMap — Available Helper Functions

These functions are available inside all `.go.tmpl` files:

```
toPascalCase(s string) string       "user_id" → "UserID", "first_name" → "FirstName"
toCamelCase(s string) string        "UserID" → "userID"
toSnakeCase(s string) string        "UserID" → "user_id"
toPlural(s string) string           "User" → "users", "Address" → "addresses"
toLower(s string) string            "UserAPIs" → "userapIs" (use for package names)
toPackageName(s string) string      "CreateUser" → "createuser" (lowercase no sep)
typeMap(dslType, lang string) string "str","go" → "string", "decimal","go" → "float64"
dbType(dslType string) string       "str" → "VARCHAR(255)", "uuid" → "UUID"
zeroValue(goType string) string     "string" → `""`, "int64" → "0", "*User" → "nil"
isPointer(field) bool               true if field is nullable
jsonTag(field) string               builds correct json tag incl omitempty
validateTag(field) string           builds go-playground/validator tag string
gormTag(field) string               builds gorm:"..." tag string
sqlcTag(field) string               builds db:"..." tag for sqlc
hasFeature(resource, feat) bool     "users","uuid" → true if uuid field present
renderImports(ImportSet) string     renders grouped import block
joinComma([]string) string          ["a","b","c"] → "a, b, c"
dict(k,v,...) map                   creates map for passing multiple values to template
```

---

## Template File Naming Convention

```
templates/go/
  types/
    type.go.tmpl                     ← Level 0: one per custom type
  table/
    model.go.tmpl                    ← Level 1: DB model + enums
    model_gorm.go.tmpl               ← Level 1: GORM variant (different struct tags)
    repository_sqlc.go.tmpl          ← Level 2: sqlc implementation
    repository_gorm.go.tmpl          ← Level 2: GORM implementation
    repository_sqlx.go.tmpl          ← Level 2: sqlx implementation
    repository_raw.go.tmpl           ← Level 2: database/sql implementation
    errors.go.tmpl                   ← Level 2: domain errors
    queries.sql.tmpl                 ← sqlc input SQL (rendered to .sql, not .go)
  messaging/
    events.go.tmpl                   ← Level 1: event payload structs
    producers.go.tmpl                ← Level 2: typed publish functions
    consumers.go.tmpl                ← Level 2: consumer listener + DLQ
  externals/
    external_client.go.tmpl          ← Level 2: HTTP client interface + default impl
    external_client_mock.go.tmpl     ← Level 2: mock for tests
  cache/
    cache.go.tmpl                    ← Level 2: typed cache interface + Redis impl
  transactions/
    transaction.go.tmpl              ← Level 3: tx orchestrator interface + impl
  auth/
    middleware.go.tmpl               ← Level 3: JWT middleware
    handler.go.tmpl                  ← Level 6: login/logout/refresh handlers
  api/
    dto.go.tmpl                      ← Level 1: request + response structs
    context.go.tmpl                  ← Level 3: shared context struct
    steps.go.tmpl                    ← Level 3: typed step input/output structs
    mappers.go.tmpl                  ← Level 4: mapper interface + default impl
    graph.go.tmpl                    ← Level 4: graph builder interface
    service.go.tmpl                  ← Level 5: executor — reads flags, calls infra
    handler.go.tmpl                  ← Level 6: HTTP handler
  wire/
    wire.go.tmpl                     ← Level 8: DI container provider registrations
    routes.go.tmpl                   ← Level 7: all route registration
    main.go.tmpl                     ← Level 8: app entry point (generated once)
    config.go.tmpl                   ← config/config.go (generated once)
    hooks_register.go.tmpl           ← hooks/register.go (generated once)
```

---

## Template Data Structs — What the Generator Passes to Each Template

### type.go.tmpl receives TypeTemplateData:
```go
type TypeTemplateData struct {
    Name    string          // "Money"
    Imports []string        // sorted deduplicated import paths
    Fields  []TypeFieldData
}
type TypeFieldData struct {
    GoName             string   // "Amount"
    JSONName           string   // "amount"
    GoType             string   // "float64"
    Required           bool
    Rules              []RuleData
    Comment            string
    IsIdentifyingField bool     // first field — used by IsZero()
}
type RuleData struct {
    Type  string   // "min_length"
    Value string   // "3"
}
```

### model.go.tmpl receives ModelTemplateData:
```go
type ModelTemplateData struct {
    PackageName      string          // "users"
    TableName        string          // "users"
    ModelName        string          // "User"
    ReceiverName     string          // "u"
    IDType           string          // "uuid.UUID" or "int64"
    Imports          []string
    Enums            []EnumData
    Fields           []ModelFieldData
    SoftDelete       bool
    HasStateField    bool
    StateEnumType    string          // "UserStatus"
    StateFieldGoName string          // "Status"
    StateTransitions []TransitionData
    EnumPrefix       string          // "UserStatus" (for const block)
    CreateParams     []ParamData
    UpdateParams     []ParamData
    QueryParams      []QueryParamStruct
    DefaultLimit     int
    MaxLimit         int
    PaginationStyle  string          // "cursor" | "offset"
    UniqueErrorSuffix string         // "EmailTaken"
}
type EnumData struct {
    GoName string    // "UserRole"
    Values []string  // ["admin", "user"]
}
type ModelFieldData struct {
    GoName   string   // "FirstName"
    JSONName string   // "first_name"
    DBName   string   // "first_name"
    GoType   string   // "string"
    GORMTag  string   // "not null" (empty for sqlc/sqlx)
    Private  bool
    Comment  string
}
```

### repository_sqlc.go.tmpl receives RepoTemplateData:
```go
type RepoTemplateData struct {
    PackageName       string
    TableName         string
    ModelName         string         // "User"
    InterfaceName     string         // "UsersRepository"
    IDType            string
    Imports           []string
    SoftDelete        bool
    DeclaredQueries   []QueryData
    CreateParams      []ParamData
    UpdateParams      []ParamData
    DefaultLimit      int
    MaxLimit          int
    PaginationStyle   string
    UniqueErrorSuffix string
    Fields            []RepoFieldData
}
type QueryData struct {
    FuncName     string          // "GetUsersByStatus"
    SQLCFuncName string          // "GetUsersByStatus" (sqlc generated)
    Params       []ParamData
    ReturnKind   string          // "one" | "many" | "scalar" | "bool"
    ReturnType   string          // "*User" | "[]User" | "int64" | "bool"
    ZeroValue    string          // "nil" | "0" | "false"
}
```

### errors.go.tmpl receives ErrorsTemplateData:
```go
type ErrorsTemplateData struct {
    PackageName string
    ModelName   string
    Errors      []ErrorData
}
type ErrorData struct {
    Suffix      string   // "NotFound"
    Message     string   // "record not found"
    Description string   // "the requested user does not exist"
    HTTPStatus  int      // 404
}
```

### events.go.tmpl receives MessagingTemplateData:
```go
type MessagingTemplateData struct {
    Imports        []string
    SchemaVersion  string
    ProducerEvents []EventData
    ConsumerEvents []EventData
}
type EventData struct {
    Name            string       // "UserCreated"
    Topic           string       // "user.created"
    KeyField        string       // "user_id"
    KeyFieldGoName  string       // "UserID"
    Group           string       // "orders-service" (consumers only)
    Fields          []EventFieldData
}
```

### external_client.go.tmpl receives ExternalTemplateData:
```go
type ExternalTemplateData struct {
    Name                  string       // "StripeClient"
    PackageName           string       // "externals"
    InterfaceName         string       // "StripeClient"
    BaseURLConfigField    string       // "StripeURL"
    AuthTokenConfigField  string       // "StripeAuthToken"
    HasAuth               bool
    TimeoutDuration       string       // "10 * time.Second"
    MaxRetries            int
    RetryOnStatuses       []int
    StaticHeaders         []HeaderData
    Imports               []string
    Calls                 []CallData
    Errors                []ErrorData
}
type CallData struct {
    MethodName      string
    HTTPMethod      string         // "POST"
    Path            string         // "/v1/charges"
    HasBody         bool
    HasResponse     bool
    RequestType     string         // "ChargeRequest"
    ResponseType    string         // "ChargeResponse"
    ZeroReturn      string         // "nil" or zero value
    RequestFields   []FieldData
    ResponseFields  []FieldData
}
```

### cache.go.tmpl receives CacheTemplateData:
```go
type CacheTemplateData struct {
    Name               string   // "UserCache"
    InterfaceName      string   // "UserCache"
    KeyTemplate        string   // "user:{id}"
    KeyParam           string   // "id"
    KeyType            string   // "uuid.UUID"
    KeyPrefix          string   // "orders-svc:user:"
    ValueType          string   // "UserResponse"
    DefaultTTL         string   // "5m"
    DefaultTTLDuration string   // "5 * time.Minute"
    Imports            []string
}
```

### transaction.go.tmpl receives TransactionTemplateData:
```go
type TransactionTemplateData struct {
    Name          string           // "PlaceOrder"
    InterfaceName string           // "PlaceOrderTx"
    ParamsType    string           // "PlaceOrderParams"
    ResultType    string           // "PlaceOrderResult"
    Imports       []string
    AllParams     []ParamData      // union of all step params
    Results       []ResultField    // from RETURNING clauses
    Errors        []ErrorData
    Steps         []TxStepData
}
type TxStepData struct {
    Name            string
    SQL             string
    Params          []ParamData
    HasReturning    bool
    ReturningFields []ReturnField
    ErrorIfZeroRows bool
    ErrorName       string
    Comment         string
}
```

### context.go.tmpl receives ContextTemplateData:
```go
type ContextTemplateData struct {
    PackageName  string           // "createuser"
    GroupName    string           // "UserAPIs"
    APIName      string           // "CreateUser"
    ContextType  string           // "CreateUserContext"
    RequestType  string           // "CreateUserRequest"
    ResponseType string           // "*UserResponse"
    Imports      []string
    Steps        []ContextStepData
}
type ContextStepData struct {
    ID              string   // "writeUser"
    FlagName        string   // "RunTableUsersCreate"
    DefaultTrue     bool
    HasInput        bool
    InputFieldName  string   // "WriteUserInput"
    InputType       string   // "WriteUserInput"
    HasOutput       bool
    OutputFieldName string   // "WriteUserOutput"
    OutputType      string   // "WriteUserOutput"
    CanFail         bool
    ErrorFieldName  string   // "WriteUserError"
}
```

### steps.go.tmpl receives StepsTemplateData:
```go
type StepsTemplateData struct {
    PackageName     string
    GroupName       string
    APIName         string
    ContextType     string
    MappersType     string         // "CreateUserMappers"
    Imports         []string
    Steps           []StepTypeData
}
type StepTypeData struct {
    ID               string
    TouchDescription string        // "table users create" | "external StripeClient ChargeCard"
    HasInput         bool
    InputType        string        // "WriteUserInput"
    InputFields      []FieldData
    HasOutput        bool
    OutputType       string        // "WriteUserOutput"
    OutputFields     []FieldData
}
```

### mappers.go.tmpl receives MappersTemplateData:
```go
type MappersTemplateData struct {
    PackageName      string
    GroupName        string
    APIName          string
    MappersType      string           // "CreateUserMappers"
    ContextType      string           // "CreateUserContext"
    ResponseType     string           // "UserResponse"
    Imports          []string
    Steps            []MapperStepData
    CanInferResponse bool
    ResponseFields   []MappedFieldData
    ResponseNilGuards []NilGuard
    UnmappedResponseFields []UnmappedField
    FirstStepOutputField   string
}
type MapperStepData struct {
    ID              string
    HasInput        bool
    InputType       string
    CanInferMapping bool
    InferenceSources []string       // ["Request.UserID", "WriteUserOutput.ID"]
    InputFields     []MappedFieldData
    UnmappedFields  []UnmappedField
}
type MappedFieldData struct {
    GoName        string
    GoType        string
    InferredFrom  string           // "shared.Request.UserID" or "" if MUST_OVERRIDE
    InferenceNote string           // "Request.UserID → WriteUserInput.UserID (same name+type)"
}
type UnmappedField struct {
    GoName string
    GoType string
    Reason string   // "requires runtime data from previous step output"
}
```

### service.go.tmpl receives ServiceTemplateData:
```go
type ServiceTemplateData struct {
    PackageName    string
    GroupName      string
    APIName        string
    ContextType    string           // "CreateUserContext"
    HooksType      string           // "CreateUserHooks"
    MappersType    string           // "CreateUserMappers"
    RequestType    string           // "CreateUserRequest"
    ResponseType   string           // "*UserResponse"
    Imports        []string
    Dependencies   []DependencyData // injected fields on the service struct
    ValidatorCall  string           // rendered validation code snippet
    Steps          []ServiceStepData
    FallbackMapper string           // code snippet for nil-response fallback
}
type ServiceStepData struct {
    ID              string
    FlagName        string          // "RunTableUsersCreate"
    InputFieldName  string          // "WriteUserInput"
    InputType       string
    OutputFieldName string          // "WriteUserOutput"
    OutputType      string
    ErrorFieldName  string
    InfraCall       string          // pre-rendered infra call code snippet
    Fatal           bool
    BeforeHook      *HookCallSite
    AfterHook       *HookCallSite
}
type HookCallSite struct {
    FieldName string   // "AfterTableUsersCreate"
}
type DependencyData struct {
    FieldName string   // "usersRepo"
    Type      string   // "users.UsersRepository"
    ParamName string   // "usersRepo" (constructor param)
}
```

### handler.go.tmpl receives HandlerTemplateData:
```go
type HandlerTemplateData struct {
    PackageName   string
    APIName       string
    GroupName     string
    HTTPMethod    string          // "POST"
    FullPath      string          // "/users"
    Auth          string          // "public" | "jwt"
    Roles         []string
    Owner         bool
    HasRequest    bool
    RequestType   string
    ResponseType  string
    StatusCode    int             // 201 for create, 200 for get, 204 for delete
    Imports       []string
}
```

### routes.go.tmpl receives RoutesTemplateData:
```go
type RoutesTemplateData struct {
    Imports    []string
    Groups     []RouteGroupData
    Middleware []string          // global middleware
}
type RouteGroupData struct {
    GroupName    string          // "UserAPIs"
    BasePath     string          // "/users"
    Auth         string          // "jwt" | "public"
    Routes       []RouteData
}
type RouteData struct {
    HandlerVar  string           // "userHandler" (lowercased group name)
    Method      string           // "POST"
    Path        string           // "/"
    HandlerFunc string           // "CreateUser"
    Auth        string
    Roles       []string
    Owner       bool
}
```

### wire.go.tmpl receives WireTemplateData:
```go
type WireTemplateData struct {
    Module         string         // "orders-service"
    Imports        []string
    InfraProviders []ProviderData // DB, Redis, Kafka constructors
    TableProviders []ProviderData // one per table
    CacheProviders []ProviderData
    ExternalProviders []ProviderData
    MessagingProviders []ProviderData
    TxProviders    []ProviderData
    AuthProviders  []ProviderData
    APIServices    []ProviderData // one per API
    AppProvider    ProviderData
    RouterProvider ProviderData
}
type ProviderData struct {
    ConstructorFunc string        // "users.NewUsersRepository"
    Comment         string        // "// table: users"
}
```

---

## Rules for Writing Templates

1. **First line is always** `// Code generated by stencil. DO NOT EDIT.`

2. **Second line is always** `// Source: stencil.yaml → {block}[{name}]`

3. **Package declarations** use `{{ .PackageName }}` — never hardcode

4. **Import blocks** use `{{ range .Imports }}"{{ . }}"{{ end }}` — never add imports manually in template. The generator's ImportCollector provides the exact set needed.

5. **Comments on generated code** explain WHY, not WHAT. Reader already sees the what.

6. **Interface + Default pattern** — every generated concrete type has:
   - An interface (`UsersRepository`)  
   - A default implementation (`DefaultUsersRepository`)
   - A constructor returning the interface (`func New... → Interface`)
   - The constructor is what gets registered in wire.go

7. **Error wrapping** — always `fmt.Errorf("{package}.{func}: %w", err)` for context

8. **No panic in generated code** — return errors. The container panics at startup for missing deps (that is its job), but generated service/repo/handler code never panics.

9. **Context propagation** — every function that touches IO takes `ctx context.Context` as first param

10. **Nil safety** — check pointers before use. Generated code is called by developer hooks that may pass unexpected nil values.

11. **The service executor template has zero if/else** — all branching is expressed via the pre-rendered `Steps []ServiceStepData`. Each step is a flag check + infra call + hook call. The template mechanically renders them in order.

12. **Private fields** — fields with `private: true` in spec get `json:"-"` tag and must never appear in any DTO type. Enforced at Validator level before generation — but also honour it in templates.

---

## Example: What CreateUser Generates End-to-End

**Input spec section:**
```yaml
- name: CreateUser
  method: POST
  path:   /
  auth:   public
  steps:
    - id: writeUser
      touch: {table: users, op: create}
      flag:    RunTableUsersCreate
      default: true
      fatal:   true
    - id: publishEvent
      touch: {message: UserCreated}
      flag:    RunMessageUserCreated
      default: true
      fatal:   false
```

**generated/apis/createuser/context.go:**
```go
// Code generated by stencil. DO NOT EDIT.
// Source: stencil.yaml → resources[UserAPIs].apis[CreateUser]

package createuser

import (
    "orders-service/generated/tables/users"
    "orders-service/generated/messaging"
)

type CreateUserContext struct {
    Request                 CreateUserRequest

    RunTableUsersCreate     bool  // default: true
    RunMessageUserCreated   bool  // default: true

    WriteUserInput          *WriteUserInput
    WriteUserOutput         *WriteUserOutput
    WriteUserError          error

    PublishEventInput       *PublishEventInput
    PublishEventOutput      *PublishEventOutput

    Response                *UserResponse
}

func NewCreateUserContext(req CreateUserRequest) *CreateUserContext {
    return &CreateUserContext{
        Request:               req,
        RunTableUsersCreate:   true,
        RunMessageUserCreated: true,
    }
}
```

**generated/apis/createuser/service.go (executor — the critical file):**
```go
// Code generated by stencil. DO NOT EDIT.
// Source: stencil.yaml → resources[UserAPIs].apis[CreateUser]

package createuser

import (
    "context"
    "fmt"
    "orders-service/generated/tables/users"
    "orders-service/generated/messaging"
    stencilerrors "github.com/stencil-run/stencil-go/errors"
)

type CreateUserService struct {
    usersRepo  users.UsersRepository
    producer   messaging.Producer
    mappers    CreateUserMappers
    hooks      *CreateUserHooks
    validator  *validator.Validate
}

func NewCreateUserService(
    usersRepo  users.UsersRepository,
    producer   messaging.Producer,
    mappers    CreateUserMappers,
    hooks      *CreateUserHooks,
) *CreateUserService {
    return &CreateUserService{
        usersRepo: usersRepo,
        producer:  producer,
        mappers:   mappers,
        hooks:     hooks,
        validator: validator.New(),
    }
}

func (s *CreateUserService) CreateUser(
    ctx context.Context,
    req CreateUserRequest,
) (*UserResponse, error) {
    // Validate request struct tags
    if err := s.validator.StructCtx(ctx, req); err != nil {
        return nil, stencilerrors.MapValidationError(err)
    }

    // Initialise shared context — flags set to spec defaults
    shared := NewCreateUserContext(req)

    // HOOK: BeforeCreateUser — entry point, set flags, mutate request
    if s.hooks.BeforeCreateUser != nil {
        if err := s.hooks.BeforeCreateUser(ctx, shared); err != nil {
            return nil, err
        }
    }

    // STEP: writeUser — table users create (fatal: true)
    if shared.RunTableUsersCreate {
        input, err := s.mappers.MapWriteUserInput(ctx, shared)
        if err != nil {
            return nil, fmt.Errorf("CreateUser.MapWriteUserInput: %w", err)
        }
        shared.WriteUserInput = input

        result, err := s.usersRepo.CreateUser(ctx, users.CreateUserParams{
            FirstName:    input.FirstName,
            LastName:     input.LastName,
            Email:        input.Email,
            PasswordHash: input.PasswordHash,
        })
        shared.WriteUserOutput = &WriteUserOutput{User: result}
        shared.WriteUserError = err
        if err != nil {
            return nil, fmt.Errorf("CreateUser.writeUser: %w", err)
        }

        // HOOK: AfterTableUsersCreate
        if s.hooks.AfterTableUsersCreate != nil {
            if err := s.hooks.AfterTableUsersCreate(ctx, shared); err != nil {
                return nil, err
            }
        }
    }

    // STEP: publishEvent — message UserCreated (fatal: false)
    if shared.RunMessageUserCreated {
        input, err := s.mappers.MapPublishEventInput(ctx, shared)
        if err != nil {
            return nil, fmt.Errorf("CreateUser.MapPublishEventInput: %w", err)
        }
        shared.PublishEventInput = input

        pubErr := s.producer.PublishUserCreated(ctx, messaging.UserCreated{
            UserID:    input.UserID,
            Email:     input.Email,
            FirstName: input.FirstName,
        })
        shared.PublishEventOutput = &PublishEventOutput{}
        // non-fatal: store error, let hook decide
        if pubErr != nil {
            shared.WriteUserError = pubErr  // note: reuse or add dedicated field
        }

        // HOOK: AfterMessageUserCreated
        if s.hooks.AfterMessageUserCreated != nil {
            if err := s.hooks.AfterMessageUserCreated(ctx, shared); err != nil {
                return nil, err
            }
        }
    }

    // HOOK: BeforeResponse — developer builds shared.Response
    if s.hooks.BeforeResponse != nil {
        if err := s.hooks.BeforeResponse(ctx, shared); err != nil {
            return nil, err
        }
    }

    // Fallback: build response from step output if hook did not set it
    if shared.Response == nil && shared.WriteUserOutput != nil {
        resp, err := s.mappers.MapResponse(ctx, shared)
        if err != nil {
            return nil, fmt.Errorf("CreateUser.MapResponse: %w", err)
        }
        shared.Response = resp
    }

    return shared.Response, nil
}
```

---

## Your Task

Generate production-ready `.go.tmpl` files for the template listed in the task. The template must:

1. Produce valid, idiomatic, compilable Go code when rendered
2. Have the exact package name, imports, and structure shown above
3. Use only the template data fields documented in the Template Data Structs section above
4. Follow all Rules for Writing Templates
5. Be complete — no placeholder comments like `// TODO` or `// implement this`
6. Handle edge cases: optional fields, nil checks, empty slices
7. Be consistent with the examples shown above

The generated output should look like code a senior Go engineer wrote by hand — not code that was obviously generated. Variable names, error messages, and comments should all be thoughtful and precise.

---

## Specific Templates to Generate

[INSERT THE SPECIFIC TEMPLATE FILE YOU WANT HERE — e.g. "templates/go/api/service.go.tmpl" or "templates/go/table/repository_gorm.go.tmpl"]

Reference the template data struct documented above for that file. Use the CreateUser end-to-end example as a style guide.
