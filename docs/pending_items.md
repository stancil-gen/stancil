# Pending Items & Technical Debt

This document tracks features, validations, and capabilities described in the product/tech specifications that have been deliberately deferred for the initial MVP.

## Pending Validation Rules (Phase 4)

These rules should eventually be added to `internal/spec/validator.go` to provide comprehensive compile-time safety:

1. **Top-Level Configs**: Verify that every `${VAR}` used in a URL (e.g. inside `externals`) actually exists in the global `config:` block.
2. **State Machines**: If a table has a `states` machine defined, verify that the field it tracks is actually typed as an `enum`.
3. **Cyclic Dependencies**: Ensure Custom Types don't infinitely reference each other (e.g., Type A has a field of Type B, and Type B has a field of Type A).
4. **Extended Touch Scopes**: Expand the existing `CheckAPITouches` logic. Currently it validates `Table` and `External` touches, but it also needs to verify that `Cache`, `Message`, and `Transaction` touches map sequentially to real, defined infrastructure objects.
5. **DTO Safety Checks**: Inspect inline DTO definition fields to ensure they are not accidentally exposing/leaking database fields that are marked with `private: true`.
6. **Authentication & Roles**: If an API endpoint restricts access using `roles: [admin]`, ensure that `admin` is actually declared as a valid role in the global `auth:` block.

## Technical Debt

1. **Language Formatter Interface Strategy**: Currently, the code relies on hardcoded `if/else` checks based on `ast.Lang == "java"` to format flag/hook names (PascalCase vs camelCase). This does not scale. It must be refactored into an injectable interface strategy (`NameFormatter`) so new languages can be plugged into the framework without modifying the Core Resolver logic.

2. **Pointer Optimization for Custom Types**: All custom types are currently emitted as raw value types in every context (struct fields, DTOs, models). This avoids premature complexity but means large nested structs are copied by value. In a future pass, introduce pointer promotion for: (a) nullable custom-type fields (`nullable: true` → `*TypeName`), (b) large embedded structs in hot-path service code. The `MapType` function in `internal/spec/resolver/typemap.go` is the single place to make this change.
