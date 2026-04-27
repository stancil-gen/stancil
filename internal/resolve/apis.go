package resolve

import (
	"fmt"
	"strings"

	"stencil/internal/spec"
)

// APIsFeature resolves the `resources:` block.
// Owns all three levels for every API resource group:
//   - Level 1: RequestDTO, ResponseDTO, SharedContext objects
//   - Level 2: HookInterface, MapperInterface, ServiceInterface
//   - Level 3: ServiceImpl, DefaultMapperImpl
type APIsFeature struct{}

func (f *APIsFeature) Name() string { return "apis" }

func (f *APIsFeature) Validate(ast *spec.SpecAST) []error { return nil }

func (f *APIsFeature) Resolve(ast *spec.SpecAST, ir *spec.ResolvedSpec) error {
	// Build pointer slices for lookups within this feature
	allObjects := objectPtrs(ir.Objects)
	allInterfaces := interfacePtrs(ir.Interfaces)

	for _, group := range ast.Resources {
		// ── Level 1: DTO + Context objects ────────────────────────────────────
		for _, api := range group.APIs {
			if req := buildRequestDTO(api, group); req != nil {
				ir.Objects = append(ir.Objects, *req)
				allObjects = append(allObjects, &ir.Objects[len(ir.Objects)-1])
			}
			if resp := buildResponseDTO(api, group); resp != nil {
				ir.Objects = append(ir.Objects, *resp)
				allObjects = append(allObjects, &ir.Objects[len(ir.Objects)-1])
			}
			ctx := buildSharedContext(api, group, ast.Externals)
			ir.Objects = append(ir.Objects, ctx)
			allObjects = append(allObjects, &ir.Objects[len(ir.Objects)-1])
		}

		// ── Level 2: Interfaces ────────────────────────────────────────────────
		for _, api := range group.APIs {
			ctxObj := findObjectByName(allObjects, toPascalCase(api.Name)+"Context")

			hookIface := buildHookInterface(api, group, ctxObj)
			ir.Interfaces = append(ir.Interfaces, hookIface)
			allInterfaces = append(allInterfaces, &ir.Interfaces[len(ir.Interfaces)-1])

			if len(api.Steps) > 0 {
				mapperIface := buildMapperInterface(api, group, ctxObj, ast.Externals)
				ir.Interfaces = append(ir.Interfaces, mapperIface)
				allInterfaces = append(allInterfaces, &ir.Interfaces[len(ir.Interfaces)-1])
			}
		}

		svcIface := buildServiceInterface(group, allObjects)
		ir.Interfaces = append(ir.Interfaces, svcIface)
		allInterfaces = append(allInterfaces, &ir.Interfaces[len(ir.Interfaces)-1])

		// ── Level 3: Implementations ───────────────────────────────────────────
		svcIfacePtr := findInterfaceByName(allInterfaces, toPascalCase(group.Group)+"Service")
		svcImpl := buildServiceImpl(group, svcIfacePtr, allObjects, allInterfaces)
		ir.Implementations = append(ir.Implementations, svcImpl)

		for _, api := range group.APIs {
			if len(api.Steps) == 0 {
				continue
			}
			mapperIface := findInterfaceByName(allInterfaces, toPascalCase(api.Name)+"Mappers")
			ctxObj := findObjectByName(allObjects, toPascalCase(api.Name)+"Context")
			mapperImpl := buildDefaultMapperImpl(api, group, mapperIface, ctxObj, allObjects, ast.Externals)
			ir.Implementations = append(ir.Implementations, mapperImpl)
		}
	}

	return nil
}

// ─── Slice pointer helpers ─────────────────────────────────────────────────────

func objectPtrs(objects []spec.ResolvedObject) []*spec.ResolvedObject {
	ptrs := make([]*spec.ResolvedObject, len(objects))
	for i := range objects {
		ptrs[i] = &objects[i]
	}
	return ptrs
}

func interfacePtrs(ifaces []spec.ResolvedInterface) []*spec.ResolvedInterface {
	ptrs := make([]*spec.ResolvedInterface, len(ifaces))
	for i := range ifaces {
		ptrs[i] = &ifaces[i]
	}
	return ptrs
}

