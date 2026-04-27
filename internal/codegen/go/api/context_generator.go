package api

import (
	"fmt"
	"strings"

	"stencil/internal/emit"
	generator "stencil/internal/generate"
	"stencil/internal/codegen/go/shared"
	"stencil/internal/spec"
	"stencil/internal/template"
)

// ─── Context template data ──────────────────────────────────────────────────

type ContextData struct {
	Package  string
	Imports  []string
	Contexts []ContextStruct
}

type ContextStruct struct {
	Name   string
	Fields []ContextField
}

type ContextField struct {
	Name    string
	Type    string
	Comment string
}

// ─── Generator ──────────────────────────────────────────────────────────────

type contextGenerator struct {
	engine *template.Engine
}

func NewContextGenerator(e *template.Engine) generator.Generator {
	return &contextGenerator{engine: e}
}

func (g *contextGenerator) ID() string { return "go.api.context" }

func (g *contextGenerator) Generate(ctx generator.GeneratorContext) ([]emitter.File, error) {
	impl, ok := ctx.Payload.(*spec.ResolvedImplementation)
	if !ok || impl.Kind != spec.ServiceImpl {
		return nil, nil
	}

	pkg := derivePackageName(impl.Name)
	module := ctx.Spec.Module

	// Build lookups for cross-package type qualification
	// model name -> table name: "Order" -> "orders"
	tableModelLookup := map[string]string{}
	for _, obj := range ctx.Spec.ObjectsOfKind(spec.TableModel) {
		tableModelLookup[obj.Name] = obj.TableName
	}

	// Build lookup for external calls: "externalName:methodName" -> response body name
	// Now derived from the IR (ExternalMethod.Returns), not from RawExternals.
	extResponseLookup := map[string]string{}
	// extInputTypeLookup maps "externalName:methodName" -> "{CallName}Input"
	extInputTypeLookup := map[string]string{}
	for _, extImpl := range ctx.Spec.ImplsOfKind(spec.ExternalImpl) {
		extName := strings.TrimSuffix(extImpl.Name, "Impl")
		for _, method := range extImpl.Methods {
			key := strings.ToLower(extName) + ":" + strings.ToLower(method.FunctionName)
			extInputTypeLookup[key] = toPascalCase(method.FunctionName) + "Input"
			// If the method has touches with a ResponseBodyRef, it has a response.
			if len(method.Touches) > 0 && method.Touches[0].ResponseBodyRef != nil {
				extResponseLookup[key] = toPascalCase(method.FunctionName) + "Response"
			}
		}
	}

	seen := map[string]bool{}
	var contexts []ContextStruct
	importSeen := map[string]bool{}
	var imports []string

	addImport := func(path string) {
		if !importSeen[path] {
			importSeen[path] = true
			imports = append(imports, path)
		}
	}

	for _, method := range impl.Methods {
		ctxName := method.FunctionName + "Context"
		if method.SharedContext != nil {
			ctxName = method.SharedContext.Name
		}
		if seen[ctxName] {
			continue
		}
		seen[ctxName] = true

		cs := ContextStruct{Name: ctxName}

		// 1. Request field
		reqName := method.FunctionName + "Request"
		if ctx.Spec.ObjectByName(reqName) != nil {
			cs.Fields = append(cs.Fields, ContextField{
				Name: "Request",
				Type: "*" + reqName,
			})
		}

		// 2. Per-step Input + Output fields from touches
		for _, touch := range method.Touches {
			stepID := touch.StepID
			if stepID == "" {
				continue
			}
			pascalStepID := toPascalCase(stepID)

			// Input field — concrete type derived from touch kind + op
			inputType := deriveContextInputType(touch, tableModelLookup, extInputTypeLookup, module, addImport)
			if inputType != "" {
				cs.Fields = append(cs.Fields, ContextField{
					Name: pascalStepID + "Input",
					Type: inputType,
				})
			}

			// Output field — resolved type based on touch kind + op
			outputType, outputComment := deriveContextOutputType(touch, tableModelLookup, extResponseLookup, module, addImport)
			if outputType != "" {
				cs.Fields = append(cs.Fields, ContextField{
					Name:    pascalStepID + "Output",
					Type:    outputType,
					Comment: outputComment,
				})
			}
		}

		// 3. Response field
		respName := method.FunctionName + "Response"
		if ctx.Spec.ObjectByName(respName) != nil {
			cs.Fields = append(cs.Fields, ContextField{
				Name: "Response",
				Type: "*" + respName,
			})
		}

		contexts = append(contexts, cs)
	}

	if len(contexts) == 0 {
		return nil, nil
	}

	// Collect standard library / third-party imports from all field types
	for _, cs := range contexts {
		for _, f := range cs.Fields {
			for substr, importPath := range shared.TypeImportMap {
				if strings.Contains(f.Type, substr) {
					addImport(importPath)
				}
			}
		}
	}

	data := ContextData{
		Package:  pkg,
		Imports:  imports,
		Contexts: contexts,
	}

	out, err := g.engine.Render("go/api/context.go.tmpl", data)
	if err != nil {
		return nil, fmt.Errorf("context generator: %w", err)
	}

	return []emitter.File{
		{Path: fmt.Sprintf("apis/%s/context.go", pkg), Content: out},
	}, nil
}

