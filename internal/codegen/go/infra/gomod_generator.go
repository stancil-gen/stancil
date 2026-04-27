package infra

import (
	"fmt"

	"stencil/internal/emit"
	generator "stencil/internal/generate"
	"stencil/internal/template"
)

// ── Template data types ─────────────────────────────────────────────────────

// GoModData is the top-level payload passed to go/infra/go.mod.tmpl.
type GoModData struct {
	Module    string
	GoVersion string
}

// ── Generator ───────────────────────────────────────────────────────────────

type goModGenerator struct {
	engine *template.Engine
}

func NewGoModGenerator(e *template.Engine) generator.Generator {
	return &goModGenerator{engine: e}
}

func (g *goModGenerator) ID() string { return "go.gomod" }

func (g *goModGenerator) Generate(ctx generator.GeneratorContext) ([]emitter.File, error) {
	data := GoModData{
		Module:    ctx.Spec.Module,
		GoVersion: "1.21",
	}
	content, err := g.engine.Render("go/infra/go.mod.tmpl", data)
	if err != nil {
		return nil, fmt.Errorf("go.mod generator render failed: %w", err)
	}
	return []emitter.File{{Path: "go.mod", Content: content, Scaffold: true}}, nil
}
