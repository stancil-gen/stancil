package golang

import (
	"fmt"
	"strings"

	"stencil/internal/lang"
	"stencil/internal/spec"
)

// GoLangPack implements lang.LangPack for the Go programming language.
// All Go-specific rendering decisions live here — generators call these methods
// instead of hardcoding Go idioms.
type GoLangPack struct{}

// NewGoLangPack returns a GoLangPack ready for use.
func NewGoLangPack() lang.LangPack {
	return &GoLangPack{}
}

// ─── TypeRef ─────────────────────────────────────────────────────────────────

// TypeRef converts a language-agnostic TypeDescriptor to a Go type string + imports.
func (g *GoLangPack) TypeRef(td spec.TypeDescriptor) lang.TypeRef {
	base := g.baseTypeRef(td)

	if td.IsList {
		// []*T — slice of pointers for custom types, []T for primitives
		if td.IsCustom {
			return lang.TypeRef{Name: "[]*" + base.Name, Imports: base.Imports}
		}
		return lang.TypeRef{Name: "[]" + base.Name, Imports: base.Imports}
	}

	if td.Nullable && !g.neverPointer(td.Kind) {
		return lang.TypeRef{Name: "*" + base.Name, Imports: base.Imports}
	}

	return base
}

// baseTypeRef returns the non-pointer, non-slice base type for a TypeDescriptor.
func (g *GoLangPack) baseTypeRef(td spec.TypeDescriptor) lang.TypeRef {
	switch td.Kind {
	case spec.TypeStr:
		return lang.TypeRef{Name: "string"}
	case spec.TypeInt:
		return lang.TypeRef{Name: "int"}
	case spec.TypeBool:
		return lang.TypeRef{Name: "bool"}
	case spec.TypeDecimal:
		return lang.TypeRef{
			Name:    "decimal.Decimal",
			Imports: []string{"github.com/shopspring/decimal"},
		}
	case spec.TypeUUID:
		return lang.TypeRef{
			Name:    "uuid.UUID",
			Imports: []string{"github.com/google/uuid"},
		}
	case spec.TypeTimestamp, spec.TypeDate:
		return lang.TypeRef{
			Name:    "time.Time",
			Imports: []string{"time"},
		}
	case spec.TypeJSON:
		return lang.TypeRef{
			Name:    "json.RawMessage",
			Imports: []string{"encoding/json"},
		}
	case spec.TypeEnum:
		return lang.TypeRef{Name: "string"}
	case spec.TypeCustom:
		// CustomName is the unqualified type name (e.g. "Money", "Order").
		// Generators add package qualification when cross-package references are needed.
		return lang.TypeRef{Name: td.CustomName}
	case spec.TypeAny:
		return lang.TypeRef{Name: "interface{}"}
	}
	return lang.TypeRef{Name: "interface{}"}
}

// neverPointer returns true for types that must not be prefixed with * when nullable.
// json.RawMessage is a []byte — already a reference type.
func (g *GoLangPack) neverPointer(kind spec.TypeKind) bool {
	return kind == spec.TypeJSON
}

// ─── FieldTag ─────────────────────────────────────────────────────────────────

// FieldTag returns the Go struct tag for a field.
// Example: `json:"email" db:"email" validate:"required,email"`
func (g *GoLangPack) FieldTag(name, dbCol string, required, unique bool, rules []spec.ResolvedRule) string {
	var validates []string
	if required {
		validates = append(validates, "required")
	}
	if unique {
		validates = append(validates, "unique")
	}
	for _, r := range rules {
		switch r.Type {
		case "email":
			validates = append(validates, "email")
		case "min_length":
			validates = append(validates, fmt.Sprintf("min=%v", r.Param))
		case "max_length":
			validates = append(validates, fmt.Sprintf("max=%v", r.Param))
		case "min":
			validates = append(validates, fmt.Sprintf("min=%v", r.Param))
		case "max":
			validates = append(validates, fmt.Sprintf("max=%v", r.Param))
		case "regex":
			validates = append(validates, fmt.Sprintf("regexp=%v", r.Param))
		}
	}

	tags := []string{
		fmt.Sprintf(`json:"%s"`, dbCol),
		fmt.Sprintf(`db:"%s"`, dbCol),
	}
	if len(validates) > 0 {
		tags = append(tags, fmt.Sprintf(`validate:"%s"`, strings.Join(validates, ",")))
	}
	return "`" + strings.Join(tags, " ") + "`"
}

// ─── Other LangPack methods ───────────────────────────────────────────────────

// ConfigVarType returns the Go type for a config variable declared in the spec.
func (g *GoLangPack) ConfigVarType(yamlType string) string {
	switch yamlType {
	case "int":
		return "int"
	case "bool":
		return "bool"
	default:
		return "string"
	}
}

// ContextParam is the first parameter of every repo/service method in Go.
func (g *GoLangPack) ContextParam() string {
	return "ctx context.Context"
}

// ContextImport is the import needed for context.Context.
func (g *GoLangPack) ContextImport() string {
	return "context"
}

// ErrorReturn is the error return type in Go.
func (g *GoLangPack) ErrorReturn() string {
	return "error"
}