func findInterfaceByName(ifaces []*spec.ResolvedInterface, name string) *spec.ResolvedInterface {
	for _, iface := range ifaces {
		if iface != nil && iface.Name == name {
			return iface
		}
	}
	return nil
}

// ─── Level 1: DTOs + SharedContext ────────────────────────────────────────────

func buildRequestDTO(api spec.APIAST, group spec.ResourceGroupAST) *spec.ResolvedObject {
	if api.DTOs == nil || api.DTOs.Request == nil {
		return nil
	}
	dto := api.DTOs.Request
	obj := spec.ResolvedObject{
		Name: toPascalCase(dto.Name),
		Path: fmt.Sprintf("generated/handler/%s/types.go", toSnakeCase(group.Group)),
		Kind: spec.RequestDTO,
	}
	for _, f := range dto.Fields {
		obj.Fields = append(obj.Fields, buildDTOField(f))
	}
	return &obj
}

func buildResponseDTO(api spec.APIAST, group spec.ResourceGroupAST) *spec.ResolvedObject {
	if api.DTOs == nil || api.DTOs.Response == nil {
		return nil
	}
	dto := api.DTOs.Response
	obj := spec.ResolvedObject{
		Name: toPascalCase(dto.Name),
		Path: fmt.Sprintf("generated/handler/%s/types.go", toSnakeCase(group.Group)),
		Kind: spec.ResponseDTO,
	}
	for _, f := range dto.Fields {
		obj.Fields = append(obj.Fields, buildDTOField(f))
	}
	return &obj
}

// buildSharedContext builds the per-API execution context object.
// Fields: Request + per-step Output + Response
// All TypeDescriptors use Kind + CustomName — no GoType strings.
func buildSharedContext(api spec.APIAST, group spec.ResourceGroupAST, externals []spec.ExternalAST) spec.ResolvedObject {
	name := toPascalCase(api.Name) + "Context"
	obj := spec.ResolvedObject{
		Name: name,
		Path: fmt.Sprintf("generated/handler/%s/types.go", toSnakeCase(group.Group)),
		Kind: spec.SharedContext,
	}

	// 1. Request field
	if api.DTOs != nil && api.DTOs.Request != nil {
		reqName := toPascalCase(api.DTOs.Request.Name)
		obj.Fields = append(obj.Fields, spec.ResolvedField{
			Name:     "Request",
			DBColumn: "request",
			Type:     spec.TypeDescriptor{Kind: spec.TypeCustom, CustomName: reqName, IsCustom: true, Nullable: true},
		})
	}

	// 2. Per-step Output fields
	for _, step := range api.Steps {
		outputFieldName := contextOutputFieldName(step.ID, touchKindStr(step.Touch), touchName(step.Touch), touchOp(step.Touch))
		outputType := deriveStepOutputType(step.Touch, externals)
		if outputType.Kind == 0 && !outputType.IsCustom {
			continue // skip steps with no meaningful output (delete, publish, etc.)
		}
		obj.Fields = append(obj.Fields, spec.ResolvedField{
			Name:     outputFieldName,
			DBColumn: toSnakeCase(outputFieldName),
			Type:     outputType,
		})
	}

	// 3. Response field
	if api.DTOs != nil && api.DTOs.Response != nil {
		respName := toPascalCase(api.DTOs.Response.Name)
		obj.Fields = append(obj.Fields, spec.ResolvedField{
			Name:     "Response",
			DBColumn: "response",
			Type:     spec.TypeDescriptor{Kind: spec.TypeCustom, CustomName: respName, IsCustom: true, Nullable: true},
		})
	}

	return obj
}

