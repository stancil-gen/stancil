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

// ─── Service template data ──────────────────────────────────────────────────

type ServiceData struct {
	Package       string
	Module        string
	Imports       []string
	InterfaceName string
	ImplName      string
	Deps          []ServiceDep
	Methods       []ServiceMethod
}

type ServiceDep struct {
	FieldName string
	TypeName  string
}

type ServiceMethod struct {
	Name         string
	ContextType  string
	RequestType  string
	ResponseType string
	HooksField   string
	HooksType    string
	MappersField string
	MappersType  string
	HasRequest   bool
	HasResponse  bool
	Steps        []ServiceStep
}

type ServiceStep struct {
	StepID       string // raw step ID from touch.StepID
	BeforeHook   string
	AfterHook    string
	InputField   string // PascalCase(StepID) + "Input"
	OutputField  string // PascalCase(StepID) + "Output"
	MapperMethod string // "Map" + PascalCase(StepID) + "Input"
	TouchKind    string // "table" or "external"

	// Table touch fields
	RepoField      string
	RepoMethod     string
	Op             string
	ModelName      string
	TablePkg       string // table package name for qualified types, e.g. "customers"
	PaginationKind string // "cursor" or "offset" (only for list op)

	// External touch fields
	ExtField    string
	ExtMethod   string
	ExtInputType string // external input type name for type assertion, e.g. "CreatePaymentIntentInput"
	HasExtResponse bool // whether external call has a response body

	Fatal bool
}

// ─── Generator ──────────────────────────────────────────────────────────────

type serviceGenerator struct {
	engine *template.Engine
}

func NewServiceGenerator(e *template.Engine) generator.Generator {
	return &serviceGenerator{engine: e}
}

func (g *serviceGenerator) ID() string { return "go.api.service" }

func (g *serviceGenerator) Generate(ctx generator.GeneratorContext) ([]emitter.File, error) {
	impl, ok := ctx.Payload.(*spec.ResolvedImplementation)
	if !ok || impl.Kind != spec.ServiceImpl {
		return nil, nil
	}

	pkg := derivePackageName(impl.Name)
	module := ctx.Spec.Module

	// Interface name
	ifaceName := ""
	if impl.Implements != nil {
		ifaceName = impl.Implements.Name
	}

	// Build lookups for cross-package type qualification
	// Repo interface name -> table name: "OrderRepository" -> "orders"
	repoTableLookup := map[string]string{}
	for _, obj := range ctx.Spec.ObjectsOfKind(spec.TableModel) {
		repoName := obj.Name + "Repository"
		repoTableLookup[repoName] = obj.TableName
	}

	// External call input type lookup: "externalName:methodName" -> "{CallName}Input"
	// Every call gets an Input type (path params + query params + body).
	extInputLookup := map[string]string{}
	// External call response existence lookup
	extResponseLookup := map[string]bool{}
	for _, extImpl := range ctx.Spec.ImplsOfKind(spec.ExternalImpl) {
		extName := strings.TrimSuffix(extImpl.Name, "Impl")
		for _, method := range extImpl.Methods {
			key := strings.ToLower(extName) + ":" + strings.ToLower(method.FunctionName)
			extInputLookup[key] = toPascalCase(method.FunctionName) + "Input"
			if len(method.Touches) > 0 && method.Touches[0].ResponseBodyRef != nil {
				extResponseLookup[key] = true
			}
		}
	}

	importSeen := map[string]bool{}
	var imports []string
	addImport := func(path string) {
		if !importSeen[path] {
			importSeen[path] = true
			imports = append(imports, path)
		}
	}

	// Dependencies — qualify cross-package types
	// Collect from impl.Dependencies + mapper dependencies (added per method below)
	var deps []ServiceDep
	for _, d := range impl.Dependencies {
		typeName := d.TypeName

		// Check if this is a repository dependency (e.g. "*OrderRepository")
		bareName := strings.TrimPrefix(typeName, "*")
		matched := false
		if tableName, ok := repoTableLookup[bareName]; ok {
			// Repository is an interface — no pointer prefix needed
			typeName = tableName + "." + bareName
			addImport(module + "/generated/tables/" + tableName)
			matched = true
		}

		// Check if this is an external interface dependency
		if !matched {
			for _, extIface := range ctx.Spec.InterfacesOfKind(spec.ExternalInterface) {
				if extIface.Name == bareName {
					typeName = "externals." + bareName
					addImport(module + "/generated/externals")
					matched = true
					break
				}
			}
		}

		deps = append(deps, ServiceDep{
			FieldName: d.FieldName,
			TypeName:  typeName,
		})
	}

	// Collect standard/third-party imports from dep types
	for _, d := range deps {
		for substr, importPath := range shared.TypeImportMap {
			if strings.Contains(d.TypeName, substr) {
				addImport(importPath)
			}
		}
	}

	// Methods
	var methods []ServiceMethod
	for _, method := range impl.Methods {
		sm := ServiceMethod{
			Name:         method.FunctionName,
			HooksField:   toCamelCase(method.FunctionName) + "Hooks",
			HooksType:    "*" + method.FunctionName + "Hooks",
			MappersField: toCamelCase(method.FunctionName) + "Mappers",
			MappersType:  method.FunctionName + "Mappers",
			RequestType:  method.FunctionName + "Request",
			ResponseType: method.FunctionName + "Response",
		}

		// Context type
		if method.SharedContext != nil {
			sm.ContextType = method.SharedContext.Name
		} else {
			sm.ContextType = method.FunctionName + "Context"
		}

		// Check if request/response DTOs exist
		if ctx.Spec.ObjectByName(sm.RequestType) != nil {
			sm.HasRequest = true
		}
		if ctx.Spec.ObjectByName(sm.ResponseType) != nil {
			sm.HasResponse = true
		}

		// Steps from touches — now using touch.StepID
		for _, touch := range method.Touches {
			step := buildServiceStep(touch, method.FunctionName, extInputLookup, extResponseLookup)
			sm.Steps = append(sm.Steps, step)
		}

		methods = append(methods, sm)
	}

	// Add external imports if any step uses externals
	for _, m := range methods {
		for _, step := range m.Steps {
			if step.TouchKind == "external" && step.ExtInputType != "" {
				addImport(module + "/generated/externals")
			}
			if step.TouchKind == "table" && step.TablePkg != "" {
				addImport(module + "/generated/tables/" + step.TablePkg)
			}
		}
	}

	// Add fmt import for error wrapping
	hasTouches := false
	for _, m := range methods {
		if len(m.Steps) > 0 {
			hasTouches = true
			break
		}
	}
	if hasTouches {
		addImport("fmt")
	}

	data := ServiceData{
		Package:       pkg,
		Module:        module,
		Imports:       imports,
		InterfaceName: ifaceName,
		ImplName:      impl.Name,
		Deps:          deps,
		Methods:       methods,
	}

	out, err := g.engine.Render("go/api/service.go.tmpl", data)
	if err != nil {
		return nil, fmt.Errorf("service generator: %w", err)
	}

	return []emitter.File{
		{Path: fmt.Sprintf("apis/%s/service.go", pkg), Content: out},
	}, nil
}

