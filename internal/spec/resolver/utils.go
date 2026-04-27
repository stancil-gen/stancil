package resolver

import (
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"

	"stencil/internal/spec"
)

// ─── Type Kind Mapping ────────────────────────────────────────────────────────

// mapTypeKind converts a YAML type string to a language-agnostic TypeKind.
// LangPack is responsible for converting Kind → language-specific type string.
func mapTypeKind(yamlType string) spec.TypeKind {
	switch yamlType {
	case "str", "string":
		return spec.TypeStr
	case "int", "integer":
		return spec.TypeInt
	case "bool", "boolean":
		return spec.TypeBool
	case "decimal", "float", "number":
		return spec.TypeDecimal
	case "uuid":
		return spec.TypeUUID
	case "timestamp", "datetime":
		return spec.TypeTimestamp
	case "date":
		return spec.TypeDate
	case "json":
		return spec.TypeJSON
	case "enum":
		return spec.TypeEnum
	default:
		// Anything else is a reference to a user-defined custom type (Money, Address, etc.)
		return spec.TypeCustom
	}
}

// mapDBType converts a YAML type string to the database column type.
// This is DB-specific (not language-specific) so it lives in the IR.
func mapDBType(yamlType string, nullable bool) string {
	base := ""
	switch yamlType {
	case "str", "string":
		base = "VARCHAR(255)"
	case "int", "integer":
		base = "BIGINT"
	case "bool", "boolean":
		base = "BOOLEAN"
	case "decimal", "float", "number":
		base = "NUMERIC"
	case "uuid":
		base = "UUID"
	case "timestamp", "datetime":
		base = "TIMESTAMP"
	case "date":
		base = "DATE"
	case "json":
		base = "JSONB"
	case "enum":
		base = "VARCHAR(50)"
	default:
		base = "JSONB" // custom types stored as JSONB
	}
	if nullable {
		return base // nullable is handled by the column definition, not the type itself
	}
	return base
}

// MapType builds a TypeDescriptor for a field from its YAML type string.
// This is language-agnostic — no GoType strings.
func MapType(yamlType string, nullable bool) spec.TypeDescriptor {
	kind := mapTypeKind(yamlType)
	td := spec.TypeDescriptor{
		Kind:     kind,
		Nullable: nullable,
		DBType:   mapDBType(yamlType, nullable),
	}
	if kind == spec.TypeCustom {
		td.IsCustom = true
		td.CustomName = toPascalCase(yamlType)
	}
	return td
}

// ─── ResolvedParam / ResolvedReturn builders ──────────────────────────────────
// These are language-agnostic — no GoType strings embedded.

func uuidParam(name string) spec.ResolvedParam {
	return spec.ResolvedParam{
		Name: name,
		Type: spec.TypeDescriptor{Kind: spec.TypeUUID},
	}
}

func ptrParam(name, typeName string) spec.ResolvedParam {
	return spec.ResolvedParam{
		Name: name,
		Type: spec.TypeDescriptor{Kind: spec.TypeCustom, CustomName: typeName, IsCustom: true, Nullable: true},
	}
}

func slicePtrParam(name, typeName string) spec.ResolvedParam {
	return spec.ResolvedParam{
		Name: name,
		Type: spec.TypeDescriptor{Kind: spec.TypeCustom, CustomName: typeName, IsCustom: true, IsList: true},
	}
}

func kindParam(name string, kind spec.TypeKind) spec.ResolvedParam {
	return spec.ResolvedParam{Name: name, Type: spec.TypeDescriptor{Kind: kind}}
}

func ptrReturn(typeName string) spec.ResolvedReturn {
	return spec.ResolvedReturn{
		Type: spec.TypeDescriptor{Kind: spec.TypeCustom, CustomName: typeName, IsCustom: true, Nullable: true},
	}
}

