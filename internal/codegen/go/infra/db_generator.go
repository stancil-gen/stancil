package infra

import (
	"fmt"

	"stencil/internal/emit"
	generator "stencil/internal/generate"
	"stencil/internal/template"
)

type DBConnection struct {
	TypeName string // "PostgresDB"
	FuncName string // "Postgres" → MustOpenPostgres
	Driver   string // "postgres" | "mysql" | "mongo"
	DSNField string // PascalCase config field name, e.g. "DatabaseUrl"
}

type DBData struct {
	Module      string
	Package     string
	Connections []DBConnection
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

	if len(s.Databases) == 0 {
		return nil, nil // no databases declared — no db.go needed
	}

	var connections []DBConnection
	for _, db := range s.Databases {
		connections = append(connections, DBConnection{
			TypeName: db.TypeName,
			FuncName: db.FuncName,
			Driver:   string(db.Driver),
			DSNField: db.URLField,
		})
	}

	data := DBData{
		Module:      s.Module,
		Package:     "db",
		Connections: connections,
	}

	content, err := g.engine.Render("go/infra/db.go.tmpl", data)
	if err != nil {
		return nil, fmt.Errorf("db generator: %w", err)
	}
	return []emitter.File{{Path: "db/db.go", Content: content}}, nil
}
