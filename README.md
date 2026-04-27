# Stancil

A deterministic Go backend code generation tool. Write one YAML spec describing your service — tables, APIs, external calls — and Stancil generates 100% of the structural Go code. You only write business logic in small hook functions.

**Why?** Structural backend code (models, repos, handlers, services, DI wiring) is mechanical and repetitive. Stancil generates all of it from a single spec file, leaving AI or developers to focus only on the business logic that actually matters.

---

## How It Works

```
stancil.yaml  →  stencil generate  →  generated/   (read-only, never edit)
                                   →  hooks/        (yours — write logic here)
```

Every API execution follows the same pattern:

```
Request → validate → shared context → hooks → step executor → hooks → response
```

The **shared context** is a typed struct that flows through every step of an API. The **step executor** (generated) reads from it and writes infra results back to it. Your **hook functions** inspect those results and control what happens next.

---

## Quick Start

**1. Install**

```bash
git clone https://github.com/stancil-gen/stancil
cd stancil
go build -o stencil ./cmd/stencil/main.go
```

**2. Write a spec**

```yaml
# stancil.yaml
version: 1
project: my-service
lang: go
framework: gin

config:
  - name: DATABASE_URL
    type: str
    required: true

db:
  - name: postgres
    driver: postgres
    framework: gorm
    url: ${DATABASE_URL}

tables:
  - name: tasks
    db: postgres
    fields:
      - name: title
        type: str
        required: true
      - name: done
        type: bool
        default: false
    errors:
      - code: NOT_FOUND
        name: NotFound

resources:
  - group: TaskAPIs
    base_path: /tasks
    auth: public
    apis:
      - name: CreateTask
        method: POST
        path: /
        dtos:
          request:
            name: CreateTaskRequest
            fields:
              - name: title
                type: str
                required: true
          response:
            name: CreateTaskResponse
            fields:
              - name: id
                type: uuid
              - name: title
                type: str
        steps:
          - id: writeTask
            touch: {table: tasks, op: create}
            fatal: true
```

**3. Generate**

```bash
stencil generate stancil.yaml
```