func slicePtrReturn(typeName string) spec.ResolvedReturn {
	return spec.ResolvedReturn{
		Type: spec.TypeDescriptor{Kind: spec.TypeCustom, CustomName: typeName, IsCustom: true, IsList: true},
	}
}

func kindReturn(kind spec.TypeKind) spec.ResolvedReturn {
	return spec.ResolvedReturn{Type: spec.TypeDescriptor{Kind: kind}}
}

func anyReturn() spec.ResolvedReturn {
	return spec.ResolvedReturn{Type: spec.TypeDescriptor{Kind: spec.TypeAny}}
}

// ─── Naming helpers ───────────────────────────────────────────────────────────

// toPascalCase converts snake_case or any space-separated words to PascalCase.
// Examples: "user_id" → "UserID", "place_order" → "PlaceOrder"
func toPascalCase(words ...string) string {
	var sb strings.Builder
	for _, w := range words {
		if w == "" {
			continue
		}
		parts := strings.FieldsFunc(w, func(r rune) bool {
			return r == '_' || r == '-' || r == ' '
		})
		for _, p := range parts {
			if len(p) > 0 {
				runes := []rune(p)
				runes[0] = unicode.ToUpper(runes[0])
				sb.WriteString(string(runes))
			}
		}
	}
	return sb.String()
}

// toSnakeCase converts PascalCase or camelCase to snake_case.
// Examples: "UserID" → "user_id", "PlaceOrder" → "place_order"
func toSnakeCase(s string) string {
	var result strings.Builder
	runes := []rune(s)
	for i, r := range runes {
		if unicode.IsUpper(r) && i > 0 {
			prev := runes[i-1]
			if unicode.IsLower(prev) || (i+1 < len(runes) && unicode.IsLower(runes[i+1])) {
				result.WriteRune('_')
			}
		}
		result.WriteRune(unicode.ToLower(r))
	}
	return result.String()
}

// singularize removes a trailing 's' from a plural noun.
func singularize(s string) string {
	if strings.HasSuffix(s, "ies") {
		return s[:len(s)-3] + "y"
	}
	if strings.HasSuffix(s, "ses") {
		return s[:len(s)-2]
	}
	if strings.HasSuffix(s, "s") && len(s) > 1 {
		return s[:len(s)-1]
	}
	return s
}

// modelName returns the PascalCase singular struct name for a table name.
// "users" → "User", "orders" → "Order"
func modelName(tableName string) string {
	return toPascalCase(singularize(tableName))
}

// toErrorMessage converts PascalCase to lowercase space-separated.
// "NotFound" → "not found", "EmailTaken" → "email taken"
func toErrorMessage(s string) string {
	var words []string
	start := 0
	runes := []rune(s)
	for i := 1; i < len(runes); i++ {
		if unicode.IsUpper(runes[i]) {
			words = append(words, strings.ToLower(string(runes[start:i])))
			start = i
		}
	}
	words = append(words, strings.ToLower(string(runes[start:])))
	return strings.Join(words, " ")
}

// joinPascal joins snake_case field names into PascalCase for function name suffixes.
// ["user_id", "status"] → "UserIdAndStatus"
func joinPascal(fields []string) string {
	parts := make([]string, len(fields))
	for i, f := range fields {
		parts[i] = toPascalCase(f)
	}
	if len(parts) == 1 {
		return parts[0]
	}
	return strings.Join(parts, "And")
}

// parseDuration parses a Stencil duration string like "5m", "1h", "30s" into time.Duration.
func parseDuration(s string) time.Duration {
	if s == "" {
		return 0
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0
	}
	return d
}

// hookSuffix derives the hook function suffix from touch kind + name + op.
func hookSuffix(kind, name, op string) string {
	switch strings.ToLower(kind) {
	case "table":
		return "Table" + toPascalCase(name) + toPascalCase(op)
	case "external":
		return toPascalCase(name) + toPascalCase(op)
	case "cache":
		return toPascalCase(name) + toPascalCase(op)
	case "transaction":
		return toPascalCase(name) + "Tx"
	case "message":
		return toPascalCase(name) + "Publish"
	}
	return toPascalCase(name) + toPascalCase(op)
}

