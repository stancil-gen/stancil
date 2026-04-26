package template

import (
	"strings"
	"text/template"
	"unicode"
)

// BuildFuncMap returns the template FuncMap available to all .go.tmpl files.
func BuildFuncMap() template.FuncMap {
	return template.FuncMap{
		"toLower":    strings.ToLower,
		"toUpper":    strings.ToUpper,
		"toSnakeCase": toSnakeCase,
		"toPascalCase": toPascalCase,
		"toCamelCase": toCamelCase,
		"trimPrefix": strings.TrimPrefix,
		"trimSuffix": strings.TrimSuffix,
		"hasPrefix":  strings.HasPrefix,
		"hasSuffix":  strings.HasSuffix,
		"contains":   strings.Contains,
		"join":       strings.Join,
		"add": func(a, b int) int { return a + b },
		"sub": func(a, b int) int { return a - b },
		"dict": func(pairs ...interface{}) map[string]interface{} {
			m := make(map[string]interface{}, len(pairs)/2)
			for i := 0; i+1 < len(pairs); i += 2 {
				k, _ := pairs[i].(string)
				m[k] = pairs[i+1]
			}
			return m
		},
	}
}

func toPascalCase(s string) string {
	parts := strings.FieldsFunc(s, func(r rune) bool {
		return r == '_' || r == '-' || r == ' '
	})
	var b strings.Builder
	for _, part := range parts {
		if len(part) == 0 {
			continue
		}
		// Handle common acronyms
		upper := strings.ToUpper(part)
		switch upper {
		case "ID", "URL", "HTTP", "API", "SQL", "UUID", "JSON", "HTML", "CSS", "JWT", "DLQ", "FK", "PK", "DB":
			b.WriteString(upper)
		default:
			runes := []rune(part)
			runes[0] = unicode.ToUpper(runes[0])
			b.WriteString(string(runes))
		}
	}
	return b.String()
}

func toCamelCase(s string) string {
	p := toPascalCase(s)
	if len(p) == 0 {
		return p
	}
	// Lowercase the leading run of uppercase chars (for acronyms like "ID" → "id", "URL" → "url")
	runes := []rune(p)
	if len(runes) == 1 {
		runes[0] = unicode.ToLower(runes[0])
		return string(runes)
	}
	// If the first 2+ chars are upper, lowercase all but the last one (e.g. "IDField" → "idField")
	i := 0
	for i < len(runes) && unicode.IsUpper(runes[i]) {
		i++
	}
	if i > 1 {
		// "IDField" → i=2, lowercase runes[0..0] = "iDField"... actually we want "idField"
		// Lowercase all upper chars except the last one before the transition
		for j := 0; j < i-1; j++ {
			runes[j] = unicode.ToLower(runes[j])
		}
	} else {
		runes[0] = unicode.ToLower(runes[0])
	}
	return string(runes)
}

func toSnakeCase(s string) string {
	var b strings.Builder
	runes := []rune(s)
	for i, r := range runes {
		if unicode.IsUpper(r) {
			if i > 0 && !unicode.IsUpper(runes[i-1]) {
				b.WriteByte('_')
			} else if i > 0 && i+1 < len(runes) && unicode.IsUpper(runes[i-1]) && !unicode.IsUpper(runes[i+1]) {
				b.WriteByte('_')
			}
			b.WriteRune(unicode.ToLower(r))
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}
