package infra

import (
	"fmt"
	"strings"

	"stencil/internal/emitter"
	"stencil/internal/generator"
	"stencil/internal/spec"
	"stencil/internal/template"
)

// ── Template data types ─────────────────────────────────────────────────────

type WireData struct {
	Package   string
	Module    string
	Imports   []WireImport
	Provides  []WireProvide
	HasDB     bool          // true if tables exist (needs gorm.DB)
	Externals []WireExternal // for closure providers
}

type WireImport struct {
	Alias string
	Path  string
}

type WireExternal struct {
	ImplName string // "StripeClientImpl"
	Pkg      string // "externals"
}

// WireProvide is one c.Provide(...) call in the generated wire.go.
// If IsFunc is false: c.Provide(Constructor)
// If IsFunc is true:  c.Provide(func() ReturnType { return FuncBody })
type WireProvide struct {
	Constructor string // used when IsFunc == false
	Comment     string
	IsFunc      bool   // true for hooks and mappers (anonymous func providers)
	ReturnType  string // e.g. "*customer_ap_is.CreateCustomerHooks"
	FuncBody    string // e.g. "&customer_ap_is.CreateCustomerHooks{}"
}

// ── Generator ───────────────────────────────────────────────────────────────

type wireGenerator struct {
	engine *template.Engine
}

func NewWireGenerator(e *template.Engine) generator.Generator {
	return &wireGenerator{engine: e}
}

func (g *wireGenerator) ID() string { return "go.wire" }

func (g *wireGenerator) Generate(ctx generator.GeneratorContext) ([]emitter.File, error) {
	s := ctx.Spec

	var provides []WireProvide
	seen := make(map[string]bool)
	var imports []WireImport

	addImport := func(alias, path string) {
		if !seen[path] {
			seen[path] = true
			imports = append(imports, WireImport{Alias: alias, Path: path})
		}
	}

	// ── Repositories ──────────────────────────────────────────────────────────
	for _, impl := range s.ImplsOfKind(spec.RepositoryImpl) {
		pkg, importPath := deriveWireImport(s.Module, impl)
		addImport(pkg, importPath)
		provides = append(provides, WireProvide{
			Constructor: pkg + ".New" + impl.Name,
			Comment:     impl.Name,
		})
	}

	// ── Default hooks + default mappers + services + handlers (per API group) ──
	for _, impl := range s.ImplsOfKind(spec.ServiceImpl) {
		pkg, importPath := deriveWireImport(s.Module, impl)
		addImport(pkg, importPath)

		// Default hooks for each method in this group
		for _, method := range impl.Methods {
			hooksType := method.FunctionName + "Hooks"
			provides = append(provides, WireProvide{
				IsFunc:     true,
				ReturnType: "*" + pkg + "." + hooksType,
				FuncBody:   "&" + pkg + "." + hooksType + "{}",
				Comment:    hooksType + " (default no-op)",
			})

			// Default mapper
			mapperIface := method.FunctionName + "Mappers"
			defaultMapper := "Default" + method.FunctionName + "Mappers"
			provides = append(provides, WireProvide{
				IsFunc:     true,
				ReturnType: pkg + "." + mapperIface,
				FuncBody:   "&" + pkg + "." + defaultMapper + "{}",
				Comment:    defaultMapper + " (must-override stubs)",
			})
		}

		// Service
		provides = append(provides, WireProvide{
			Constructor: pkg + ".New" + impl.Name,
			Comment:     impl.Name,
		})

		// Handler
		provides = append(provides, WireProvide{
			Constructor: pkg + ".New" + impl.Name + "Handler",
			Comment:     impl.Name + "Handler",
		})
	}

	// ── Externals — use closure providers so they receive cfg ──
	var externals []WireExternal
	for _, impl := range s.ImplsOfKind(spec.ExternalImpl) {
		externals = append(externals, WireExternal{
			ImplName: impl.Name,
			Pkg:      "externals",
		})
	}

	data := WireData{
		Package:   "generated",
		Module:    s.Module,
		Imports:   imports,
		Provides:  provides,
		HasDB:     len(s.ObjectsOfKind(spec.TableModel)) > 0,
		Externals: externals,
	}

	out, err := g.engine.Render("go/infra/wire.go.tmpl", data)
	if err != nil {
		return nil, fmt.Errorf("wire generator render failed: %w", err)
	}

	return []emitter.File{{Path: "wire.go", Content: out}}, nil
}

func deriveWireImport(module string, impl *spec.ResolvedImplementation) (pkg string, importPath string) {
	switch impl.Kind {
	case spec.RepositoryImpl:
		pkg = lastPathSegment(impl.Path)
		importPath = module + "/generated/tables/" + pkg
	case spec.ExternalImpl:
		pkg = "externals"
		importPath = module + "/generated/externals"
	case spec.ServiceImpl:
		trimmed := strings.TrimSuffix(impl.Name, "ServiceImpl")
		pkg = toSnakeCase(trimmed)
		importPath = module + "/generated/apis/" + pkg
	default:
		pkg = lastPathSegment(impl.Path)
		importPath = module + "/generated/" + pkg
	}
	return
}

func lastPathSegment(path string) string {
	parts := strings.Split(path, "/")
	if len(parts) < 2 {
		return "unknown"
	}
	return parts[len(parts)-2]
}
