package api

import (
	"fmt"
	"regexp"
	"strings"

	"stencil/internal/emitter"
	"stencil/internal/generator"
	"stencil/internal/lang"
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
	importSeen := map[string]bool{}

	for _, method := range impl.Methods {
		// Request DTO
		reqObj := ctx.Spec.ObjectByName(method.FunctionName + "Request")
		if reqObj != nil && !seen[reqObj.Name] {
			seen[reqObj.Name] = true
			s, imps := objectToDTOStruct(reqObj, ctx.Lang)
			structs = append(structs, s)
			for _, imp := range imps {
				importSeen[imp] = true
			}
		}

		// Response DTO
		respObj := ctx.Spec.ObjectByName(method.FunctionName + "Response")
		if respObj != nil && !seen[respObj.Name] {
			seen[respObj.Name] = true
			s, imps := objectToDTOStruct(respObj, ctx.Lang)
			structs = append(structs, s)
			for _, imp := range imps {
				importSeen[imp] = true
			}
		}
	}

	if len(structs) == 0 {
		return nil, nil
	}

	var imports []string
	for imp := range importSeen {
		imports = append(imports, imp)
	}

	data := DTOData{
		Package: pkg,
		Imports: imports,
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

func objectToDTOStruct(obj *spec.ResolvedObject, lp lang.LangPack) (DTOStruct, []string) {
	s := DTOStruct{Name: obj.Name}
	var imports []string
	for _, f := range obj.Fields {
		ref := lp.TypeRef(f.Type)
		imports = append(imports, ref.Imports...)
		s.Fields = append(s.Fields, DTOField{
			Name: f.Name,
			Type: ref.Name,
			Tag:  stripDBTag(lp.FieldTag(f.Name, f.DBColumn, f.Required, f.Unique, f.Rules)),
		})
	}
	return s, imports
}

func derivePackageName(implName string) string {
	trimmed := strings.TrimSuffix(implName, "ServiceImpl")
	return toSnakeCase(trimmed)
}