// deriveStepOutputType returns the TypeDescriptor for what a step produces.
// Language-agnostic — uses Kind + CustomName, not GoType strings.
func deriveStepOutputType(touch spec.TouchAST, externals []spec.ExternalAST) spec.TypeDescriptor {
	if touch.Table != "" {
		name := modelName(touch.Table)
		switch touch.Op {
		case "list":
			return spec.TypeDescriptor{Kind: spec.TypeCustom, CustomName: name, IsCustom: true, IsList: true}
		case "delete":
			return spec.TypeDescriptor{} // no output
		default: // create, get, update
			return spec.TypeDescriptor{Kind: spec.TypeCustom, CustomName: name, IsCustom: true, Nullable: true}
		}
	}
	if touch.External != "" {
		// Find the response type name from the externals AST
		for _, ext := range externals {
			if !strings.EqualFold(ext.Name, touch.External) {
				continue
			}
			for _, call := range ext.Calls {
				if strings.EqualFold(call.Name, touch.Method) {
					if call.Response != nil {
						// External generator names responses as "{CallName}Response"
						return spec.TypeDescriptor{
							Kind:       spec.TypeCustom,
							CustomName: toPascalCase(call.Name) + "Response",
							IsCustom:   true,
							Nullable:   true,
						}
					}
					return spec.TypeDescriptor{} // no response
				}
			}
		}
		// External call found but no response — no output
		return spec.TypeDescriptor{}
	}
	if touch.Cache != "" {
		if touch.Op == "get" {
			return spec.TypeDescriptor{Kind: spec.TypeAny} // cache value type resolved later
		}
		return spec.TypeDescriptor{} // set/delete/invalidate produce no output
	}
	return spec.TypeDescriptor{}
}

// ─── Level 2: Interfaces ──────────────────────────────────────────────────────

func buildHookInterface(api spec.APIAST, group spec.ResourceGroupAST, ctxObj *spec.ResolvedObject) spec.ResolvedInterface {
	ctxType := toPascalCase(api.Name) + "Context"
	ri := spec.ResolvedInterface{
		Name: toPascalCase(api.Name) + "Hooks",
		Path: fmt.Sprintf("generated/handler/%s/hooks.go", toSnakeCase(group.Group)),
		Kind: spec.HookInterface,
	}

	hookSig := func(fnName string) spec.ResolvedFunction {
		return spec.ResolvedFunction{
			Name:    fnName,
			Params:  []spec.ResolvedParam{ptrParam("shared", ctxType)},
			Returns: nil,
		}
	}

	ri.Functions = append(ri.Functions, hookSig("Before"+toPascalCase(api.Name)))

	for _, step := range api.Steps {
		t := step.Touch
		suffix := hookSuffix(touchKindStr(t), touchName(t), touchOp(t))
		ri.Functions = append(ri.Functions,
			hookSig("Before"+suffix),
			hookSig("After"+suffix),
		)
	}

	ri.Functions = append(ri.Functions, hookSig("BeforeResponse"))
	return ri
}

func buildMapperInterface(api spec.APIAST, group spec.ResourceGroupAST, ctxObj *spec.ResolvedObject, externals []spec.ExternalAST) spec.ResolvedInterface {
	apiName := toPascalCase(api.Name)
	ctxType := apiName + "Context"

	ri := spec.ResolvedInterface{
		Name: apiName + "Mappers",
		Path: fmt.Sprintf("generated/handler/%s/mappers.go", toSnakeCase(group.Group)),
		Kind: spec.MapperInterface,
	}

	mapperSig := func(fnName string, retType spec.TypeDescriptor) spec.ResolvedFunction {
		return spec.ResolvedFunction{
			Name:    fnName,
			Params:  []spec.ResolvedParam{ptrParam("shared", ctxType)},
			Returns: []spec.ResolvedReturn{{Type: retType}},
		}
	}

	for _, step := range api.Steps {
		fnName := "Map" + toPascalCase(step.ID) + "Input"
		inputType := stepInputReturnType(step, externals)
		ri.Functions = append(ri.Functions, mapperSig(fnName, inputType))
	}

	// MapResponse returns the ResponseDTO
	respType := spec.TypeDescriptor{Kind: spec.TypeCustom, CustomName: apiName + "Response", IsCustom: true, Nullable: true}
	ri.Functions = append(ri.Functions, mapperSig("MapResponse", respType))

	return ri
}

