# Complete File Structure for Implementation
## Phase 12: Validator + Phase 13: Resolver

---

## PHASE 12: GRAPH-BASED VALIDATOR

### Core Files to Create

```
internal/spec/validator/
├── graph.go                          [NEW] Node, Edge, EntityKind definitions
├── validator.go                      [MODIFY] Main orchestrator
├── resolve.go                        [NEW] Core resolveNode() algorithm
├── rules_table.go                    [NEW] Table validation & state machines
├── rules_type.go                     [NEW] Type validation & circular checking
├── rules_api.go                      [NEW] API & touch validation
├── rules_infrastructure.go           [NEW] External, cache, message, transaction
├── error_reporting.go                [NEW] Error accumulation & deduplication
├── validator_test.go                 [NEW] Comprehensive unit tests
└── integration_test.go               [NEW] Complex spec testing
```

### Test Data Files

```
testdata/validator/
├── golden/
│   ├── circular_types/
│   │   ├── spec.yaml
│   │   └── errors.json.golden
│   ├── fk_to_missing_table/
│   │   ├── spec.yaml
│   │   └── errors.json.golden
│   ├── api_missing_table_touch/
│   │   ├── spec.yaml
│   │   └── errors.json.golden
│   ├── state_machine_unreachable/
│   │   ├── spec.yaml
│   │   └── errors.json.golden
│   └── complex_dependency_chain/
│       ├── spec.yaml
│       └── graph.json.golden
└── integration/
    ├── orders_service_complete.yaml
    ├── user_service_with_auth.yaml
    └── messaging_cache_example.yaml
```

### Files to Modify

```
internal/spec/
├── ast.go                            [MODIFY] Add Index field to nodes for error paths
├── resolved.go                       [MODIFY] Ensure all fields match new requirements
└── parser.go                         [No change - parser stays as is]
```

---

## PHASE 13: ADVANCED SEMANTIC RESOLVER

### Core Files to Create

#### Resolver Orchestration
```
internal/spec/resolver/
├── resolver.go                       [NEW] Main Resolver orchestrator (5 stages)
├── stages.go                         [NEW] Stage implementations (build, link, etc.)
└── resolver_test.go                 [NEW] Integration tests
```

#### Pillar 1: Type Mapper
```
internal/spec/resolver/mapper/
├── mapper.go                         [NEW] TypeMapper main implementation
├── primitives.go                     [NEW] Type mapping tables (Stencil → Go/Java/SQL)
├── types.go                          [NEW] GoType struct and helpers
├── mapper_test.go                    [NEW] Type mapping unit tests
└── testdata/
    ├── primitives_table.yaml         [Config for testing all type combinations]
    └── custom_types.yaml
```

#### Pillar 2: Reference Linker
```
internal/spec/resolver/linker/
├── linker.go                         [NEW] Reference linker main
├── cross_link.go                     [NEW] Cross-entity linking logic
├── linker_test.go                    [NEW] Unit tests
└── testdata/
    ├── simple_reference.yaml
    ├── circular_reference.yaml
    └── deep_dependency_chain.yaml
```

#### Pillar 3: Query Expander (ORM-Specific)
```
internal/spec/resolver/query/
├── expander.go                       [NEW] QueryExpander interface
├── factory.go                        [NEW] Expander factory (per ORM)
│
├── gorm_expander.go                  [NEW] GORM-specific (method chains)
├── gorm_patterns.go                  [NEW] GORM-specific pattern builders
├── gorm_expander_test.go             [NEW] GORM tests
│
├── sqlc_expander.go                  [NEW] SQLC-specific (query names)
├── sqlc_expander_test.go             [NEW] SQLC tests
│
├── raw_sql_expander.go               [NEW] Raw SQL (full SQL statements)
├── sql_builder.go                    [NEW] SQL building utilities
├── raw_sql_expander_test.go          [NEW] Raw SQL tests
│
└── testdata/
    ├── find_by_single_field.yaml
    ├── find_by_multiple_fields.yaml
    ├── count_by_field.yaml
    ├── paginate_with_cursor.yaml
    └── custom_query.yaml
```

