# ResolvedImplementation — The Missing Four
## RepositoryImpl, CacheImpl, ExternalImpl, TransactionImpl

---

## The Gap

The previous doc defined `ResolvedImplementation` and gave a full body model for `ServiceImpl` — the touch list, the flag fields, the shared context, the per-method files. But it skipped the other four kinds. They were listed in the enum and the mapping table but never fleshed out.

This doc fills that gap. The question for each kind is the same: **what information does the Resolver need to put into `ResolvedMethod.Touches` so the Generator can write the body without making any decisions?**

---

## The Pattern They All Share

Before going kind by kind, notice that all four follow a simpler version of `ServiceImpl`. `ServiceImpl` is complex because it has flags, shared context, conditional execution, hooks. The other four don't. Their bodies are mostly linear: take inputs, call one thing, return result or error.

That means `ResolvedMethod` for all four looks like this:

```go
type ResolvedMethod struct {
    FunctionName string

    // For the other four kinds, Touches has exactly one entry per method
    // (except TransactionImpl which has one entry per step).
    // ServiceImpl is the only one with multiple touches per method.
    Touches []ResolvedTouch

    // SharedContext is ServiceImpl-only. Nil for all other kinds.
    SharedContext *ResolvedObject
}
```

The generator reads `len(Touches)` and `Touches[0].Kind` to know what to render. Simple.

---

## RepositoryImpl

### What it is

The concrete implementation of the `RepositoryInterface`. One method per declared query. The generator writes the actual DB call — ORM-specific or raw SQL depending on config.

### Where it comes from

```
TableAST
  queries:
    - find_by: [email]          → one ResolvedMethod{FunctionName: "GetUserByEmail", ...}
    - find_by: [status]         → one ResolvedMethod{FunctionName: "GetUsersByStatus", ...}
    - exists: [email]           → one ResolvedMethod{FunctionName: "UserExistsByEmail", ...}
    - paginate: cursor          → one ResolvedMethod{FunctionName: "ListUsers", ...}
    - soft_delete: true         → one ResolvedMethod{FunctionName: "SoftDeleteUser", ...}
    - bulk_create: true         → one ResolvedMethod{FunctionName: "BatchCreateUsers", ...}
    - custom: GetWithOrderCount → one ResolvedMethod{FunctionName: "GetWithOrderCount", ...}
  + always generated:
    - create                    → one ResolvedMethod{FunctionName: "CreateUser", ...}
    - get by PK                 → one ResolvedMethod{FunctionName: "GetUserByID", ...}
    - update                    → one ResolvedMethod{FunctionName: "UpdateUser", ...}
    - delete                    → one ResolvedMethod{FunctionName: "DeleteUser", ...}
```

### ResolvedTouch for RepositoryImpl

Each method has exactly one touch of kind `Query`:

```go
ResolvedTouch{
    Kind: TouchKindQuery

    // The table being queried
    TableRef *ResolvedObject   // the User TableModel

    // What kind of query this is
    QueryKind QueryKind        // FindBy | Exists | Count | Paginate | SoftDelete | BulkCreate | Custom | Create | Get | Update | Delete

    // For FindBy: which fields are the WHERE clause
    FilterFields []ResolvedField   // the resolved fields from the table

    // For FindBy: which comparison operator
    Op string   // "eq", "gte", "lte", "like", "in", "between"

    // For Paginate: cursor or offset, which field, which direction
    PaginationKind string      // "cursor" | "offset"
    OrderByField  *ResolvedField
    OrderDir      string       // "asc" | "desc"
    DefaultLimit  int

    // For Custom: the raw SQL string and its named params
    CustomSQL    string
    CustomParams []ResolvedParam   // typed params for the custom query
    ReturnsMany  bool

    // For BulkCreate: nothing extra — generator knows it's a batch from QueryKind

    // Error condition: if rows affected == N, return this error
    // Used by custom queries that declare error_if_rows
    ErrorIfRows *int    // nil means no error condition
    ErrorName   string  // "UserNotFound", "InsufficientStock"
}
```

### What the Generator does with this

The generator reads `Touch.QueryKind` and `Touch.Op` and renders the appropriate ORM call. For GORM:

