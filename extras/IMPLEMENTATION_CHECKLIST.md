# Implementation Checklist

## Phase 12: Validator (10 Core Files)

### Week 1
- [ ] **Day 1-2: Foundation**
  - [ ] Create `internal/spec/validator/graph.go`
    - Node struct with Kind, Name, Index, Source
    - Edge struct with From, To, Kind, Path
    - EntityKind enum
    - DependencyKind enum
  
  - [ ] Create `internal/spec/validator/validator.go`
    - Validator struct
    - ValidateSpec() entry point
    - visitAll*() methods for each entity type
    - Tests for basic initialization

- [ ] **Day 3: Resolve Algorithm**
  - [ ] Create `internal/spec/validator/resolve.go`
    - resolveNode() core algorithm
    - visiting set for cycle detection
    - recordError() helper
    - Edge creation logic
    - Tests for resolveNode with simple cases

- [ ] **Day 4: Entity Rules Part 1**
  - [ ] Create `internal/spec/validator/rules_table.go`
    - Field type resolution
    - Foreign key validation
    - Enum validation
    - State machine BFS validation
    - Query name validation
    - Tests for each rule
  
  - [ ] Create `internal/spec/validator/rules_type.go`
    - Custom type field resolution
    - Circular type detection
    - Nullable handling
    - Tests for circular types

- [ ] **Day 5: Entity Rules Part 2 + Error Handling**
  - [ ] Create `internal/spec/validator/rules_api.go`
    - API request/response DTO resolution
    - Touch validation (table, external, message, cache, transaction)
    - Auth role validation
    - Owner field validation
    - Tests for each touch kind
  
  - [ ] Create `internal/spec/validator/rules_infrastructure.go`
    - External method validation
    - Cache interface validation
    - Message event type validation
    - Transaction validation
    - Tests for each subsystem
  
  - [ ] Create `internal/spec/validator/error_reporting.go`
    - recordError() implementation
    - Error deduplication
    - Cycle path reconstruction
    - Tests for error paths

- [ ] **Day 5 Evening: Initial Testing**
  - [ ] Create `internal/spec/validator/validator_test.go`
    - Unit tests for each rules file
    - Golden file tests (5-10 fixtures)
  
  - [ ] Create `testdata/validator/golden/` fixtures
    - circular_types/spec.yaml + errors.json.golden
    - fk_to_missing_table/spec.yaml + errors.json.golden
    - api_missing_table_touch/spec.yaml + errors.json.golden
    - state_machine_unreachable/spec.yaml + errors.json.golden
    - complex_dependency_chain/spec.yaml + graph.json.golden

---

## Phase 13: Resolver (25+ Files)

### Week 2

- [ ] **Day 1-2: Orchestration + Pillar 1 (TypeMapper)**
  - [ ] Create `internal/spec/resolver/resolver.go`
    - Resolver struct
    - Resolve() entry point
    - Five-stage pipeline orchestration
    - Tests for each stage
  
  - [ ] Create `internal/spec/resolver/mapper/mapper.go`
    - TypeMapper struct
    - MapType() main method
    - isPointerType(), isCustomType() helpers
    - Tests for type mapping
  
  - [ ] Create `internal/spec/resolver/mapper/primitives.go`
    - stencilToGo map (Postgres)
    - stencilToSQL map (Postgres, MySQL variants)
    - stencilToJava map
    - getPackage() for import generation
    - Tests for each type

- [ ] **Day 2-3: Pillar 2 (Reference Linker)**
  - [ ] Create `internal/spec/resolver/linker/linker.go`
    - Linker struct
    - LinkAll() orchestration
    - linkTables(), linkTypes(), linkExternals(), linkAPIs()
    - Tests for each entity kind
  
  - [ ] Create `internal/spec/resolver/linker/cross_link.go`
    - crossLinkTableFields() (field type refs)
    - crossLinkAPIs() (touch refs)
    - crossLinkExternals() (method refs)
    - Tests for reference resolution

