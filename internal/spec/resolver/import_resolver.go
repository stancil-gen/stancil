package resolver

import (
	"sort"
	"stencil/internal/spec"
)

// ImportResolver collects language-specific import paths from resolved structs.
// It is stateless and can be called multiple times on different inputs.
// Phase 1: shallow collection — does NOT recurse into TypeRef fields (known limitation).
type ImportResolver struct {
	lang   spec.Lang
	module string // Go module path, e.g. "github.com/acme/orders-service"
}

// NewImportResolver creates an ImportResolver for the given target language and module.
func NewImportResolver(lang spec.Lang, module string) *ImportResolver {
	return &ImportResolver{lang: lang, module: module}
}

// ForObject collects all imports needed to declare a ResolvedObject in the target language.
func (ir *ImportResolver) ForObject(obj *spec.ResolvedObject) spec.ImportSet {
	seen := make(map[string]bool)
	var paths []string

	add := func(p string) {
		if p != "" && !seen[p] {
			seen[p] = true
			paths = append(paths, p)
		}
	}

	for _, f := range obj.Fields {
		add(ir.importForType(f.Type))
		// Phase 1: shallow — if custom type, add its package
		if f.Type.IsCustom && f.TypeRef != nil {
			add(ir.packageForPath(f.TypeRef.Path))
		}
	}

	sort.Strings(paths)
	return spec.ImportSet{Lang: ir.lang, Paths: paths}
}

// ForInterface collects all imports needed to declare a ResolvedInterface.
func (ir *ImportResolver) ForInterface(iface *spec.ResolvedInterface) spec.ImportSet {
	seen := make(map[string]bool)
	var paths []string

	add := func(p string) {
		if p != "" && !seen[p] {
			seen[p] = true
			paths = append(paths, p)
		}
	}

	// All interfaces have method signatures — need context
	add("context")

	for _, fn := range iface.Functions {
		for _, p := range fn.Params {
			add(ir.importForType(p.Type))
		}
		for _, r := range fn.Returns {
			add(ir.importForType(r.Type))
		}
	}

	sort.Strings(paths)
	return spec.ImportSet{Lang: ir.lang, Paths: paths}
}

// ForImplementation collects all imports needed to write a ResolvedImplementation.
func (ir *ImportResolver) ForImplementation(impl *spec.ResolvedImplementation) spec.ImportSet {
	seen := make(map[string]bool)
	var paths []string

	add := func(p string) {
		if p != "" && !seen[p] {
			seen[p] = true
			paths = append(paths, p)
		}
	}

	// Implementations always need context
	add("context")

	// Dependencies drive imports
	for _, dep := range impl.Dependencies {
		if dep.Import != "" {
			add(dep.Import)
		}
	}

	// Walk method touches for additional imports
	for _, m := range impl.Methods {
		for _, t := range m.Touches {
			switch t.Kind {
			case spec.TouchKindHTTPCall:
				add("net/http")
				add("encoding/json")
				add("bytes")
				if t.RetryAttempts > 0 {
					add("time")
				}
			case spec.TouchKindCacheOp:
				add("time")
				add("fmt")
			case spec.TouchKindTxStep:
				add("database/sql")
			}
		}
	}

	sort.Strings(paths)
	return spec.ImportSet{Lang: ir.lang, Paths: paths}
}

// importForType returns the import path for a given TypeDescriptor in the target language.
func (ir *ImportResolver) importForType(t spec.TypeDescriptor) string {
	if t.IsCustom {
		// Phase 1: all custom types are in generated/types
		// Phase 2: this will use packageForPath(t.customPath)
		if ir.module != "" {
			return ir.module + "/generated/types"
		}
		return ""
	}

	switch ir.lang {
	case spec.LangGo:
		switch t.Kind {
		case spec.TypeDecimal:
			return "github.com/shopspring/decimal"
		case spec.TypeUUID:
			return "github.com/google/uuid"
		case spec.TypeTimestamp, spec.TypeDate:
			return "time"
		case spec.TypeJSON:
			return "encoding/json"
		default:
			return "" // primitives need no import
		}
	case spec.LangJava:
		switch t.Kind {
		case spec.TypeDecimal:
			return "java.math.BigDecimal"
		case spec.TypeUUID:
			return "java.util.UUID"
		case spec.TypeTimestamp:
			return "java.time.Instant"
		case spec.TypeDate:
			return "java.time.LocalDate"
		default:
			return ""
		}
	}
	return ""
}

// packageForPath derives the Go import path from a generated file path.
// "generated/repo/users/model.go" → module + "/generated/repo/users"
func (ir *ImportResolver) packageForPath(path string) string {
	if path == "" || ir.module == "" {
		return ""
	}
	// Strip filename
	idx := -1
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' {
			idx = i
			break
		}
	}
	if idx < 0 {
		return ir.module + "/" + path
	}
	return ir.module + "/" + path[:idx]
}
