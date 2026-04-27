package types

import (
	"regexp"

	"stencil/internal/emitter"
	"stencil/internal/generator"
	"stencil/internal/spec"
	"stencil/internal/template"
)

// ─── Template data structs ──────────────────────────────────────────────────

type TypesData struct {
	Package string
	Imports []string
	Structs []TypeStruct
}

type TypeStruct struct {
	Name   string
	Fields []TypeField
}

type TypeField struct {
	Name string
	Type string
	Tag  string
}

// ─── Generator ──────────────────────────────────────────────────────────────

var dbTagRe = regexp.MustCompile(` ?db:"[^"]*"`)

func stripDBTag(tag string) string {
	return dbTagRe.ReplaceAllString(tag, "")
}

type typesGenerator struct {
	engine *template.Engine
}

func NewTypesGenerator(e *template.Engine) generator.Generator {
	return &typesGenerator{engine: e}
}

func (g *typesGenerator) ID() string { return "go.types" }

func (g *typesGenerator) Generate(ctx generator.GeneratorContext) ([]emitter.File, error) {
	typeObjs := ctx.Spec.ObjectsOfKind(spec.TypeObject)
	if len(typeObjs) == 0 {
		return nil, nil
	}

	data := TypesData{Package: "types"}
	importSeen := map[string]bool{}

	for _, obj := range typeObjs {
		s := TypeStruct{Name: obj.Name}
		for _, f := range obj.Fields {
			ref := ctx.Lang.TypeRef(f.Type)
			s.Fields = append(s.Fields, TypeField{
				Name: f.Name,
				Type: ref.Name,
				Tag:  stripDBTag(ctx.Lang.FieldTag(f.Name, f.DBColumn, f.Required, f.Unique, f.Rules)),
			})
			for _, imp := range ref.Imports {
				importSeen[imp] = true
			}
		}
		data.Structs = append(data.Structs, s)
	}

	for imp := range importSeen {
		data.Imports = append(data.Imports, imp)
	}

	content, err := g.engine.Render("go/types/types.go.tmpl", data)
	if err != nil {
		return nil, err
	}
	return []emitter.File{{Path: "types/types.go", Content: content}}, nil
}