// stepInputReturnType derives the TypeDescriptor the mapper should return for a step.
// External touch → ExternalInput body type. Table touch → TableModel type.
func stepInputReturnType(step spec.StepAST, externals []spec.ExternalAST) spec.TypeDescriptor {
	t := step.Touch
	if t.External != "" {
		// Find the body type name for this external call
		for _, ext := range externals {
			if !strings.EqualFold(ext.Name, t.External) {
				continue
			}
			for _, call := range ext.Calls {
				if strings.EqualFold(call.Name, t.Method) {
					if call.Body != nil {
						return spec.TypeDescriptor{
							Kind:       spec.TypeCustom,
							CustomName: toPascalCase(call.Body.Name),
							IsCustom:   true,
						}
					}
					return spec.TypeDescriptor{Kind: spec.TypeAny}
				}
			}
		}
		return spec.TypeDescriptor{Kind: spec.TypeAny}
	}
	if t.Table != "" {
		return spec.TypeDescriptor{Kind: spec.TypeCustom, CustomName: modelName(t.Table), IsCustom: true}
	}
	return spec.TypeDescriptor{Kind: spec.TypeAny}
}

func buildServiceInterface(group spec.ResourceGroupAST, objects []*spec.ResolvedObject) spec.ResolvedInterface {
	ri := spec.ResolvedInterface{
		Name: toPascalCase(group.Group) + "Service",
		Path: fmt.Sprintf("generated/handler/%s/service.go", toSnakeCase(group.Group)),
		Kind: spec.ServiceInterface,
	}

	for _, api := range group.APIs {
		reqName := toPascalCase(api.Name) + "Request"
		respName := toPascalCase(api.Name) + "Response"

		var params []spec.ResolvedParam
		if hasObject(objects, reqName) {
			params = append(params, ptrParam("req", reqName))
		} else {
			params = append(params, kindParam("req", spec.TypeAny))
		}

		var returns []spec.ResolvedReturn
		if hasObject(objects, respName) {
			returns = []spec.ResolvedReturn{ptrReturn(respName)}
		} else {
			returns = []spec.ResolvedReturn{anyReturn()}
		}

		ri.Functions = append(ri.Functions, spec.ResolvedFunction{
			Name:    toPascalCase(api.Name),
			Params:  params,
			Returns: returns,
		})
	}

	return ri
}

func hasObject(objects []*spec.ResolvedObject, name string) bool {
	for _, obj := range objects {
		if obj != nil && obj.Name == name {
			return true
		}
	}
	return false
}

// ─── Level 3: ServiceImpl ────────────────────────────────────────────────────

