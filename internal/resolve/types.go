package resolve

import "stencil/internal/spec"

// TypesFeature resolves the top-level `types:` block.
// Custom types (Money, Address, etc.) are shared value-object structs
// that tables and DTOs reference.
type TypesFeature struct{}

func (f *TypesFeature) Name() string { return "types" }

func (f *TypesFeature) Validate(ast *spec.SpecAST) []error { return nil }

func (f *TypesFeature) Resolve(ast *spec.SpecAST, ir *spec.ResolvedSpec) error {
	for _, t := range ast.Types {
		ir.Objects = append(ir.Objects, buildTypeObject(t))
	}
	return nil
}

// ─── buildTypeObject ─────────────────────────────────────────────────────────

func buildTypeObject(t spec.CustomTypeAST) spec.ResolvedObject {
	obj := spec.ResolvedObject{
		Name: toPascalCase(t.Name),
		Path: "generated/types/types.go",
		Kind: spec.TypeObject,
	}
	for _, f := range t.Fields {
		obj.Fields = append(obj.Fields, buildField(f))
	}
	return obj
}

// ─── buildField ───────────────────────────────────────────────────────────────

// buildField converts a FieldAST to a ResolvedField.
// No language-specific tags — LangPack computes those at generator time.
func buildField(f spec.FieldAST) spec.ResolvedField {
	td := MapType(f.Type, f.Nullable)
	if f.Type == "enum" {
		td.IsEnum = true
		td.EnumValues = f.Values
	}

	// Collect validation rules — language-agnostic
	var rules []spec.ResolvedRule
	for _, r := range f.Rules {
		rules = append(rules, spec.ResolvedRule{Type: r.Type, Param: r.Value})
	}

	return spec.ResolvedField{
		Name:     toPascalCase(f.Name),
		DBColumn: toSnakeCase(toPascalCase(f.Name)),
		Type:     td,
		Required: f.Required,
		Unique:   f.Unique,
		Nullable: f.Nullable,
		Private:  f.Private,
		Default:  f.Default,
		Values:   f.Values,
		Rules:    rules,
	}
}

// buildDTOField converts a DTOFieldAST to a ResolvedField.
func buildDTOField(f spec.DTOFieldAST) spec.ResolvedField {
	td := MapType(f.Type, false)

	var rules []spec.ResolvedRule
	for _, r := range f.Rules {
		rules = append(rules, spec.ResolvedRule{Type: r.Type, Param: r.Value})
	}

	return spec.ResolvedField{
		Name:     toPascalCase(f.Name),
		DBColumn: toSnakeCase(toPascalCase(f.Name)),
		Type:     td,
		Required: f.Required,
		Private:  f.Private,
		Compute:  f.Compute,
		Rules:    rules,
	}
}
