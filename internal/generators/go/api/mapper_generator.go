package api

import (
	"fmt"
	"strings"

	"stencil/internal/emitter"
	"stencil/internal/generator"
	"stencil/internal/generators/go/shared"
	"stencil/internal/spec"
	"stencil/internal/template"
)

// ─── Mapper template data ──────────────────────────────────────────────────

type MapperData struct {
	Package string
	Imports []string
	Mappers []MapperStruct
}

type MapperStruct struct {
	APIName         string
	InterfaceName   string
	DefaultImplName string
	ContextType     string
	Methods         []MapperMethod
}

type MapperMethod struct {
	Name         string
	ReturnType   string
	ZeroValue    string
	MustOverride bool
}

// ─── Generator ──────────────────────────────────────────────────────────────

type mapperGenerator struct {
	engine *template.Engine
}

func NewMapperGenerator(e *template.Engine) generator.Generator {
	return &mapperGenerator{engine: e}
}

func (g *mapperGenerator) ID() string { return "go.api.mappers" }

func (g *mapperGenerator) Generate(ctx generator.GeneratorContext) ([]emitter.File, error) {
	impl, ok := ctx.Payload.(*spec.ResolvedImplementation)
	if !ok || impl.Kind != spec.ServiceImpl {
		return nil, nil
	}

	pkg := derivePackageName(impl.Name)
	module := ctx.Spec.Module

	// Build table model lookup: model name -> table name
	tableModelLookup := map[string]string{}
	for _, obj := range ctx.Spec.ObjectsOfKind(spec.TableModel) {
		tableModelLookup[obj.Name] = obj.TableName
	}

	// Build external input type lookup: "externalName:methodName" -> "{CallName}Input"
	// Derived from the IR (ExternalImpl methods), not from RawExternals.
	extInputTypeLookup := map[string]string{}
	for _, extImpl := range ctx.Spec.ImplsOfKind(spec.ExternalImpl) {
		extName := strings.TrimSuffix(extImpl.Name, "Impl")
		for _, method := range extImpl.Methods {
			key := strings.ToLower(extName) + ":" + strings.ToLower(method.FunctionName)
			extInputTypeLookup[key] = toPascalCase(method.FunctionName) + "Input"
		}
	}

	seen := map[string]bool{}
	var mappers []MapperStruct

	importSeen := map[string]bool{}
	var imports []string
	addImport := func(path string) {
		if !importSeen[path] {
			importSeen[path] = true
			imports = append(imports, path)
		}
	}
	// fmt is always needed for error messages
	_ = addImport

	for _, method := range impl.Methods {
		mapperIfaceName := method.FunctionName + "Mappers"
		if seen[mapperIfaceName] {
			continue
		}

		// Only generate mapper if there are steps
		if len(method.Touches) == 0 {
			continue
		}
		seen[mapperIfaceName] = true

		// Determine context type
		contextType := method.FunctionName + "Context"
		if method.SharedContext != nil {
			contextType = method.SharedContext.Name
		}

		ms := MapperStruct{
			APIName:         method.FunctionName,
			InterfaceName:   mapperIfaceName,
			DefaultImplName: "Default" + mapperIfaceName,
			ContextType:     contextType,
		}

		// One MapXxxInput per step — return type derived from touch kind + op
		for _, touch := range method.Touches {
			stepID := touch.StepID
			if stepID == "" {
				continue
			}
			fnName := "Map" + toPascalCase(stepID) + "Input"
			returnType, zeroValue := deriveMapperReturnType(touch, tableModelLookup, extInputTypeLookup, module, addImport)
			ms.Methods = append(ms.Methods, MapperMethod{
				Name:         fnName,
				ReturnType:   returnType,
				ZeroValue:    zeroValue,
				MustOverride: true,
			})
		}

		// MapResponse — returns *ResponseType (typed, not interface{})
		respTypeName := method.FunctionName + "Response"
		if ctx.Spec.ObjectByName(respTypeName) != nil {
			ms.Methods = append(ms.Methods, MapperMethod{
				Name:         "MapResponse",
				ReturnType:   "*" + respTypeName,
				ZeroValue:    "nil",
				MustOverride: true,
			})
		} else {
			// Fallback: even without a known response DTO, generate MapResponse with interface{}
			ms.Methods = append(ms.Methods, MapperMethod{
				Name:         "MapResponse",
				ReturnType:   "interface{}",
				ZeroValue:    "nil",
				MustOverride: true,
			})
		}

		mappers = append(mappers, ms)
	}

	if len(mappers) == 0 {
		return nil, nil
	}

	// Collect standard library / third-party imports from mapper return types
	for _, ms := range mappers {
		for _, m := range ms.Methods {
			for substr, importPath := range shared.TypeImportMap {
				if strings.Contains(m.ReturnType, substr) {
					addImport(importPath)
				}
			}
		}
	}

	data := MapperData{
		Package: pkg,
		Imports: imports,
		Mappers: mappers,
	}

	out, err := g.engine.Render("go/api/mappers.go.tmpl", data)
	if err != nil {
		return nil, fmt.Errorf("mapper generator: %w", err)
	}

	return []emitter.File{
		{Path: fmt.Sprintf("apis/%s/mappers.go", pkg), Content: out},
	}, nil
}

// deriveMapperReturnType returns the Go return type and zero value for a mapper method
// based on the touch kind and operation.
func deriveMapperReturnType(
	touch spec.ResolvedTouch,
	tableModelLookup map[string]string,
	extInputTypeLookup map[string]string,
	module string,
	addImport func(string),
) (string, string) {
	switch touch.Kind {
	case spec.TouchKindTable:
		if touch.TableRef == nil {
			return "interface{}", "nil"
		}
		modelName := touch.TableRef.Name
		tableName := touch.TableRef.TableName

		switch touch.Op {
		case "get", "delete":
			addImport("github.com/google/uuid")
			return "uuid.UUID", "uuid.UUID{}"
		case "create", "update":
			addImport(module + "/generated/tables/" + tableName)
			return "*" + tableName + "." + modelName, "nil"
		case "list":
			return "interface{}", "nil"
		default:
			return "interface{}", "nil"
		}

	case spec.TouchKindExternal:
		extName := ""
		methodName := ""
		if touch.ExternalRef != nil {
			extName = touch.ExternalRef.Name
		}
		if touch.ExternalMethod != nil {
			methodName = touch.ExternalMethod.Name
		}

		key := strings.ToLower(extName) + ":" + strings.ToLower(methodName)
		if inputTypeName, ok := extInputTypeLookup[key]; ok {
			addImport(module + "/generated/externals")
			return "externals." + inputTypeName, "externals." + inputTypeName + "{}"
		}
		return "interface{}", "nil"

	default:
		return "interface{}", "nil"
	}
}