#### Pillar 4: Context Engine
```
internal/spec/resolver/context/
├── engine.go                         [NEW] ContextEngine main
├── field_builder.go                  [NEW] Field creation logic
├── signature_builder.go              [NEW] Go/Java signature generation
├── context_test.go                   [NEW] Unit tests
└── testdata/
    ├── simple_api.yaml
    ├── multi_touch_api.yaml
    └── complex_api_with_state.yaml
```

#### Pillar 5: Subsystem Linker
```
internal/spec/resolver/subsystems/
├── messaging.go                      [NEW] Messaging resolution
├── cache.go                          [NEW] Cache resolution
├── auth.go                           [NEW] Auth resolution
├── transaction.go                    [NEW] Transaction resolution
├── subsystems_test.go                [NEW] Integration tests
└── testdata/
    ├── kafka_messaging.yaml
    ├── redis_cache.yaml
    ├── jwt_auth.yaml
    └── transaction_example.yaml
```

### Test Data Files

```
testdata/resolver/
├── golden/
│   ├── type_mapping/
│   │   ├── input.yaml
│   │   └── output.json.golden
│   │
│   ├── query_expansion/
│   │   ├── gorm_find_by.yaml
│   │   ├── gorm_find_by.golden.json
│   │   ├── sqlc_find_by.yaml
│   │   ├── sqlc_find_by.golden.json
│   │   ├── raw_sql_find_by.yaml
│   │   └── raw_sql_find_by.golden.json
│   │
│   ├── context_building/
│   │   ├── simple_api.yaml
│   │   └── simple_api.golden.json
│   │
│   ├── subsystem_resolution/
│   │   ├── messaging_kafka.yaml
│   │   ├── messaging_kafka.golden.json
│   │   ├── cache_redis.yaml
│   │   └── cache_redis.golden.json
│   │
│   └── end_to_end/
│       ├── orders_service/
│       │   ├── spec.yaml
│       │   └── resolved.json.golden
│       └── user_service/
│           ├── spec.yaml
│           └── resolved.json.golden
│
└── integration/
    ├── gorm_postgres.yaml
    ├── sqlc_postgres.yaml
    ├── raw_sql_mysql.yaml
    └── multiple_orm_same_spec.yaml
```

### Files to Modify

```
internal/spec/
├── resolved.go                       [MODIFY] Update with new ResolvedSpec structure
│                                              Add ResolvedMessaging, ResolvedCache (not AST!)
│                                              Add new types for queries, context
│
├── spec_types.go                     [NEW] Organize all resolved types
└── ast.go                           [No change needed]
```

---

## SUPPORTING FILES

### Configuration & Constants

```
internal/spec/resolver/
├── config.go                         [NEW] Configuration for resolvers
│                                            (ORM selection, framework, etc.)
└── constants.go                      [NEW] ORM and Framework constants
```

### Documentation Files

```
internal/spec/resolver/
├── README.md                         [NEW] Resolver architecture overview
└── IMPLEMENTATION.md                 [NEW] Step-by-step implementation guide
```

---

## EXISTING FILES TO UPDATE

```
cmd/stencil/
├── validate.go                       [MODIFY] Use new graph-based validator
├── generate.go                       [MODIFY] Pass ValidationResult to resolver

internal/spec/
├── spec.go                          [MODIFY] Add ValidationResult, ResolvedSpec updates
└── errors.go                        [MODIFY] Add new error codes

internal/generator/
├── generator.go                      [No change needed - interface stays same]
└── registry.go                       [No change needed]
```

---

## FILE CREATION CHECKLIST

### Phase 12: Validator (10 files)