func buildServiceImpl(
	group spec.ResourceGroupAST,
	serviceIface *spec.ResolvedInterface,
	objects []*spec.ResolvedObject,
	interfaces []*spec.ResolvedInterface,
) spec.ResolvedImplementation {
	impl := spec.ResolvedImplementation{
		Name:       toPascalCase(group.Group) + "ServiceImpl",
		Path:       fmt.Sprintf("generated/handler/%s/%s_impl.go", toSnakeCase(group.Group), toSnakeCase(group.Group)),
		Kind:       spec.ServiceImpl,
		Implements: serviceIface,
		BasePath:   group.BasePath,
	}

	depByKey := make(map[string]spec.ResolvedDependency)
	depOrder := []string{} // insertion order for deterministic output
	addDep := func(key, fieldName, typeName, importPath string) {
		if _, ok := depByKey[key]; !ok {
			depByKey[key] = spec.ResolvedDependency{
				FieldName: fieldName,
				TypeName:  typeName,
				Import:    importPath,
			}
			depOrder = append(depOrder, key)
		}
	}

	for _, api := range group.APIs {
		ctxName := toPascalCase(api.Name) + "Context"
		ctxObj := findObjectByName(objects, ctxName)

		// Hooks dependency per API
		hooksTypeName := "*" + toPascalCase(api.Name) + "Hooks"
		hooksFieldName := toPascalCase(api.Name) + "Hooks"
		addDep("hooks:"+api.Name,
			strings.ToLower(string(hooksFieldName[0]))+hooksFieldName[1:],
			hooksTypeName, "")

		// Mapper dependency (if steps exist)
		var mapperRef *spec.ResolvedInterface
		if len(api.Steps) > 0 {
			mapperIfaceName := toPascalCase(api.Name) + "Mappers"
			mapperRef = findInterfaceByName(interfaces, mapperIfaceName)
			if mapperRef != nil {
				mapperFieldName := toPascalCase(api.Name) + "Mappers"
				addDep("mappers:"+api.Name,
					strings.ToLower(string(mapperFieldName[0]))+mapperFieldName[1:],
					"*Default"+toPascalCase(api.Name)+"Mappers", "")
			}
		}

		method := spec.ResolvedMethod{
			FunctionName:   toPascalCase(api.Name),
			SharedContext:  ctxObj,
			ExecutionModel: spec.Sequential,
			MapperRef:      mapperRef,
			HTTPMethod:     api.Method,
			HTTPPath:       api.Path,
		}

		for _, step := range api.Steps {
			t := step.Touch
			outputField := contextOutputFieldName(step.ID, touchKindStr(t), touchName(t), touchOp(t))
			fatal := step.Fatal != nil && *step.Fatal

			var touch spec.ResolvedTouch

			if t.Table != "" {
				repoName := modelName(t.Table) + "Repository"
				repoIface := findInterfaceByName(interfaces, repoName)

				// Find the specific QueryRef function matching the op
				var queryRef *spec.ResolvedFunction
				if repoIface != nil {
					targetFnName := mapOpToRepoFunc(t.Op, modelName(t.Table))
					for i := range repoIface.Functions {
						if repoIface.Functions[i].Name == targetFnName {
							queryRef = &repoIface.Functions[i]
							break
						}
					}
				}

				// Add repo dependency (interface type, no pointer prefix)
				repoFieldName := strings.ToLower(string(repoName[0])) + repoName[1:]
				addDep("repo:"+t.Table, repoFieldName, repoName, "")

				touch = spec.ResolvedTouch{
					Kind:        spec.TouchKindTable,
					TableRef:    findObjectByName(objects, modelName(t.Table)),
					QueryRef:    queryRef,
					ExternalRef: repoIface,
					Op:          t.Op,
					ResultField: outputField,
					FatalError:  fatal,
					StepID:      step.ID,
				}

			} else if t.External != "" {
				extIfaceName := toPascalCase(t.External)
				extIface := findInterfaceByName(interfaces, extIfaceName)

				// Accept both `method:` and `op:` in the YAML for external touches.
				extMethodRaw := t.Method
				if extMethodRaw == "" {
					extMethodRaw = t.Op
				}

				var extMethod *spec.ResolvedFunction
				if extIface != nil && extMethodRaw != "" {
					methodName := toPascalCase(extMethodRaw)
					for i := range extIface.Functions {
						if extIface.Functions[i].Name == methodName {
							extMethod = &extIface.Functions[i]
							break
						}
					}
				}

				// Register as interface type (no pointer) — the service holds the interface,
				// not the concrete implementation, enabling developer override via DI.
				extFieldName := strings.ToLower(string(extIfaceName[0])) + extIfaceName[1:]
				addDep("ext:"+t.External, extFieldName, extIfaceName, "")

				touch = spec.ResolvedTouch{
					Kind:           spec.TouchKindExternal,
					ExternalRef:    extIface,
					ExternalMethod: extMethod,
					Op:             extMethodRaw,
					ResultField:    outputField,
					FatalError:     fatal,
					StepID:         step.ID,
				}
			} else {
				// Cache / Transaction / Message — deferred to Phase 2
				continue
			}

			method.Touches = append(method.Touches, touch)
		}

		impl.Methods = append(impl.Methods, method)
	}

	// Flatten dependencies in insertion order — deterministic output.
	for _, key := range depOrder {
		impl.Dependencies = append(impl.Dependencies, depByKey[key])
	}

	return impl
}

// mapOpToRepoFunc converts a touch op to the corresponding repository function name.
func mapOpToRepoFunc(op, model string) string {
	switch op {
	case "create":
		return "Create" + model
	case "get":
		return "Get" + model + "ByID"
	case "update":
		return "Update" + model
	case "delete":
		return "Delete" + model
	case "list":
		return "List" + model + "s"
	case "soft_delete":
		return "SoftDelete" + model
	default:
		return toPascalCase(op) + model
	}
}

// ─── Level 3: DefaultMapperImpl ───────────────────────────────────────────────

func buildDefaultMapperImpl(
	api spec.APIAST,
	group spec.ResourceGroupAST,
	mapperIface *spec.ResolvedInterface,
	ctxObj *spec.ResolvedObject,
	objects []*spec.ResolvedObject,
	externals []spec.ExternalAST,
) spec.ResolvedImplementation {
	apiName := toPascalCase(api.Name)
	return spec.ResolvedImplementation{
		Name:          "Default" + apiName + "Mappers",
		Path:          fmt.Sprintf("generated/handler/%s/mappers_default.go", toSnakeCase(group.Group)),
		Kind:          spec.DefaultMapperImpl,
		Implements:    mapperIface,
		FieldMappings: resolveFieldMappings(api, ctxObj, objects, externals),
	}
}