**4. Implement your hooks** (optional — only for business logic the generator can't infer)

```go
// hooks/task_hooks.go
func (h *CreateTaskHookImpl) BeforeResponse(ctx context.Context, shared *task_ap_is.CreateTaskContext) error {
    shared.Response = &task_ap_is.CreateTaskResponse{
        Id:    shared.WriteTaskOutput.ID,
        Title: shared.WriteTaskOutput.Title,
    }
    return nil
}
```

**5. Register and run**

```bash
go mod tidy
go run main.go
```

---

## The Spec Format

### Top-level structure

```yaml
version: 1
project: service-name     # Go module name
lang: go
framework: gin            # HTTP framework

config: [...]             # environment variables
db: [...]                 # database connections
types: [...]              # custom value objects (optional)
tables: [...]             # database tables
externals: [...]          # outbound HTTP clients (optional)
resources: [...]          # API endpoint groups
```

### `config` — Environment variables

```yaml
config:
  - name: DATABASE_URL    type: str    required: true
  - name: PORT            type: int    default: 8080
  - name: STRIPE_URL      type: str    required: true
```

### `db` — Database connections

```yaml
db:
  - name: postgres        # identifier used by tables
    driver: postgres      # postgres | mysql | mongo
    framework: gorm       # gorm | sqlx | sqlc (default: gorm)
    url: ${DATABASE_URL}  # must reference a declared config var
```

Multiple databases in one service are supported. Each gets a distinct named connection type in the generated code, avoiding DI collisions.

### `tables` — Database tables

```yaml
tables:
  - name: users
    db: postgres          # which database
    fields:
      - name: email       type: str    required: true    unique: true
      - name: name        type: str    required: true
      - name: password    type: str    required: true    private: true  # excluded from responses
      - name: status      type: enum   values: [active, suspended]    default: active
    queries:
      - find_by: [email]            # generates FindByEmail(ctx, email)
      - paginate: offset            # generates Paginate(ctx, page, limit) → OffsetPage[T]
      - soft_delete: true           # generates SoftDelete(ctx, id)
    errors:
      - code: NOT_FOUND    name: NotFound
      - code: EMAIL_TAKEN  name: EmailTaken
```

**Auto-generated fields** (always present, do not declare): `id` (UUID primary key), `created_at`, `updated_at`.

**Field types:** `str`, `int`, `uuid`, `bool`, `decimal`, `timestamp`, `date`, `json`, `enum`

### `externals` — Outbound HTTP clients

```yaml
externals:
  - name: StripeClient
    type: http
    base_url: ${STRIPE_URL}
    auth: bearer_token        # bearer_token | api_key | none
    timeout: 10s
    retry:
      attempts: 3
      backoff: exponential    # exponential | linear | none
      on_status: [429, 502, 503]
    calls:
      - name: ChargeCard
        method: POST
        path: /v1/charges
        body:
          name: ChargeRequest
          fields:
            - name: amount      type: int    required: true
            - name: currency    type: str    required: true
        response:
          name: ChargeResponse
          fields:
            - name: id          type: str
            - name: status      type: str
        errors:
          - status: 402    error: CardDeclined
```

### `resources` — API endpoint groups

```yaml
resources:
  - group: OrderAPIs
    base_path: /orders
    auth: public

    apis:
      - name: PlaceOrder
        method: POST
        path: /
        dtos:
          request:
            name: PlaceOrderRequest
            fields:
              - name: user_id         type: uuid    required: true
              - name: payment_token   type: str     required: true
          response:
            name: PlaceOrderResponse
            fields:
              - name: order_id        type: uuid
              - name: charge_id       type: str
        steps:
          - id: chargePayment
            touch: {external: StripeClient, method: ChargeCard}
            fatal: true          # abort on failure
          - id: writeOrder
            touch: {table: orders, op: create}
            fatal: true
```

**Step `touch` options:**

```yaml
touch: {table: users,        op: create}   # op: create | get | update | delete | list
touch: {external: StripeClient, method: ChargeCard}
```

---

## Generated Output

```
generated/                    ← read-only, wiped and rewritten on every generate
  config/config.go            ← typed AppConfig struct + env loader
  db/db.go                    ← database connection(s) with named types
  tables/
    users/
      model.go                ← User struct, enums, error vars
      repository.go           ← UsersRepository interface + GORM implementation
  externals/
    stripe_client.go          ← StripeClient interface + HTTP implementation
  apis/
    order_ap_is/
      dto.go                  ← PlaceOrderRequest, PlaceOrderResponse
      context.go              ← PlaceOrderContext (shared state struct)
      hooks.go                ← PlaceOrderHooks (your extension points)
      mappers.go              ← DefaultPlaceOrderMappers
      service.go              ← PlaceOrderServiceImpl (step executor)
      handler.go              ← HTTP handler (bind → service → respond)
  routes.go                   ← all route registrations
  wire.go                     ← DI container wiring

hooks/                        ← yours — never touched by the generator
  register.go                 ← wire your hook implementations here

main.go                       ← generated once, then yours to own
go.mod                        ← generated once, then yours to own
```

---

## Hooks — Writing Business Logic

Every API has a generated `Hooks` struct with optional function fields. Set only the ones you need; `nil` fields are skipped by the executor.

```go
type PlaceOrderHooks struct {
    BeforePlaceOrder             func(ctx context.Context, shared *PlaceOrderContext) error
    BeforeStripeClientChargeCard func(ctx context.Context, shared *PlaceOrderContext) error
    AfterStripeClientChargeCard  func(ctx context.Context, shared *PlaceOrderContext) error
    BeforeTableOrdersCreate      func(ctx context.Context, shared *PlaceOrderContext) error
    AfterTableOrdersCreate       func(ctx context.Context, shared *PlaceOrderContext) error
    BeforeResponse               func(ctx context.Context, shared *PlaceOrderContext) error
}
```

The shared context carries everything:

```go
type PlaceOrderContext struct {
    Request              *PlaceOrderRequest

    // Step inputs — set by mappers before each infra call
    ChargePaymentInput   externals.ChargeCardInput
    WriteOrderInput      *orders.Order

    // Step outputs — populated by generated code after each infra call
    ChargePaymentOutput  *externals.ChargeCardResponse
    WriteOrderOutput     *orders.Order

    // Response — set by your BeforeResponse hook
    Response             *PlaceOrderResponse
}
```

**Example — hash password before DB write:**

```go
func (h *Impl) BeforeTableUsersCreate(ctx context.Context, shared *user_ap_is.CreateUserContext) error {
    hash := sha256.Sum256([]byte(shared.Request.Password))
    shared.WriteUserInput.PasswordHash = fmt.Sprintf("%x", hash)
    return nil
}
```

**Example — build response from two step outputs:**

```go
func (h *Impl) BeforeResponse(ctx context.Context, shared *order_ap_is.PlaceOrderContext) error {
    shared.Response = &order_ap_is.PlaceOrderResponse{
        OrderId:  shared.WriteOrderOutput.ID,
        ChargeId: shared.ChargePaymentOutput.Id,   // from Stripe
        Status:   string(shared.WriteOrderOutput.Status),
    }
    return nil
}
```

**Register hooks in `hooks/register.go`:**

```go
func Register(c *container.Container) {
    c.Provide(func() *order_ap_is.PlaceOrderHooks {
        impl := &PlaceOrderHookImpl{}
        return &order_ap_is.PlaceOrderHooks{
            BeforePlaceOrder:        impl.BeforePlaceOrder,
            AfterStripeClientChargeCard: impl.AfterStripeClientChargeCard,
            BeforeResponse:          impl.BeforeResponse,
        }
    })
}
```

---

## CLI Commands

```bash
stencil generate stancil.yaml           # generate all files from spec
stencil generate stancil.yaml --force   # force regenerate even if spec unchanged
stencil validate stancil.yaml           # parse and validate spec, report all errors
stencil diff stancil.yaml               # show what would change without writing
stencil parse stancil.yaml              # print raw parsed AST (debug)
stencil resolve stancil.yaml            # print resolved IR (debug)
stencil plan stancil.yaml               # print DAG execution plan (debug)
```

---

## Validation

Stancil validates the spec before generating anything. All errors are reported at once (never stops at the first one).

| Error code | Meaning |
|---|---|
| `MISSING_DB_BLOCK` | Tables defined but no `db:` block |
| `DB_UNKNOWN_URL_VAR` | `url: ${VAR}` references an undeclared config var |
| `DB_DUPLICATE_NAME` | Two databases with the same name |
| `DB_UNKNOWN_DRIVER` | Driver not one of: postgres, mysql, mongo |
| `TABLE_AMBIGUOUS_DB` | Table has no `db:` field and multiple databases are declared |
| `TABLE_UNKNOWN_DB` | Table's `db:` references an undeclared database name |
| `MISSING_PROJECT` | `project:` field is missing |
| `MISSING_RESOURCES` | No API resource groups defined |

---

## Runtime Library

Generated services depend on [`stancil-go`](https://github.com/stancil-gen/stancil-go) — a small Go library providing:

| Package | What it does |
|---|---|
| `container` | Reflect-based DI container with lifecycle hooks |
| `errors` | HTTP error types (`DomainError`, `ValidationError`) + Gin helpers |
| `graph` | DAG executor for ordered/parallel step orchestration |
| `handler` | `Bind[T]` for request parsing + `WriteCreated`, `WriteOK` helpers |
| `httputil` | HTTP retry with exponential/linear backoff |
| `pagination` | Generic `CursorPage[T]` and `OffsetPage[T]` types |

---

## Internal Architecture

The tool is a 6-stage pipeline:

```
stancil.yaml
  ↓ parse/       YAML → SpecAST
  ↓ validate/    SpecAST → []error
  ↓ resolve/     SpecAST → ResolvedSpec (fills defaults, derives interfaces)
  ↓ plan/        ResolvedSpec → GenerationPlan (DAG, parallel tiers)
  ↓ generate/    GenerationPlan → []File (template rendering)
  ↓ emit/        []File → disk (atomic write, chmod 0444/0555)
```

```
internal/
  spec/         ← AST + ResolvedSpec types
  parse/        ← stage 1
  validate/     ← stage 2
  resolve/      ← stage 3
  plan/         ← stage 4
  generate/     ← stage 5 core (Generator interface, Orchestrator, Registry)
  codegen/go/   ← stage 5 implementations (all Go generators)
  emit/         ← stage 6
  lang/         ← language pack interface + Go implementation
  template/     ← text/template engine with custom FuncMap
  diff/         ← lock file + diff display
```

---

## Contributing

The cleanest entry point for adding a new generator:

1. Implement `generate.Generator` interface (one `ID()` and one `Generate()` method)
2. Add a template file in `templates/go/`
3. Register in `cmd/stencil/main.go`
4. Add it to the right DAG tier in `plan/planner.go`

To add a new language target, implement `lang.LangPack` and add a case in `generate/orchestrator.go`.