// contextOutputFieldName derives the SharedContext output field name for a step.
func contextOutputFieldName(stepID, touchKind, name, op string) string {
	if stepID != "" {
		return toPascalCase(stepID) + "Output"
	}
	return hookSuffix(touchKind, name, op) + "Output"
}

// touchKindStr returns the touch kind string for a TouchAST.
func touchKindStr(t spec.TouchAST) string {
	if t.Table != "" {
		return "table"
	}
	if t.External != "" {
		return "external"
	}
	if t.Cache != "" {
		return "cache"
	}
	if t.Transaction != "" {
		return "transaction"
	}
	if t.Message != "" {
		return "message"
	}
	return "unknown"
}

// touchName returns the touch target name for a TouchAST.
func touchName(t spec.TouchAST) string {
	if t.Table != "" {
		return t.Table
	}
	if t.External != "" {
		return t.External
	}
	if t.Cache != "" {
		return t.Cache
	}
	if t.Transaction != "" {
		return t.Transaction
	}
	if t.Message != "" {
		return t.Message
	}
	return ""
}

// touchOp returns the operation for a TouchAST.
func touchOp(t spec.TouchAST) string {
	if t.Op != "" {
		return t.Op
	}
	if t.Method != "" {
		return t.Method
	}
	return ""
}

// ─── Cache key template helpers ───────────────────────────────────────────────

var keyTemplateRe = regexp.MustCompile(`\{([^}]+)\}`)

// parseKeyTemplate extracts parameter names from a cache key template.
func parseKeyTemplate(template string) []string {
	matches := keyTemplateRe.FindAllStringSubmatch(template, -1)
	var params []string
	for _, m := range matches {
		params = append(params, m[1])
	}
	return params
}

// FormatVerb returns the fmt verb for a TypeKind.
func FormatVerb(kind spec.TypeKind) string {
	switch kind {
	case spec.TypeInt:
		return "%d"
	case spec.TypeBool:
		return "%t"
	case spec.TypeUUID:
		return "%s"
	default:
		return "%v"
	}
}

// renderKeyFunc builds the pre-computed key function string.
func renderKeyFunc(template string, params []keyParam) string {
	if len(params) == 0 {
		return fmt.Sprintf(`"%s"`, template)
	}

	fmtStr := keyTemplateRe.ReplaceAllStringFunc(template, func(match string) string {
		inner := match[1 : len(match)-1]
		for _, p := range params {
			if p.name == inner {
				return p.verb
			}
		}
		return "%v"
	})

	var argNames []string
	for _, p := range params {
		argName := p.name
		if p.uuidStr {
			argName = p.name + ".String()"
		}
		argNames = append(argNames, argName)
	}

	return fmt.Sprintf(`fmt.Sprintf("%s", %s)`, fmtStr, strings.Join(argNames, ", "))
}

type keyParam struct {
	name    string
	verb    string
	uuidStr bool
}

// ─── Object lookup helpers ────────────────────────────────────────────────────

func findObjectByName(objects []*spec.ResolvedObject, name string) *spec.ResolvedObject {
	for _, obj := range objects {
		if obj != nil && obj.Name == name {
			return obj
		}
	}
	return nil
}

// linkTypeRefs is a second pass that fills TypeRef pointers on all custom-type fields.
func linkTypeRefs(objects []spec.ResolvedObject) {
	index := make(map[string]*spec.ResolvedObject, len(objects))
	for i := range objects {
		index[objects[i].Name] = &objects[i]
	}
	for i := range objects {
		for j := range objects[i].Fields {
			f := &objects[i].Fields[j]
			if f.Type.IsCustom && f.Type.CustomName != "" {
				f.TypeRef = index[f.Type.CustomName]
			}
		}
	}
}