```
FindBy [email] eq  →  db.Where("email = ?", email).First(&result)
FindBy [status] gte →  db.Where("status >= ?", status).Find(&result)
Exists [email]     →  db.Where("email = ?", email).Select("1").First(&dummy)
Paginate cursor    →  db.Order("created_at DESC").Limit(limit+1).Find(&results)
SoftDelete         →  db.Where("id = ?", id).Update("deleted_at", time.Now())
BulkCreate         →  db.CreateInBatches(users, 100)
Create             →  db.Create(&user)
Get by PK          →  db.First(&user, id)
Update             →  db.Save(&user)
Delete             →  db.Delete(&user, id)
Custom             →  db.Raw(sql, params...).Scan(&result)
```

For raw SQL the generator renders `db.QueryRowContext(...)` or `db.ExecContext(...)` patterns. Same `ResolvedTouch` in both cases — only the generator template differs.

### Dependencies for RepositoryImpl

```go
// Always:
Dependencies: []ResolvedDependency{
    {FieldName: "db", TypeName: "*sql.DB", Import: "database/sql"},
}
// If soft_delete or any query that touches deleted_at:
// no extra dep — db handles it

// If observability is enabled (Phase 3 feature):
// {FieldName: "metrics", TypeName: "*observability.Metrics", ...}
```

### File layout

```
generated/repo/users/
  repo_impl.go       ← struct definition: UsersRepositoryImpl{db *sql.DB}
  get_user_by_id.go  ← GetUserByID method body
  create_user.go     ← CreateUser method body
  get_user_by_email.go
  list_users.go
  ...
```

The `Path` on `ResolvedImplementation` points to `repo_impl.go`. The generator derives per-method file paths from `FunctionName`.

---

## CacheImpl

### What it is

The concrete Redis-backed implementation of the `CacheInterface`. One method per declared operation in `cache.interfaces[*].methods`. The generated struct embeds `cache.BaseCache` from `stencil-go`, which handles the Redis client, key prefixing, serialization, and TTL.

### Where it comes from

```
CacheAST.Interface:
  name: UserCache
  key_template: "user:{id}"    ← drives key function derivation
  value_type: UserResponse      ← drives what Get returns, what Set takes
  default_ttl: 5m
  methods: [Get, Set, Delete, Invalidate]
```

### ResolvedTouch for CacheImpl

Each method has one touch of kind `CacheOp`:

```go
ResolvedTouch{
    Kind: TouchKindCacheOp

    // The cache interface being implemented
    CacheRef *ResolvedInterface   // the UserCache interface

    // Which operation
    CacheOp CacheOpKind   // Get | Set | Delete | Invalidate

    // The value type — what Get returns, what Set takes
    ValueTypeRef *ResolvedObject   // the UserResponse ResolvedObject

    // Key derivation — extracted from key_template "user:{id}"
    KeyParams []ResolvedParam      // [{Name: "id", Type: int64}]
    KeyTemplate string             // "user:{id}"
    KeyFunc string                 // pre-computed: fmt.Sprintf("user:%d", id)

    // TTL
    DefaultTTL time.Duration       // 5m parsed to time.Duration value
}
```

**Key function derivation** is the Resolver's job:

```
key_template: "user:{id}"
  → params: [{Name: "id", Type: field type of "id" in value_type object}]
  → KeyFunc: fmt.Sprintf("user:%d", id)    (Go)
             String.format("user:%d", id)  (Java)
```

The Resolver walks the `key_template`, extracts `{id}`, looks up `id` in the `value_type` object's fields, gets its `TypeDescriptor`, and derives the format verb (`%d` for int64, `%s` for string, `%s` for UUID with `.String()`). The generator writes `KeyFunc` verbatim.

### What the Generator does with this

```go
// Get:
func (c *UserCacheImpl) Get(ctx context.Context, id int64) (*UserResponse, bool, error) {
    key := fmt.Sprintf("user:%d", id)     // KeyFunc
    var val UserResponse
    hit, err := c.BaseCache.Get(ctx, key, &val)
    return &val, hit, err
}

// Set:
func (c *UserCacheImpl) Set(ctx context.Context, id int64, val *UserResponse, ttl time.Duration) error {
    key := fmt.Sprintf("user:%d", id)
    if ttl == 0 { ttl = 5 * time.Minute }   // DefaultTTL
    return c.BaseCache.Set(ctx, key, val, ttl)
}

// Delete:
func (c *UserCacheImpl) Delete(ctx context.Context, id int64) error {
    key := fmt.Sprintf("user:%d", id)
    return c.BaseCache.Delete(ctx, key)
}
```

The generator reads `CacheOp`, `KeyFunc`, `ValueTypeRef.Name`, `DefaultTTL`. No decisions needed.

### Dependencies for CacheImpl