- [ ] **Day 3-4: Pillar 4 (Context Engine) - Parallelizable**
  - [ ] Create `internal/spec/resolver/context/engine.go`
    - ContextEngine struct
    - BuildContext() for APIs
    - buildFlagName(), buildResultName(), buildErrorName()
    - Tests for context building
  
  - [ ] Create `internal/spec/resolver/context/field_builder.go`
    - buildRequestField()
    - buildFlagField()
    - buildResultField()
    - buildErrorField()
    - buildResponseField()
    - Tests for field creation

### Week 2-3: Parallel Work

- [ ] **Pillar 3a: Query Expander Factory**
  - [ ] Create `internal/spec/resolver/query/expander.go`
    - QueryExpander interface
    - ExpandQuery() signature
    - Tests for interface compliance
  
  - [ ] Create `internal/spec/resolver/query/factory.go`
    - QueryExpanderFactory struct
    - CreateExpander(lang, framework, orm)
    - Tests for factory
  
  - [ ] Create `internal/spec/resolver/query/gorm_expander.go`
    - GORMQueryExpander
    - expandFindBy(), expandExists(), expandCount(), expandPaginate()
    - buildGormPattern() for method chains
    - Tests for each shorthand
  
  - [ ] Create `internal/spec/resolver/query/gorm_patterns.go`
    - whereClause builder
    - parameter builder
    - methodChain builder
    - Helpers for GORM-specific patterns

- [ ] **Pillar 3b: SQLC Expander**
  - [ ] Create `internal/spec/resolver/query/sqlc_expander.go`
    - SQLCQueryExpander
    - expandFindBy(), expandExists(), expandCount()
    - SQL name mapping
    - Tests for SQLC patterns

- [ ] **Pillar 3c: Raw SQL Expander**
  - [ ] Create `internal/spec/resolver/query/raw_sql_expander.go`
    - RawSQLExpander
    - expandFindBy(), expandExists(), expandCount()
    - Full SQL generation
    - Parameter positioning
    - Tests for SQL generation
  
  - [ ] Create `internal/spec/resolver/query/sql_builder.go`
    - whereClause()
    - parameterList()
    - selectStatement()
    - countStatement()
    - Tests for SQL building

- [ ] **Pillar 5: Subsystem Linker**
  - [ ] Create `internal/spec/resolver/subsystems/messaging.go`
    - ResolveMessaging()
    - resolveTopics()
    - resolveConsumers(), resolveProducers()
    - Kafka/RabbitMQ metadata extraction
    - Tests for messaging
  
  - [ ] Create `internal/spec/resolver/subsystems/cache.go`
    - ResolveCache()
    - resolveCacheInterfaces()
    - Redis metadata extraction
    - Tests for cache
  
  - [ ] Create `internal/spec/resolver/subsystems/auth.go`
    - ResolveAuth()
    - resolveRoles(), resolvePermissions()
    - JWT/OAuth2 metadata extraction
    - Tests for auth
  
  - [ ] Create `internal/spec/resolver/subsystems/transaction.go`
    - ResolveTransaction()
    - Transaction metadata extraction
    - Tests for transactions

### Week 3

- [ ] **Orchestration + Integration**
  - [ ] Create `internal/spec/resolver/stages.go`
    - buildTypeRegistry() Stage 1
    - buildInfrastructure() Stage 2
    - expandQueries() Stage 3
    - buildSubsystems() Stage 4
    - buildAPIs() Stage 5
    - Tests for each stage
  
  - [ ] Create `internal/spec/resolver/config.go`
    - ResolverConfig struct
    - Load configuration
    - Tests for config loading
  
  - [ ] Create `internal/spec/resolver/constants.go`
    - Language, Framework, ORM enums
    - Constant definitions