- [ ] `internal/spec/validator/graph.go` - Node, Edge, EntityKind
- [ ] `internal/spec/validator/validator.go` - Main orchestrator
- [ ] `internal/spec/validator/resolve.go` - resolveNode() algorithm
- [ ] `internal/spec/validator/rules_table.go` - Table validation
- [ ] `internal/spec/validator/rules_type.go` - Type validation
- [ ] `internal/spec/validator/rules_api.go` - API validation
- [ ] `internal/spec/validator/rules_infrastructure.go` - Infra validation
- [ ] `internal/spec/validator/error_reporting.go` - Error accumulation
- [ ] `internal/spec/validator/validator_test.go` - Tests
- [ ] `internal/spec/validator/integration_test.go` - Integration tests

### Phase 13: Resolver (25+ files)

**Core Orchestration:**
- [ ] `internal/spec/resolver/resolver.go` - Main orchestrator
- [ ] `internal/spec/resolver/stages.go` - 5 stages

**Pillar 1: Type Mapper (4 files)**
- [ ] `internal/spec/resolver/mapper/mapper.go`
- [ ] `internal/spec/resolver/mapper/primitives.go`
- [ ] `internal/spec/resolver/mapper/types.go`
- [ ] `internal/spec/resolver/mapper/mapper_test.go`

**Pillar 2: Reference Linker (3 files)**
- [ ] `internal/spec/resolver/linker/linker.go`
- [ ] `internal/spec/resolver/linker/cross_link.go`
- [ ] `internal/spec/resolver/linker/linker_test.go`

**Pillar 3: Query Expander (9 files)**
- [ ] `internal/spec/resolver/query/expander.go`
- [ ] `internal/spec/resolver/query/factory.go`
- [ ] `internal/spec/resolver/query/gorm_expander.go`
- [ ] `internal/spec/resolver/query/gorm_patterns.go`
- [ ] `internal/spec/resolver/query/gorm_expander_test.go`
- [ ] `internal/spec/resolver/query/sqlc_expander.go`
- [ ] `internal/spec/resolver/query/sqlc_expander_test.go`
- [ ] `internal/spec/resolver/query/raw_sql_expander.go`
- [ ] `internal/spec/resolver/query/raw_sql_expander_test.go`

**Pillar 4: Context Engine (3 files)**
- [ ] `internal/spec/resolver/context/engine.go`
- [ ] `internal/spec/resolver/context/field_builder.go`
- [ ] `internal/spec/resolver/context/context_test.go`

**Pillar 5: Subsystem Linker (5 files)**
- [ ] `internal/spec/resolver/subsystems/messaging.go`
- [ ] `internal/spec/resolver/subsystems/cache.go`
- [ ] `internal/spec/resolver/subsystems/auth.go`
- [ ] `internal/spec/resolver/subsystems/transaction.go`
- [ ] `internal/spec/resolver/subsystems/subsystems_test.go`

**Supporting Files:**
- [ ] `internal/spec/resolver/config.go`
- [ ] `internal/spec/resolver/constants.go`
- [ ] `internal/spec/resolver/README.md`

---

## DIRECTORY STRUCTURE (After Implementation)