```go
Dependencies: []ResolvedDependency{
    {FieldName: "BaseCache", TypeName: "cache.BaseCache", Import: "github.com/stencil-run/stencil-go/cache"},
}
// BaseCache is embedded, not a pointer field — the generator knows this from Kind==CacheImpl
```

Also generates a **mock** implementation. The mock's `ResolvedImplementation` has no `Touches` — it just implements the interface with in-memory map storage. The generator handles it from `Kind == CacheImpl + isMock flag` or a separate `Kind == CacheMockImpl`.

---

## ExternalImpl

### What it is

The concrete HTTP client implementation of the `ExternalInterface`. One method per declared call. The generated struct wraps a `*http.Client` with base URL, auth headers, retry logic, and timeout — all pre-configured. Also generates a mock implementation that records calls and returns stubbed responses.

### Where it comes from

```
ExternalAST:
  name: StripeClient
  base_url: ${STRIPE_URL}
  auth: bearer_token
  timeout: 10s
  attempts: 3
  backoff: exponential
  on_status: [429, 502, 503]   ← retry on these
  calls:
    - name: ChargeCard
      method: POST
      path: /v1/charges
      body: ChargeRequest          ← ExternalInput object
      response: ChargeResponse     ← ExternalOutput object
      on_status:
        - status: 402  error: CardDeclined
```

### ResolvedTouch for ExternalImpl

Each method has one touch of kind `HTTPCall`:

```go
ResolvedTouch{
    Kind: TouchKindHTTPCall

    // HTTP mechanics
    HTTPMethod string   // "POST", "GET", "PUT", "DELETE"
    PathTemplate string // "/v1/charges" or "/users/{id}" with params

    // Path params extracted from PathTemplate
    PathParams []ResolvedParam   // [{Name: "id", Type: int64}] if path has {id}

    // Request body — nil for GET/DELETE
    RequestBodyRef *ResolvedObject   // ChargeRequest ResolvedObject

    // Response body — nil if response: void
    ResponseBodyRef *ResolvedObject  // ChargeResponse ResolvedObject

    // Status code → domain error mappings
    StatusErrors []ResolvedStatusError   // [{Status: 402, ErrorName: "CardDeclined"}]

    // Retry config (resolved from ExternalAST)
    RetryAttempts int
    RetryBackoff  string   // "exponential" | "linear" | "none"
    RetryOnStatus []int    // [429, 502, 503]

    // Auth
    AuthKind string   // "bearer_token" | "api_key" | "none"

    // Timeout
    Timeout time.Duration   // 10s parsed
}
```

### What the Generator does with this

```go
func (c *StripeClientImpl) ChargeCard(ctx context.Context, req *ChargeRequest) (*ChargeResponse, error) {
    // body serialization
    body, err := json.Marshal(req)
    if err != nil { return nil, err }

    // build request
    httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/v1/charges", bytes.NewReader(body))
    if err != nil { return nil, err }
    httpReq.Header.Set("Authorization", "Bearer " + c.token)   // AuthKind=bearer_token
    httpReq.Header.Set("Content-Type", "application/json")

    // retry loop (RetryAttempts=3, RetryBackoff=exponential, RetryOnStatus=[429,502,503])
    var resp *http.Response
    for attempt := 0; attempt < 3; attempt++ {
        resp, err = c.client.Do(httpReq)
        if err == nil && !shouldRetry(resp.StatusCode, []int{429,502,503}) { break }
        if attempt < 2 { time.Sleep(exponentialBackoff(attempt)) }
    }
    if err != nil { return nil, err }

    // status error mapping
    switch resp.StatusCode {
    case 402: return nil, ErrCardDeclined     // from StatusErrors
    }
    if resp.StatusCode >= 400 { return nil, ErrStripeUnexpected }

    // decode response
    var result ChargeResponse
    if err := json.NewDecoder(resp.Body).Decode(&result); err != nil { return nil, err }
    return &result, nil
}
```

Everything in this body came from `ResolvedTouch` fields — no generator logic, only template substitution.

### Mock implementation

The mock is a separate `ResolvedImplementation` with `Kind == ExternalMockImpl`. It has no `Touches`. The generator writes a struct with a map of stubbed responses and a call recorder:

```go
type StripeClientMock struct {
    ChargeCardFunc func(ctx context.Context, req *ChargeRequest) (*ChargeResponse, error)
    ChargeCardCalls []*ChargeRequest
}
func (m *StripeClientMock) ChargeCard(ctx context.Context, req *ChargeRequest) (*ChargeResponse, error) {
    m.ChargeCardCalls = append(m.ChargeCardCalls, req)
    if m.ChargeCardFunc != nil { return m.ChargeCardFunc(ctx, req) }
    return nil, nil
}
```

The generator knows to produce this pattern for every function in the `ExternalInterface`. It reads the interface's `Functions` list and generates one `FuncField + CallsField + Method` per function. The Resolver doesn't need to pre-declare anything for the mock — the generator derives it entirely from the interface.

### Dependencies for ExternalImpl

```go
Dependencies: []ResolvedDependency{
    {FieldName: "client",  TypeName: "*http.Client",   Import: "net/http"},
    {FieldName: "baseURL", TypeName: "string"},
    {FieldName: "token",   TypeName: "string"},   // if auth: bearer_token
}
// Additional per-auth-kind:
// api_key → {FieldName: "apiKey", TypeName: "string"}
// none    → only client + baseURL
```

---

## TransactionImpl

### What it is

The concrete implementation of a named transaction. Wraps multiple SQL steps in a `BEGIN / COMMIT / ROLLBACK` using `tx.RunTx` from `stencil-go`. One `ResolvedMethod` — `Execute` — with one `ResolvedTouch` per declared step.

### Where it comes from

```
TransactionAST:
  name: PlaceOrder
  type: local
  steps:
    - name: CreateOrder
      sql: INSERT INTO orders (user_id, total, status) VALUES ($1, $2, 'pending') RETURNING id
      params: [{name: user_id, type: int}, {name: total, type: decimal}]
      error_if_rows: 0
      error: OrderCreationFailed

    - name: DeductInventory
      sql: UPDATE inventory SET quantity = quantity - $1 WHERE product_id = $2 AND quantity >= $1
      params: [{name: quantity, type: int}, {name: product_id, type: int}]
      error_if_rows: 0
      error: InsufficientStock
```

### ResolvedMethod for TransactionImpl

One method — `Execute` — with one touch per step:

```go
ResolvedMethod{
    FunctionName: "Execute",

    // params to Execute() — union of all step params, deduplicated
    // the Resolver merges them; generator renders them as Execute(ctx, params PlaceOrderParams)
    // where PlaceOrderParams is the TransactionParams ResolvedObject from Level 1
    InputParamsRef *ResolvedObject   // → PlaceOrderParams

    Touches: []ResolvedTouch{
        // Step 1
        {
            Kind: TouchKindTxStep

            StepName string   // "CreateOrder"

            // Raw SQL — stays as-is, generator writes it verbatim
            SQL string   // "INSERT INTO orders ..."

            // Typed params for this step (subset of Execute's full params)
            StepParams []ResolvedParam   // [{Name: "UserID", Type: int64}, {Name: "Total", Type: decimal.Decimal}]

            // The rows-affected error condition
            ErrorIfRows *int   // pointer to 0
            ErrorName   string // "OrderCreationFailed" — maps to a generated sentinel error

            // If RETURNING: what to scan into
            // nil if the step doesn't return anything
            ScanInto *ResolvedParam   // {Name: "orderID", Type: int64}
        },

        // Step 2
        {
            Kind: TouchKindTxStep
            StepName: "DeductInventory"
            SQL: "UPDATE inventory ..."
            StepParams: [{Name: "Quantity", Type: int64}, {Name: "ProductID", Type: int64}]
            ErrorIfRows: &zero
            ErrorName: "InsufficientStock"
            ScanInto: nil   // no RETURNING
        },
    }
}
```

### What the Generator does with this

```go
func (t *PlaceOrderTx) Execute(ctx context.Context, p PlaceOrderParams) (*PlaceOrderResult, error) {
    var result PlaceOrderResult
    err := tx.RunTx(ctx, t.db, func(tx *sql.Tx) error {

        // Step 1: CreateOrder
        row := tx.QueryRowContext(ctx,
            "INSERT INTO orders (user_id, total, status) VALUES ($1, $2, 'pending') RETURNING id",
            p.UserID, p.Total,
        )
        if err := row.Scan(&result.OrderID); err != nil {
            return ErrOrderCreationFailed   // ErrorIfRows == 0 and Scan failed
        }

        // Step 2: DeductInventory
        res, err := tx.ExecContext(ctx,
            "UPDATE inventory SET quantity = quantity - $1 WHERE product_id = $2 AND quantity >= $1",
            p.Quantity, p.ProductID,
        )
        if err != nil { return err }
        affected, _ := res.RowsAffected()
        if affected == 0 { return ErrInsufficientStock }   // ErrorIfRows == 0

        return nil
    })
    if err != nil { return nil, err }
    return &result, nil
}
```