func resolveFieldMappings(
	api spec.APIAST,
	ctxObj *spec.ResolvedObject,
	objects []*spec.ResolvedObject,
	externals []spec.ExternalAST,
) []spec.ResolvedFieldMapping {
	apiName := toPascalCase(api.Name)
	reqObj := findObjectByName(objects, apiName+"Request")

	var mappings []spec.ResolvedFieldMapping
	var availableOutputs []*spec.ResolvedObject

	systemField := func(name string) bool {
		return name == "ID" || name == "CreatedAt" || name == "UpdatedAt" || name == "DeletedAt"
	}

	for _, step := range api.Steps {
		var inputObj *spec.ResolvedObject
		var skipSystemFields bool
		t := step.Touch

		if t.External != "" {
			for _, ext := range externals {
				if !strings.EqualFold(ext.Name, t.External) {
					continue
				}
				for _, call := range ext.Calls {
					if strings.EqualFold(call.Name, t.Method) && call.Body != nil {
						inputObj = findObjectByName(objects, toPascalCase(call.Body.Name))
					}
				}
			}
		} else if t.Table != "" {
			inputObj = findObjectByName(objects, modelName(t.Table))
			skipSystemFields = true
		}

		if inputObj != nil {
			methodName := "Map" + toPascalCase(step.ID) + "Input"
			for _, field := range inputObj.Fields {
				if skipSystemFields && systemField(field.Name) {
					continue
				}
				mappings = append(mappings, inferFieldMapping(methodName, field, reqObj, availableOutputs))
			}
		}

		// After this step runs, its output becomes available to later steps
		outputName := apiName + toPascalCase(step.ID) + "Output"
		if out := findObjectByName(objects, outputName); out != nil {
			availableOutputs = append(availableOutputs, out)
		}
	}

	// MapResponse: source pool is all step outputs + request
	respObj := findObjectByName(objects, apiName+"Response")
	if respObj != nil {
		for _, field := range respObj.Fields {
			if field.Compute {
				mappings = append(mappings, spec.ResolvedFieldMapping{
					MethodName:   "MapResponse",
					TargetField:  field.Name,
					MustOverride: true,
					Reason:       "field is computed — value must be provided by the caller",
				})
				continue
			}
			mappings = append(mappings, inferFieldMapping("MapResponse", field, reqObj, availableOutputs))
		}
	}

	return mappings
}

func inferFieldMapping(
	methodName string,
	target spec.ResolvedField,
	reqObj *spec.ResolvedObject,
	availableOutputs []*spec.ResolvedObject,
) spec.ResolvedFieldMapping {
	// Rule 1: match by name + compatible Kind in Request
	if reqObj != nil {
		if path := matchFieldInObject(target, reqObj, "shared.Request"); path != "" {
			return spec.ResolvedFieldMapping{
				MethodName:  methodName,
				TargetField: target.Name,
				SourcePath:  path,
				Inferred:    true,
			}
		}
	}

	// Rule 2: match in any earlier step output
	for _, out := range availableOutputs {
		prefix := "shared." + out.Name
		if path := matchFieldInObject(target, out, prefix); path != "" {
			return spec.ResolvedFieldMapping{
				MethodName:  methodName,
				TargetField: target.Name,
				SourcePath:  path,
				Inferred:    true,
			}
		}
	}

	// Rule 3: cannot infer — generator emits an error body
	return spec.ResolvedFieldMapping{
		MethodName:   methodName,
		TargetField:  target.Name,
		MustOverride: true,
		Reason:       fmt.Sprintf("no matching source found for field %q by name and type", target.Name),
	}
}

func matchFieldInObject(target spec.ResolvedField, source *spec.ResolvedObject, prefix string) string {
	if source == nil {
		return ""
	}
	for _, sf := range source.Fields {
		if sf.Name == target.Name && sf.Type.Kind == target.Type.Kind {
			return prefix + "." + sf.Name
		}
	}
	return ""
}
