package infra

import (
	"fmt"

	"stencil/internal/emitter"
	"stencil/internal/generator"
	"stencil/internal/template"
)

// ── Generator ───────────────────────────────────────────────────────────────

type hooksScaffoldGenerator struct {
	engine *template.Engine
}

func NewHooksScaffoldGenerator(e *template.Engine) generator.Generator {
	return &hooksScaffoldGenerator{engine: e}
}

func (g *hooksScaffoldGenerator) ID() string { return "go.hooks" }

func (g *hooksScaffoldGenerator) Generate(ctx generator.GeneratorContext) ([]emitter.File, error) {
	data := struct{ Module string }{Module: ctx.Spec.Module}
	content, err := g.engine.Render("go/infra/hooks_register.go.tmpl", data)
	if err != nil {
		return nil, fmt.Errorf("hooks scaffold generator render failed: %w", err)
	}
	return []emitter.File{{Path: "hooks/register.go", Content: content, Scaffold: true}}, nil
}

