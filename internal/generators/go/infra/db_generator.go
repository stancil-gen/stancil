package infra

import (
	"fmt"

	"stencil/internal/emitter"
	"stencil/internal/generator"
	"stencil/internal/template"
)

type DBData struct {
	Module   string
	Driver   string // "postgres" | "mysql" | "mongo"
	DSNField string // the AppConfig field name for the DSN, e.g. "DatabaseUrl"
}

type dbGenerator struct {
	engine *template.Engine
}

func NewDBGenerator(e *template.Engine) generator.Generator {
	return &dbGenerator{engine: e}
}

func (g *dbGenerator) ID() string { return "go.db" }

func (g *dbGenerator) Generate(ctx generator.GeneratorContext) ([]emitter.File, error) {
	s := ctx.Spec

	// Find the DATABASE_URL config var (conventional name)
	dsnField := "DatabaseUrl" // default
	for _, cv := range s.Config {
		name := configToPascalCase(cv.Name)
		if name == "DatabaseUrl" || name == "DbUrl" || name == "DatabaseDsn" {
			dsnField = name
			break
		}
	}

	driver := string(s.DB) // "postgres", "mysql", "mongo"

	data := DBData{
		Module:   s.Module,
		Driver:   driver,
		DSNField: dsnField,
	}

	content, err := g.engine.Render("go/infra/db.go.tmpl", data)
	if err != nil {
		return nil, fmt.Errorf("db generator: %w", err)
	}
	return []emitter.File{{Path: "db/db.go", Content: content}}, nil
}