// deriveContextOutputType returns the Go type string for a touch's output field in the context struct.
// It properly qualifies table model types and external response types with their packages.
// Returns ("", "") if the touch produces no output (e.g. delete, void external).
func deriveContextOutputType(
	touch spec.ResolvedTouch,
	tableModelLookup map[string]string,
	extResponseLookup map[string]string,
	module string,
	addImport func(string),
) (string, string) {
	switch touch.Kind {
	case spec.TouchKindTable:
		if touch.TableRef == nil {
			return "interface{}", ""
		}
		modelName := touch.TableRef.Name
		tableName := touch.TableRef.TableName

		switch touch.Op {
		case "delete":
			return "", "" // no output for delete
		case "list":
			// Detect pagination style from the query function's second return type Kind:
			// cursor → second return Kind == TypeStr (string cursor token)
			// offset → second return Kind == TypeInt (total count)
			addImport(module + "/generated/tables/" + tableName)
			paginationPkg := generator.LibImportPath("pagination")
			if touch.QueryRef != nil && len(touch.QueryRef.Returns) >= 2 {
				if touch.QueryRef.Returns[1].Type.Kind == spec.TypeStr {
					addImport(paginationPkg)
					return "*pagination.CursorPage[" + tableName + "." + modelName + "]", ""
				}
			}
			// Default: offset pagination
			addImport(paginationPkg)
			return "*pagination.OffsetPage[" + tableName + "." + modelName + "]", ""
		default: // create, get, update
			addImport(module + "/generated/tables/" + tableName)
			return "*" + tableName + "." + modelName, ""
		}

	case spec.TouchKindExternal:
		// Look up response type from external method
		extName := ""
		methodName := ""
		if touch.ExternalRef != nil {
			extName = touch.ExternalRef.Name
		}
		if touch.ExternalMethod != nil {
			methodName = touch.ExternalMethod.Name
		}

		key := strings.ToLower(extName) + ":" + strings.ToLower(methodName)
		if respTypeName, ok := extResponseLookup[key]; ok {
			addImport(module + "/generated/externals")
			return "*externals." + respTypeName, ""
		}
		// No response body — void external call
		return "", ""

	default:
		return "interface{}", ""
	}
}

// deriveContextInputType returns the Go type string for a touch's input field in the context struct.
// It properly qualifies table model types and external input types with their packages.
// Returns "" if the touch produces no input (should not happen in practice).
func deriveContextInputType(
	touch spec.ResolvedTouch,
	tableModelLookup map[string]string,
	extInputTypeLookup map[string]string,
	module string,
	addImport func(string),
) string {
	switch touch.Kind {
	case spec.TouchKindTable:
		if touch.TableRef == nil {
			return "interface{}"
		}
		modelName := touch.TableRef.Name
		tableName := touch.TableRef.TableName

		switch touch.Op {
		case "get", "delete":
			addImport("github.com/google/uuid")
			return "uuid.UUID"
		case "create", "update":
			addImport(module + "/generated/tables/" + tableName)
			return "*" + tableName + "." + modelName
		case "list":
			return "interface{}"
		default:
			return "interface{}"
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
			return "externals." + inputTypeName
		}
		return "interface{}"

	default:
		return "interface{}"
	}
}

// qualifyTableType checks if a Go type references a table model (e.g. "*Order",
// "[]*Order") and qualifies it with the table package (e.g. "*orders.Order").
// It also adds the corresponding import path via addImport.
func qualifyTableType(goType string, tableModelLookup map[string]string, module string, addImport func(string)) string {
	// Strip pointer/slice prefix to find the bare type name
	bare := goType
	prefix := ""
	for strings.HasPrefix(bare, "*") || strings.HasPrefix(bare, "[]") {
		if strings.HasPrefix(bare, "[]*") {
			prefix += "[]*"
			bare = bare[3:]
		} else if strings.HasPrefix(bare, "[]") {
			prefix += "[]"
			bare = bare[2:]
		} else if strings.HasPrefix(bare, "*") {
			prefix += "*"
			bare = bare[1:]
		}
	}

	// Check if the bare type name matches a known table model
	if tableName, ok := tableModelLookup[bare]; ok {
		addImport(module + "/generated/tables/" + tableName)
		return prefix + tableName + "." + bare
	}

	return goType
}
