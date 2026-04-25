package resolver

import (
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"

	"stencil/internal/spec"
)

// ─── ResolvedParam / ResolvedReturn helpers ───────────────────────────────────

func ctxParam() spec.ResolvedParam {
	return spec.ResolvedParam{Name: "ctx", Type: spec.TypeDescriptor{GoType: "context.Context"}}
}

func uuidParam(name string) spec.ResolvedParam {
	return spec.ResolvedParam{Name: name, Type: spec.TypeDescriptor{Kind: spec.TypeUUID, GoType: "uuid.UUID"}}
}

func ptrParam(name, typeName string) spec.ResolvedParam {
	return spec.ResolvedParam{
		Name: name,
		Type: spec.TypeDescriptor{Kind: spec.TypeCustom, GoType: "*" + typeName, IsCustom: true, GoPointer: true},
	}
}

func slicePtrParam(name, typeName string) spec.ResolvedParam {
	return spec.ResolvedParam{
		Name: name,
		Type: spec.TypeDescriptor{Kind: spec.TypeCustom, GoType: "[]*" + typeName, IsCustom: true},
	}
}

func primitiveParam(name, goType string) spec.ResolvedParam {
	return spec.ResolvedParam{Name: name, Type: spec.TypeDescriptor{GoType: goType}}
}

func errReturn() spec.ResolvedReturn {
	return spec.ResolvedReturn{Type: spec.TypeDescriptor{GoType: "error"}}
}

func ptrReturn(typeName string) spec.ResolvedReturn {
	return spec.ResolvedReturn{
		Type: spec.TypeDescriptor{Kind: spec.TypeCustom, GoType: "*" + typeName, IsCustom: true, GoPointer: true},
	}
}

func slicePtrReturn(typeName string) spec.ResolvedReturn {
	return spec.ResolvedReturn{
		Type: spec.TypeDescriptor{Kind: spec.TypeCustom, GoType: "[]*" + typeName, IsCustom: true},
	}
}

func primitiveReturn(goType string) spec.ResolvedReturn {
	return spec.ResolvedReturn{Type: spec.TypeDescriptor{GoType: goType}}
}

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
			// Add underscore before uppercase letter if:
			// - previous char is lowercase, OR
			// - next char is lowercase (e.g. "ID" in "UserID" → "user_id")
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
// This is intentionally naive for Phase 1.
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

// deriveGoStructTag builds the Go struct tag string for a field.
// Example: `json:"field_name" db:"col_name" validate:"required"`
func deriveGoStructTag(fieldName string, required, unique bool, rules []string) string {
	snake := toSnakeCase(fieldName)
	tags := []string{
		fmt.Sprintf(`json:"%s"`, snake),
		fmt.Sprintf(`db:"%s"`, snake),
	}

	var validates []string
	if required {
		validates = append(validates, "required")
	}
	if unique {
		validates = append(validates, "unique")
	}
	validates = append(validates, rules...)

	if len(validates) > 0 {
		tags = append(tags, fmt.Sprintf(`validate:"%s"`, strings.Join(validates, ",")))
	}

	return "`" + strings.Join(tags, " ") + "`"
}

// hookFuncName derives the hook function suffix from touch kind + name + op.
// Example: table "users", op "create" → "TableUsersCreate"
// Example: external "StripeClient", method "ChargeCard" → "StripeClientChargeCard"
func hookSuffix(kind string, name string, op string) string {
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
// If stepID is non-empty, uses "{pascal(stepID)}Output".
// Fallback: "{TouchKind}{Pascal(name)}{Pascal(op)}Output"
func contextOutputFieldName(stepID, touchKind, name, op string) string {
	if stepID != "" {
		return toPascalCase(stepID) + "Output"
	}
	return hookSuffix(touchKind, name, op) + "Output"
}

// parseKeyTemplate extracts parameter names from a cache key template.
// "user:{id}" → ["id"]
// "order:{user_id}:{order_id}" → ["user_id", "order_id"]
var keyTemplateRe = regexp.MustCompile(`\{([^}]+)\}`)

func parseKeyTemplate(template string) []string {
	matches := keyTemplateRe.FindAllStringSubmatch(template, -1)
	var params []string
	for _, m := range matches {
		params = append(params, m[1])
	}
	return params
}

// renderKeyFunc builds the pre-computed Go key function string.
// template: "user:{id}", params: [{Name: "id", Type: int64}]
// → `fmt.Sprintf("user:%d", id)`
func renderKeyFunc(template string, params []keyParam) string {
	if len(params) == 0 {
		return fmt.Sprintf(`"%s"`, template)
	}

	fmtStr := keyTemplateRe.ReplaceAllStringFunc(template, func(match string) string {
		// extract param name from {name}
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
	uuidStr bool // if UUID, need .String()
}