The generator reads `Touches` in order, renders one block per touch, wraps all of them in `tx.RunTx`. Sentinel errors (`ErrOrderCreationFailed`) come from the table's `errors:` list — the generator refers to them by `ErrorName`.

### Dependencies for TransactionImpl

```go
Dependencies: []ResolvedDependency{
    {FieldName: "db", TypeName: "*sql.DB", Import: "database/sql"},
}
```

---

## Revised ResolvedTouch — Unified

Now that all four impl kinds are described, here is the complete `ResolvedTouch` that covers every use case:

```go
type ResolvedTouch struct {
    Kind TouchKind
    // TouchKind: Query | CacheOp | HTTPCall | TxStep | Publish
    //   (Publish is for messaging — producer publishes an event)
    //   (ServiceImpl uses all of them; the others use exactly one kind each)

    // ── Query (RepositoryImpl) ───────────────────────────────
    TableRef     *ResolvedObject   // the table model
    QueryKind    QueryKind         // FindBy | Exists | Count | Paginate | Create | Get | Update | Delete | SoftDelete | BulkCreate | Custom
    FilterFields []ResolvedField   // for FindBy/Exists/Count
    Op           string            // "eq" | "gte" | "like" | ...
    // Pagination
    PaginationKind string
    OrderByField  *ResolvedField
    OrderDir      string
    DefaultLimit  int
    // Custom query
    CustomSQL    string
    CustomParams []ResolvedParam
    ReturnsMany  bool
    // Error condition
    ErrorIfRows *int
    ErrorName   string
    // What to scan the RETURNING clause into
    ScanInto *ResolvedParam

    // ── CacheOp (CacheImpl) ─────────────────────────────────
    CacheRef     *ResolvedInterface
    CacheOp      CacheOpKind   // Get | Set | Delete | Invalidate
    ValueTypeRef *ResolvedObject
    KeyParams    []ResolvedParam
    KeyTemplate  string
    KeyFunc      string         // pre-computed format string
    DefaultTTL   time.Duration

    // ── HTTPCall (ExternalImpl) ──────────────────────────────
    HTTPMethod      string
    PathTemplate    string
    PathParams      []ResolvedParam
    RequestBodyRef  *ResolvedObject
    ResponseBodyRef *ResolvedObject
    StatusErrors    []ResolvedStatusError
    RetryAttempts   int
    RetryBackoff    string
    RetryOnStatus   []int
    AuthKind        string
    Timeout         time.Duration

    // ── TxStep (TransactionImpl) ─────────────────────────────
    StepName   string
    SQL        string
    StepParams []ResolvedParam

    // ── Publish (ServiceImpl — messaging touch) ───────────────
    MessageName  string
    EventTypeRef *ResolvedObject   // the event struct

    // ── ServiceImpl — control fields ─────────────────────────
    // These are populated only when this touch is part of a ServiceImpl method.
    // For all other impl kinds they are zero values.
    Flag        string
    Default     bool
    ResultField string
    ErrorField  string
    FatalError  bool

    // Resolved infra references for ServiceImpl dispatch
    // (so the generator knows which repo method to call for a table touch)
    QueryRef       *ResolvedFunction   // the specific repository function
    ExternalRef    *ResolvedInterface  // the external client interface
    ExternalMethod *ResolvedFunction   // the specific method on the external
    CacheMethod    *ResolvedFunction   // Get, Set, Delete, Invalidate
}

type ResolvedStatusError struct {
    Status    int
    ErrorName string   // "CardDeclined" → maps to generated sentinel error
}
```

---

## What Each Kind Puts Into Its Methods

| Impl Kind | Methods come from | Touches per method | Touch kind |
|---|---|---|---|
| `RepositoryImpl` | Table queries + CRUD | 1 | `Query` |
| `CacheImpl` | Cache interface `methods:` | 1 | `CacheOp` |
| `ExternalImpl` | External `calls:` | 1 | `HTTPCall` |
| `TransactionImpl` | One `Execute` method | N (one per step) | `TxStep` |
| `ServiceImpl` | Resource group APIs | N (one per touch in the API) | Mixed |

The Generator only needs to switch on `Touch.Kind` to know how to render each block. The rest is data substitution from the `ResolvedTouch` fields.
