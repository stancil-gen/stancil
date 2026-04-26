package table

import (
	"fmt"

	"stencil/internal/emitter"
	"stencil/internal/generator"
	"stencil/internal/spec"
	"stencil/internal/template"
)

// ─── Template data structures ────────────────────────────────────────────────

// ErrorEntry is one sentinel error variable.
type ErrorEntry struct {
	VarName string // "ErrUserNotFound"
	Code    string // "NOT_FOUND"
	Message string // "user: not found"
}

// ErrorsData is the top-level payload passed to errors.go.tmpl.
type ErrorsData struct {
	Package   string       // snake_case table name: "users"
	ModelName string       // PascalCase: "User"
	Errors    []ErrorEntry // one per declared error
}

// ─── Generator ───────────────────────────────────────────────────────────────

type errorsGenerator struct {
	engine *template.Engine
}

func NewErrorsGenerator(engine *template.Engine) generator.Generator {
	return &errorsGenerator{engine: engine}
}

func (g *errorsGenerator) ID() string { return "go.table.errors" }

func (g *errorsGenerator) Generate(ctx generator.GeneratorContext) ([]emitter.File, error) {
	obj, ok := ctx.Payload.(*spec.ResolvedObject)
	if !ok {
		return nil, fmt.Errorf("errors generator: payload is not *spec.ResolvedObject")
	}
	if obj.Kind != spec.TableModel {
		return nil, fmt.Errorf("errors generator: object %s is not a TableModel", obj.Name)
	}

	// No errors declared — skip file generation.
	if len(obj.Errors) == 0 {
		return nil, nil
	}

	data := buildErrorsData(obj)

	content, err := g.engine.Render("go/table/errors.go.tmpl", data)
	if err != nil {
		return nil, fmt.Errorf("errors generator: render failed for %s: %w", obj.Name, err)
	}

	return []emitter.File{
		{
			Path:    fmt.Sprintf("tables/%s/errors.go", obj.TableName),
			Content: content,
		},
	}, nil
}

// buildErrorsData transforms the Errors slice on a ResolvedObject into template-ready ErrorsData.
func buildErrorsData(obj *spec.ResolvedObject) ErrorsData {
	data := ErrorsData{
		Package:   obj.TableName,
		ModelName: obj.Name,
	}

	for _, e := range obj.Errors {
		data.Errors = append(data.Errors, ErrorEntry{
			VarName: e.VarName,
			Code:    e.Code,
			Message: e.Message,
		})
	}

	return data
}
