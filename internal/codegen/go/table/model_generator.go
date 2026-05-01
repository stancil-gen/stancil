package table

import (
	"fmt"
	"strings"

	"stencil/internal/emit"
	generator "stencil/internal/generate"
	"stencil/internal/lang"
	"stencil/internal/spec"
	"stencil/internal/template"
)

// langPack is a local alias so helper functions don't need to import the full package path repeatedly.
type langPack = lang.LangPack

// ─── Template data structures ────────────────────────────────────────────────

type ModelField struct {
	Name string
	Type string
	Tag  string
}

type EnumBlock struct {
	TypeName string
	Values   []string
}

type ModelData struct {
	Package    string
	ModelName  string
	TableName  string
	IDType     string // "uuid" | "auto_increment"
	Fields     []ModelField
	EnumBlocks []EnumBlock
	Imports    []string
}

// ─── Generator ───────────────────────────────────────────────────────────────

type modelGenerator struct {
	engine *template.Engine
}

func NewModelGenerator(engine *template.Engine) generator.Generator {
	return &modelGenerator{engine: engine}
}

func (g *modelGenerator) ID() string { return "go.table.model" }

func (g *modelGenerator) Generate(ctx generator.GeneratorContext) ([]emitter.File, error) {
	obj, ok := ctx.Payload.(*spec.ResolvedObject)
	if !ok {
		return nil, fmt.Errorf("model generator: payload is not *spec.ResolvedObject")
	}
	if obj.Kind != spec.TableModel {
		return nil, fmt.Errorf("model generator: object %s is not a TableModel", obj.Name)
	}

	data := buildModelData(obj, ctx.Spec.Module, ctx.Lang)

	content, err := g.engine.Render("go/table/model.go.tmpl", data)
	if err != nil {
		return nil, fmt.Errorf("model generator: render failed for %s: %w", obj.Name, err)
	}

	return []emitter.File{
		{Path: fmt.Sprintf("tables/%s/model.go", obj.TableName), Content: content},
	}, nil
}

func buildModelData(obj *spec.ResolvedObject, module string, lang langPack) ModelData {
	data := ModelData{
		Package:   obj.TableName,
		ModelName: obj.Name,
		TableName: obj.TableName,
		IDType:    obj.IDType,
	}

	hasCustomTypes := false
	importSeen := map[string]bool{}

	for _, f := range obj.Fields {
		ref := lang.TypeRef(f.Type)
		goType := ref.Name

		// Qualify custom types: "Money" → "types.Money", "*Address" → "*types.Address"
		if f.Type.IsCustom && !strings.Contains(goType, ".") {
			bare := strings.TrimPrefix(goType, "*")
			if strings.HasPrefix(goType, "[]*") {
				bare = strings.TrimPrefix(goType, "[]*")
				goType = "[]*types." + bare
			} else if strings.HasPrefix(goType, "*") {
				goType = "*types." + bare
			} else {
				goType = "types." + bare
			}
			hasCustomTypes = true
		}

		tag := lang.FieldTag(f.Name, f.DBColumn, f.Required, f.Unique, f.Rules)

		// Override the ID field GORM tag based on id_type
		if f.Name == "ID" {
			if obj.IDType == "auto_increment" {
				tag = "`gorm:\"primaryKey;autoIncrement\" json:\"id\"`"
			} else {
				tag = "`gorm:\"primaryKey;type:uuid;default:gen_random_uuid()\" json:\"id\"`"
			}
		}

		data.Fields = append(data.Fields, ModelField{
			Name: f.Name,
			Type: goType,
			Tag:  tag,
		})

		for _, imp := range ref.Imports {
			importSeen[imp] = true
		}

		if f.Type.IsEnum && len(f.Values) > 0 {
			data.EnumBlocks = append(data.EnumBlocks, EnumBlock{
				TypeName: obj.Name + f.Name,
				Values:   f.Values,
			})
		}
	}

	for imp := range importSeen {
		data.Imports = append(data.Imports, imp)
	}
	if hasCustomTypes {
		data.Imports = append(data.Imports, module+"/generated/types")
	}
	return data
}
