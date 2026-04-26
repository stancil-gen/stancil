package infra

import (
	"fmt"
	"strings"
	"unicode"

	"stencil/internal/emitter"
	"stencil/internal/generator"
	"stencil/internal/spec"
	"stencil/internal/template"
)

// ── Template data types ─────────────────────────────────────────────────────

// RoutesData is the top-level payload passed to go/infra/routes.go.tmpl.
type RoutesData struct {
	Package string // "generated"
	Module  string
	Groups  []RouteGroup
}

// RouteGroup represents one API group (e.g. /users) with its routes.
type RouteGroup struct {
	GroupName   string // "UserAPIs"
	PackageName string // "user_apis" — for import
	ImportPath  string // "mymodule/generated/apis/user_apis"
	BasePath    string // "/users"
	HandlerType string // "UserAPIsServiceImplHandler"
	HandlerVar  string // "userAPIsHandler"
	Routes      []Route
}

// Route is a single HTTP route inside a group.
type Route struct {
	Method      string // "POST"
	Path        string // "/"
	HandlerFunc string // "CreateUser"
}

// ── Generator ───────────────────────────────────────────────────────────────

type routesGenerator struct {
	engine *template.Engine
}

func NewRoutesGenerator(e *template.Engine) generator.Generator {
	return &routesGenerator{engine: e}
}

func (g *routesGenerator) ID() string { return "go.routes" }

func (g *routesGenerator) Generate(ctx generator.GeneratorContext) ([]emitter.File, error) {
	s := ctx.Spec

	var groups []RouteGroup
	for _, impl := range s.ImplsOfKind(spec.ServiceImpl) {
		groupName := strings.TrimSuffix(impl.Implements.Name, "Service")

		var routes []Route
		for _, m := range impl.Methods {
			routes = append(routes, Route{
				Method:      m.HTTPMethod,
				Path:        m.HTTPPath,
				HandlerFunc: m.FunctionName,
			})
		}

		handlerVar := toCamelCase(groupName) + "Handler"
		pkgName := toSnakeCase(groupName)
		importPath := s.Module + "/generated/apis/" + pkgName

		groups = append(groups, RouteGroup{
			GroupName:   groupName,
			PackageName: pkgName,
			ImportPath:  importPath,
			BasePath:    impl.BasePath,
			HandlerType: impl.Name + "Handler",
			HandlerVar:  handlerVar,
			Routes:      routes,
		})
	}

	data := RoutesData{
		Package: "generated",
		Module:  s.Module,
		Groups:  groups,
	}

	out, err := g.engine.Render("go/infra/routes.go.tmpl", data)
	if err != nil {
		return nil, fmt.Errorf("routes generator render failed: %w", err)
	}

	return []emitter.File{{Path: "routes.go", Content: out}}, nil
}

// ── local helpers ───────────────────────────────────────────────────────────

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

func toCamelCase(s string) string {
	if len(s) == 0 {
		return s
	}
	r := []rune(s)
	r[0] = r[0] | 32 // toLower first rune
	return string(r)
}