- [ ] **Testing**
  - [ ] Create `internal/spec/resolver/resolver_test.go`
    - Unit tests for each pillar
    - Golden file tests
    - Integration tests
  
  - [ ] Create `testdata/resolver/golden/` fixtures
    - type_mapping/input.yaml + output.json.golden
    - query_expansion/ (GORM, SQLC, Raw variants)
    - context_building/simple_api.yaml + .golden
    - subsystem_resolution/ (messaging, cache, auth)
    - end_to_end/ (orders_service, user_service)
  
  - [ ] Create `testdata/resolver/integration/` test specs
    - gorm_postgres.yaml
    - sqlc_postgres.yaml
    - raw_sql_mysql.yaml
    - multiple_orm_same_spec.yaml

---

## File Dependencies

```
Phase 12 Dependencies:
  validator.go → graph.go, resolve.go
  resolve.go → rules_*.go
  rules_*.go → error_reporting.go
  All → validator_test.go

Phase 13 Dependencies:
  resolver.go → stages.go, mapper/, linker/, query/, context/, subsystems/
  
  mapper/ (NO DEPS) ← Start here
  linker/ → mapper/
  query/ → mapper/, linker/
  context/ → mapper/, linker/, query/
  subsystems/ → mapper/, linker/
```

---

## Success Criteria Checklist

### Phase 12 Complete
- [ ] `ValidationResult` includes `Graph`
- [ ] Cycles auto-detected via visiting set
- [ ] Error paths preserved in `Edge.Path`
- [ ] All golden files pass
- [ ] Complex spec validates correctly
- [ ] Zero re-traversal after graph built

### Phase 13 Complete
- [ ] No `MessagingAST` in `ResolvedSpec`
- [ ] No `CacheAST` in `ResolvedSpec`
- [ ] All types are `GoType` structs
- [ ] Every `ContextField` has exact `GoType`
- [ ] TypeMapper is single source of truth
- [ ] Query Expander works for GORM, SQLC, Raw
- [ ] All golden files pass
- [ ] Integration tests with real specs pass
- [ ] Generators 50% simpler (verified by LOC reduction)

---

## Daily Standups

### Week 1 (Phase 12)
- **Day 1 EOD:** graph.go, validator.go done. Tests running.
- **Day 2 EOD:** resolve.go algorithm verified. Basic cycle detection works.
- **Day 3 EOD:** rules_table.go, rules_type.go done. State machine validation ready.
- **Day 4 EOD:** rules_api.go, rules_infrastructure.go complete. All rules integrated.
- **Day 5 EOD:** error_reporting.go complete. Golden files passing (70%+).

### Week 2 (Phase 13 Start)
- **Day 1 EOD:** resolver.go, mapper.go, primitives.go done. Type mapping verified.
- **Day 2 EOD:** linker.go working. Types resolve to pointers correctly.
- **Day 3 EOD:** context/engine.go done. Rich context fields with exact types.
- **Day 4 EOD:** gorm_expander.go, sqlc_expander.go, raw_sql_expander.go done.
- **Day 5 EOD:** subsystems/ (messaging, cache, auth) all done.

### Week 3 (Phase 13 Complete)
- **Day 1 EOD:** stages.go orchestration working. All 5 stages integrated.
- **Day 2 EOD:** Golden files 90%+ passing. Integration tests ready.
- **Day 3 EOD:** All tests passing. Documentation complete.
- **Day 4-5:** Buffer for issues, refactoring, final verification.

---

## Quick Command Refs

```bash
# Run validator tests
go test ./internal/spec/validator/... -v

# Run resolver tests
go test ./internal/spec/resolver/... -v

# Update golden files
go test ./internal/spec/validator/... -update
go test ./internal/spec/resolver/... -update

# Build stencil with new validator/resolver
go build ./cmd/stencil

# Test end-to-end
./stencil validate testdata/integration_spec.yaml
./stencil generate testdata/integration_spec.yaml
```

