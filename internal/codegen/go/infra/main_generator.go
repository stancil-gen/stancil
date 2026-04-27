package infra

import (
	"fmt"
	"strings"

	"stencil/internal/emit"
	generator "stencil/internal/generate"
	"stencil/internal/spec"
	"stencil/internal/template"
)

// ── Template data types ─────────────────────────────────────────────────────

type MainData struct {
	Module string
	Groups []MainGroup // one per resource group (for handler resolution)
}

type MainGroup struct {
	HandlerType string // "CustomerAPIsServiceImplHandler"
	HandlerVar  string // "customerAPIsHandler"
	HandlerPkg  string // "customer_ap_is"
	ImportPath  string // "order-service/generated/apis/customer_ap_is"
}

type MainExternal struct {
	ImplName    string // "StripeClientImpl"
	ConstructorCall string // "externals.NewStripeClientImpl(httpClient, cfg.StripeUrl, cfg.StripeSecretKey)"
}

// ── Generator ───────────────────────────────────────────────────────────────

type mainGenerator struct {
	engine *template.Engine
}

func NewMainGenerator(e *template.Engine) generator.Generator {
	return &mainGenerator{engine: e}
}

func (g *mainGenerator) ID() string { return "go.main" }

func (g *mainGenerator) Generate(ctx generator.GeneratorContext) ([]emitter.File, error) {
	s := ctx.Spec

	data := MainData{Module: s.Module}

	// Build handler info for each service group
	for _, impl := range s.ImplsOfKind(spec.ServiceImpl) {
		trimmed := strings.TrimSuffix(impl.Name, "ServiceImpl")
		pkg := toSnakeCase(trimmed)
		handlerType := impl.Name + "Handler"
		handlerVar := toCamelCase(trimmed) + "Handler"
		data.Groups = append(data.Groups, MainGroup{
			HandlerType: handlerType,
			HandlerVar:  handlerVar,
			HandlerPkg:  pkg,
			ImportPath:  s.Module + "/generated/apis/" + pkg,
		})
	}

	content, err := g.engine.Render("go/infra/main.go.tmpl", data)
	if err != nil {
		return nil, fmt.Errorf("main generator render failed: %w", err)
	}
	return []emitter.File{{Path: "main.go", Content: content, Scaffold: true}}, nil
}
