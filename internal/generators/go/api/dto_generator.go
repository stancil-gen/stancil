package api

import (
	"fmt"
	"regexp"
	"strings"

	"stencil/internal/emitter"
	"stencil/internal/generator"
	"stencil/internal/generators/go/shared"
	"stencil/internal/spec"
	"stencil/internal/template"
)

// ─── DTO template data ──────────────────────────────────────────────────────

type DTOData struct {
	Package string
	Imports []string
	Structs []DTOStruct
}

type DTOStruct struct {
	Name   string
	Fields []DTOField
}

type DTOField struct {
	Name string
	Type string
	Tag  string
}

// ─── Generator ──────────────────────────────────────────────────────────────

type dtoGenerator struct {
	engine *template.Engine
}

func NewDTOGenerator(e *template.Engine) generator.Generator {
	return &dtoGenerator{engine: e}
}

func (g *dtoGenerator) ID() string { return "go.api.dto" }

func (g *dtoGenerator) Generate(ctx generator.GeneratorContext) ([]emitter.File, error) {
	impl, ok := ctx.Payload.(*spec.ResolvedImplementation)
	if !ok || impl.Kind != spec.ServiceImpl {
		return nil, nil
	}

	pkg := derivePackageName(impl.Name)

	seen := map[string]bool{}
	var structs []DTOStruct

	for _, method := range impl.Methods {
		// Request DTO
		reqObj := ctx.Spec.ObjectByName(method.FunctionName + "Request")
		if reqObj != nil && !seen[reqObj.Name] {
			seen[reqObj.Name] = true
			structs = append(structs, objectToDTOStruct(reqObj))
		}

		// Response DTO
		respObj := ctx.Spec.ObjectByName(method.FunctionName + "Response")
		if respObj != nil && !seen[respObj.Name] {
			seen[respObj.Name] = true
			structs = append(structs, objectToDTOStruct(respObj))
		}
	}

	if len(structs) == 0 {
		return nil, nil
	}

	// Collect imports from all DTO field types
	var allTypes []string
	for _, s := range structs {
		for _, f := range s.Fields {
			allTypes = append(allTypes, f.Type)
		}
	}

	data := DTOData{
		Package: pkg,
		Imports: shared.CollectImports(allTypes),
		Structs: structs,
	}

	out, err := g.engine.Render("go/api/dto.go.tmpl", data)
	if err != nil {
		return nil, fmt.Errorf("dto generator: %w", err)
	}

	return []emitter.File{
		{Path: fmt.Sprintf("apis/%s/dto.go", pkg), Content: out},
	}, nil
}

// ─── Helpers ────────────────────────────────────────────────────────────────

var dbTagRe = regexp.MustCompile(` ?db:"[^"]*"`)

func stripDBTag(tag string) string {
	return dbTagRe.ReplaceAllString(tag, "")
}

func objectToDTOStruct(obj *spec.ResolvedObject) DTOStruct {
	s := DTOStruct{Name: obj.Name}
	for _, f := range obj.Fields {
		s.Fields = append(s.Fields, DTOField{
			Name: f.Name,
			Type: f.Type.GoType,
			Tag:  stripDBTag(f.GoStructTag),
		})
	}
	return s
}

func derivePackageName(implName string) string {
	trimmed := strings.TrimSuffix(implName, "ServiceImpl")
	return toSnakeCase(trimmed)
}
