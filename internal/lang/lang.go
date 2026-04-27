package lang

import "stencil/internal/spec"

// TypeRef is a rendered, language-specific type reference.
// Returned by LangPack.TypeRef — carries both the rendered name and the imports needed.
type TypeRef struct {
	Name    string   // e.g. "uuid.UUID", "*time.Time", "[]*Order", "decimal.Decimal"
	Imports []string // fully qualified import paths needed to use this type
}

// LangPack is the single source of truth for all language-specific rendering decisions.
// Every generator calls these methods instead of hardcoding language idioms.
//
// To add a new target language, implement this interface and register it in the orchestrator.
type LangPack interface {
	// TypeRef converts a language-agnostic TypeDescriptor to a rendered type string.
	// Handles nullability (pointer in Go, | null in TypeScript) and list wrapping.
	TypeRef(td spec.TypeDescriptor) TypeRef

	// FieldTag returns the struct tag / field annotation for a field.
	// Go: `json:"field_name" db:"col" validate:"required,email"`
	// TypeScript: @IsEmail()\n@IsNotEmpty()
	// Returns empty string if the language does not use field annotations.
	FieldTag(name, dbCol string, required, unique bool, rules []spec.ResolvedRule) string

	// ConfigVarType returns the language type for a config variable.
	// Go: "string", "int", "bool"
	// TypeScript: "string", "number", "boolean"
	ConfigVarType(yamlType string) string

	// ContextParam is the first parameter added to every repo/service method.
	// Go: "ctx context.Context"
	// TypeScript: (empty — async functions carry context implicitly)
	ContextParam() string

	// ContextImport is the import path needed for ContextParam.
	// Returns empty string if ContextParam is empty.
	ContextImport() string

	// ErrorReturn is the error return type appended to every fallible method.
	// Go: "error"
	// TypeScript: (empty — errors propagate via Promise rejection)
	ErrorReturn() string
}