```
stencil/
├── internal/
│   └── spec/
│       ├── validator/                    ← Phase 12 (10 files)
│       │   ├── graph.go
│       │   ├── validator.go
│       │   ├── resolve.go
│       │   ├── rules_table.go
│       │   ├── rules_type.go
│       │   ├── rules_api.go
│       │   ├── rules_infrastructure.go
│       │   ├── error_reporting.go
│       │   ├── validator_test.go
│       │   └── integration_test.go
│       │
│       └── resolver/                     ← Phase 13 (25+ files)
│           ├── resolver.go
│           ├── stages.go
│           ├── config.go
│           ├── constants.go
│           ├── README.md
│           │
│           ├── mapper/
│           │   ├── mapper.go
│           │   ├── primitives.go
│           │   ├── types.go
│           │   └── mapper_test.go
│           │
│           ├── linker/
│           │   ├── linker.go
│           │   ├── cross_link.go
│           │   └── linker_test.go
│           │
│           ├── query/
│           │   ├── expander.go
│           │   ├── factory.go
│           │   ├── gorm_expander.go
│           │   ├── gorm_patterns.go
│           │   ├── gorm_expander_test.go
│           │   ├── sqlc_expander.go
│           │   ├── sqlc_expander_test.go
│           │   ├── raw_sql_expander.go
│           │   └── raw_sql_expander_test.go
│           │
│           ├── context/
│           │   ├── engine.go
│           │   ├── field_builder.go
│           │   └── context_test.go
│           │
│           └── subsystems/
│               ├── messaging.go
│               ├── cache.go
│               ├── auth.go
│               ├── transaction.go
│               └── subsystems_test.go
│
└── testdata/
    ├── validator/                       ← Phase 12 test fixtures
    │   ├── golden/
    │   │   ├── circular_types/
    │   │   ├── fk_to_missing_table/
    │   │   ├── api_missing_table_touch/
    │   │   ├── state_machine_unreachable/
    │   │   └── complex_dependency_chain/
    │   └── integration/
    │
    └── resolver/                        ← Phase 13 test fixtures
        ├── golden/
        │   ├── type_mapping/
        │   ├── query_expansion/
        │   ├── context_building/
        │   ├── subsystem_resolution/
        │   └── end_to_end/
        └── integration/
```

---

## IMPLEMENTATION ORDER

### Week 1: Phase 12 Foundation
- Day 1-2: Create `graph.go`, `validator.go`, `resolve.go`
- Day 3: Create `rules_table.go`, `rules_type.go`
- Day 4: Create `rules_api.go`, `rules_infrastructure.go`
- Day 5: Create `error_reporting.go`, tests, golden files

### Week 2: Phase 13 Foundation + Parallel Work
- Day 1-2: Create `resolver.go`, `stages.go`, `mapper/` (4 files)
- Day 2-3: Create `linker/` (3 files) + `context/` (3 files)
- Day 3-4: Create `subsystems/` (5 files) - Parallel with above
- Day 4-5: Create `query/` expanders (9 files) - Can be parallel

### Week 3: Testing & Integration
- Day 1-2: Golden file tests for all pillars
- Day 3: Integration tests (complex specs)
- Day 4-5: Fix issues, refactor, documentation

---

## DEPENDENCY GRAPH

```
Phase 12:
  validator.go → graph.go, resolve.go
  resolve.go → rules_*.go
  rules_*.go → error_reporting.go

Phase 13:
  resolver.go → stages.go, mapper/, linker/, query/, context/, subsystems/
  
  stages.go → mapper/ + linker/ + query/ + context/ + subsystems/
  
  mapper/ (TYPE MAPPER - no dependencies)
  linker/ → mapper/ (for TypeMapper)
  query/ → mapper/ + linker/ (for both)
  context/ → mapper/ + linker/ + query/
  subsystems/ → mapper/ + linker/

Integration:
  cmd/stencil/validate.go → Phase 12 validator
  cmd/stencil/generate.go → Phase 13 resolver → Generators
```

---

## KEY POINTS FOR IMPLEMENTATION

1. **Phase 12 is independent** - Can be built and tested before Phase 13
2. **Phase 13 Pillars are parallelizable** - After TypeMapper is done
3. **Test data is crucial** - Golden files for each file
4. **ORM-specific code** - Query Expander has 3 variants (GORM, SQLC, Raw)
5. **No AST leakage** - Resolver's output must have zero AST pointers

---

## SUCCESS CHECKLIST

After Implementation:

- [ ] Phase 12: `ValidationResult` includes `Graph`
- [ ] Phase 13: No `MessagingAST` or `CacheAST` in `ResolvedSpec`
- [ ] All types are `GoType` structs or typed structs
- [ ] Every `ContextField` has exact `GoType`
- [ ] Every `ResolvedQuery` has ORM-specific expansion
- [ ] TypeMapper is single source of truth
- [ ] All golden file tests pass
- [ ] Integration tests with complex specs pass
- [ ] Generators work with new `ResolvedSpec`