// ─── Helpers ────────────────────────────────────────────────────────────────

func buildServiceStep(touch spec.ResolvedTouch, methodName string, extInputLookup map[string]string, extResponseLookup map[string]bool) ServiceStep {
	stepID := touch.StepID
	if stepID == "" {
		// Fallback: derive from touch target
		stepID = touch.ResultField
	}
	pascalStepID := toPascalCase(stepID)

	step := ServiceStep{
		StepID:       stepID,
		InputField:   pascalStepID + "Input",
		OutputField:  pascalStepID + "Output",
		MapperMethod: "Map" + pascalStepID + "Input",
		Fatal:        touch.FatalError,
	}

	switch touch.Kind {
	case spec.TouchKindTable:
		step.TouchKind = "table"

		tableName := ""
		if touch.TableRef != nil {
			tableName = touch.TableRef.TableName
		}
		step.Op = touch.Op
		step.ModelName = modelNameFromTable(tableName)
		step.TablePkg = tableName

		// Hook names: based on touch target (table name + op), NOT step ID
		suffix := hookSuffix("table", tableName, touch.Op)
		step.BeforeHook = "Before" + suffix
		step.AfterHook = "After" + suffix

		// Repo reference: the ExternalRef on a table touch is the repo interface
		if touch.ExternalRef != nil {
			step.RepoField = toCamelCase(touch.ExternalRef.Name)
		}
		// Repo method from QueryRef
		if touch.QueryRef != nil {
			step.RepoMethod = touch.QueryRef.Name
		}

		// Pagination kind for list ops: detect from QueryRef return types using Kind
		if touch.Op == "list" && touch.QueryRef != nil && len(touch.QueryRef.Returns) >= 2 {
			if touch.QueryRef.Returns[1].Type.Kind == spec.TypeStr {
				step.PaginationKind = "cursor"
			} else {
				step.PaginationKind = "offset"
			}
		}

	case spec.TouchKindExternal:
		step.TouchKind = "external"

		extName := ""
		if touch.ExternalRef != nil {
			extName = touch.ExternalRef.Name
		}
		extMethodName := ""
		if touch.ExternalMethod != nil {
			extMethodName = touch.ExternalMethod.Name
		}

		// Hook names: based on touch target (external name + method), NOT step ID
		suffix := hookSuffix("external", extName, extMethodName)
		step.BeforeHook = "Before" + suffix
		step.AfterHook = "After" + suffix

		// External fields
		if touch.ExternalRef != nil {
			step.ExtField = toCamelCase(touch.ExternalRef.Name)
		}
		if touch.ExternalMethod != nil {
			step.ExtMethod = touch.ExternalMethod.Name
		}

		// Look up external input type from raw externals
		key := strings.ToLower(extName) + ":" + strings.ToLower(extMethodName)
		if inputTypeName, ok := extInputLookup[key]; ok {
			step.ExtInputType = inputTypeName
		}
		step.HasExtResponse = extResponseLookup[key]

	default:
		step.TouchKind = "unknown"
		step.BeforeHook = "Before" + toPascalCase(touch.ResultField)
		step.AfterHook = "After" + toPascalCase(touch.ResultField)
	}

	return step
}

func modelNameFromTable(tableName string) string {
	if tableName == "" {
		return ""
	}
	return toPascalCase(singularize(tableName))
}

func singularize(s string) string {
	if strings.HasSuffix(s, "ies") {
		return s[:len(s)-3] + "y"
	}
	if strings.HasSuffix(s, "ses") {
		return s[:len(s)-2]
	}
	if strings.HasSuffix(s, "s") && len(s) > 1 {
		return s[:len(s)-1]
	}
	return s
}

func hookSuffix(kind, name, op string) string {
	switch strings.ToLower(kind) {
	case "table":
		return "Table" + toPascalCase(name) + toPascalCase(op)
	case "external":
		return toPascalCase(name) + toPascalCase(op)
	case "cache":
		return toPascalCase(name) + toPascalCase(op)
	case "transaction":
		return toPascalCase(name) + "Tx"
	case "message":
		return toPascalCase(name) + "Publish"
	}
	return toPascalCase(name) + toPascalCase(op)
}
