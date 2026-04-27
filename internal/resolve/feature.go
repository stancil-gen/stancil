package resolve

import "stencil/internal/spec"

// Feature is implemented by each spec section (types, tables, externals, apis).
// Each feature owns its complete vertical slice:
//   - Level 1: objects (structs to declare)
//   - Level 2: interfaces (contracts to define)
//   - Level 3: implementations (concrete bodies to generate)
//
// Adding a new spec concept (e.g. webhooks) = one new file implementing this interface
// + one registration line in resolver.go.
type Feature interface {
	// Name identifies the feature in error messages and logs.
	Name() string

	// Validate runs feature-specific semantic checks that go beyond the global validator.
	// Called before any Resolve — all features validate first, then all features resolve.
	// Return all errors found; do not halt on the first error.
	Validate(ast *spec.SpecAST) []error

	// Resolve converts the feature's AST section into IR components.
	// Appends to ir.Objects, ir.Interfaces, ir.Implementations as appropriate.
	// Called only after ALL features have passed Validate.
	// Features are resolved in registration order — Types before Tables before APIs.
	Resolve(ast *spec.SpecAST, ir *spec.ResolvedSpec) error
}
