package resolver

import (
	"fmt"
	"strings"

	"stencil/internal/spec"
)

// ─── buildRepositoryInterface ─────────────────────────────────────────────────

func buildRepositoryInterface(table spec.TableAST, tableObj *spec.ResolvedObject, lang spec.Lang, db spec.DBDriver) spec.ResolvedInterface {
	name := modelName(table.Name)
	iface := spec.ResolvedInterface{
		Name:    name + "Repository",
		Path:    fmt.Sprintf("generated/repo/%s/repo.go", table.Name),
		Kind:    spec.RepositoryInterface,
		Imports: make(map[spec.Lang]spec.ImportSet),
	}

	// Always-generated standard CRUD
	iface.Functions = append(iface.Functions,
		repoFunc(fmt.Sprintf("Create%s", name),
			[]spec.ResolvedParam{ptrParam("data", name)},
			nil,
			spec.QueryCreate),
		repoFunc(fmt.Sprintf("Get%sByID", name),
			[]spec.ResolvedParam{uuidParam("id")},
			[]spec.ResolvedReturn{ptrReturn(name)},
			spec.QueryGet),
		repoFunc(fmt.Sprintf("Update%s", name),
			[]spec.ResolvedParam{uuidParam("id"), ptrParam("data", name)},
			nil,
			spec.QueryUpdate),
		repoFunc(fmt.Sprintf("Delete%s", name),
			[]spec.ResolvedParam{uuidParam("id")},
			nil,
			spec.QueryDelete),
	)

	// Expand query shorthands
	for _, q := range table.Queries {
		fns := expandQuery(q, name, tableObj, lang, db)
		iface.Functions = append(iface.Functions, fns...)
	}

	return iface
}

// expandQuery turns a QueryAST shorthand into one or more ResolvedFunctions.
func expandQuery(q spec.QueryAST, modelName string, tableObj *spec.ResolvedObject, lang spec.Lang, db spec.DBDriver) []spec.ResolvedFunction {
	var fns []spec.ResolvedFunction

	if q.SoftDelete {
		fns = append(fns, repoFunc(
			fmt.Sprintf("SoftDelete%s", modelName),
			[]spec.ResolvedParam{uuidParam("id")},
			nil,
			spec.QuerySoftDelete,
		))
		return fns
	}

	if q.BulkCreate {
		fns = append(fns, repoFunc(
			fmt.Sprintf("BatchCreate%ss", modelName),
			[]spec.ResolvedParam{slicePtrParam("items", modelName)},
			nil,
			spec.QueryBulkCreate,
		))
		return fns
	}

	if len(q.FindBy) > 0 {
		suffix := "By" + joinPascal(q.FindBy)
		params := buildQueryParams(q.FindBy, tableObj)
		var returns []spec.ResolvedReturn
		var fnName string
		if q.Returns == "single" {
			returns = []spec.ResolvedReturn{ptrReturn(modelName)}
			fnName = fmt.Sprintf("Get%s%s", modelName, suffix)
		} else {
			returns = []spec.ResolvedReturn{slicePtrReturn(modelName)}
			fnName = fmt.Sprintf("Get%ss%s", modelName, suffix)
		}
		fns = append(fns, repoFunc(fnName, params, returns, spec.QueryFindBy))
		return fns
	}

	if len(q.Exists) > 0 {
		suffix := "By" + joinPascal(q.Exists)
		params := buildQueryParams(q.Exists, tableObj)
		fns = append(fns, repoFunc(
			fmt.Sprintf("%sExists%s", modelName, suffix),
			params,
			[]spec.ResolvedReturn{primitiveReturn("bool")},
			spec.QueryExists,
		))
		return fns
	}

	if len(q.Count) > 0 {
		suffix := "By" + joinPascal(q.Count)
		params := buildQueryParams(q.Count, tableObj)
		fns = append(fns, repoFunc(
			fmt.Sprintf("Count%ss%s", modelName, suffix),
			params,
			[]spec.ResolvedReturn{primitiveReturn("int64")},
			spec.QueryCount,
		))
		return fns
	}

	if q.Paginate != nil {
		paginateKind := "cursor"
		if s, ok := q.Paginate.(string); ok {
			paginateKind = s
		}
		var params []spec.ResolvedParam
		var returns []spec.ResolvedReturn
		if paginateKind == "offset" {
			params = []spec.ResolvedParam{primitiveParam("page", "int"), primitiveParam("limit", "int")}
			returns = []spec.ResolvedReturn{slicePtrReturn(modelName), primitiveReturn("int")}
		} else {
			params = []spec.ResolvedParam{primitiveParam("cursor", "string"), primitiveParam("limit", "int")}
			returns = []spec.ResolvedReturn{slicePtrReturn(modelName), primitiveReturn("string")}
		}
		fns = append(fns, repoFunc(fmt.Sprintf("List%ss", modelName), params, returns, spec.QueryPaginate))
		return fns
	}

	if q.Custom != "" {
		var params []spec.ResolvedParam
		for _, p := range q.Params {
			td := MapType(p.Type, lang, db, false)
			params = append(params, spec.ResolvedParam{Name: p.Name, Type: td})
		}
		var returns []spec.ResolvedReturn
		if q.Returns == "single" {
			returns = []spec.ResolvedReturn{ptrReturn(modelName)}
		} else {
			returns = []spec.ResolvedReturn{slicePtrReturn(modelName)}
		}
		fns = append(fns, repoFunc(toPascalCase(q.Custom), params, returns, spec.QueryCustom))
		return fns
	}

	return fns
}

// buildQueryParams resolves field names to typed ResolvedParams using the table model's fields.
func buildQueryParams(fieldNames []string, tableObj *spec.ResolvedObject) []spec.ResolvedParam {
	var params []spec.ResolvedParam
	for _, fn := range fieldNames {
		p := primitiveParam(fn, "interface{}")
		if tableObj != nil {
			fieldPascal := toPascalCase(fn)
			for _, f := range tableObj.Fields {
				if f.Name == fieldPascal {
					goType := f.Type.GoType
					if strings.HasPrefix(goType, "*") {
						goType = goType[1:]
					}
					p = spec.ResolvedParam{
						Name: fn,
						Type: spec.TypeDescriptor{Kind: f.Type.Kind, GoType: goType, IsCustom: f.Type.IsCustom},
					}
					break
				}
			}
		}
		params = append(params, p)
	}
	return params
}

// repoFunc builds a ResolvedFunction with typed params, returns, and a QueryKind tag.
// The QueryKind tells generators which GORM pattern to emit for this method.
func repoFunc(name string, params []spec.ResolvedParam, returns []spec.ResolvedReturn, qk spec.QueryKind) spec.ResolvedFunction {
	return spec.ResolvedFunction{
		Name:      name,
		Params:    params,
		Returns:   returns,
		QueryKind: &qk,
	}
}

// joinPascal joins snake_case field names into PascalCase for function name suffixes.
// ["user_id", "status"] → "UserIdAndStatus"
func joinPascal(fields []string) string {
	parts := make([]string, len(fields))
	for i, f := range fields {
		parts[i] = toPascalCase(f)
	}
	if len(parts) == 1 {
		return parts[0]
	}
	return strings.Join(parts, "And")
}

// ─── buildCacheInterface ──────────────────────────────────────────────────────
// TODO(phase-2): Cache interfaces are deferred to Phase 2.
// func buildCacheInterface(iface spec.CacheInterfaceAST, valueTypeObj *spec.ResolvedObject) spec.ResolvedInterface { ... }

// ─── buildExternalInterface ───────────────────────────────────────────────────

func buildExternalInterface(ext spec.ExternalAST, objects []*spec.ResolvedObject) spec.ResolvedInterface {
	ri := spec.ResolvedInterface{
		Name:    toPascalCase(ext.Name),
		Path:    fmt.Sprintf("generated/external/%s/client.go", toSnakeCase(ext.Name)),
		Kind:    spec.ExternalInterface,
		Imports: make(map[spec.Lang]spec.ImportSet),
	}

	for _, call := range ext.Calls {
		var params []spec.ResolvedParam
		if call.Body != nil {
			bodyName := toPascalCase(call.Body.Name)
			bodyObj := findObjectByName(objects, bodyName)
			params = append(params, spec.ResolvedParam{
				Name:    "req",
				Type:    spec.TypeDescriptor{Kind: spec.TypeCustom, GoType: bodyName, IsCustom: true},
				TypeRef: bodyObj,
			})
		}
		var returns []spec.ResolvedReturn
		if call.Response != nil {
			respName := toPascalCase(call.Response.Name)
			respObj := findObjectByName(objects, respName)
			returns = []spec.ResolvedReturn{
				{
					Type:    spec.TypeDescriptor{Kind: spec.TypeCustom, GoType: respName, IsCustom: true},
					TypeRef: respObj,
				},
			}
		}
		fn := spec.ResolvedFunction{
			Name:    toPascalCase(call.Name),
			Params:  params,
			Returns: returns,
		}
		ri.Functions = append(ri.Functions, fn)
	}

	return ri
}

// ─── buildHookInterface ───────────────────────────────────────────────────────

func buildHookInterface(api spec.APIAST, group spec.ResourceGroupAST, ctxObj *spec.ResolvedObject) spec.ResolvedInterface {
	ctxType := toPascalCase(api.Name) + "Context"
	ri := spec.ResolvedInterface{
		Name:    toPascalCase(api.Name) + "Hooks",
		Path:    fmt.Sprintf("generated/handler/%s/hooks.go", toSnakeCase(group.Group)),
		Kind:    spec.HookInterface,
		Imports: make(map[spec.Lang]spec.ImportSet),
	}

	hookSig := func(fnName string) spec.ResolvedFunction {
		return spec.ResolvedFunction{
			Name:    fnName,
			Params:  []spec.ResolvedParam{ptrParam("shared", ctxType)},
			Returns: nil,
		}
	}

	// Fixed entry hook
	ri.Functions = append(ri.Functions, hookSig("Before"+toPascalCase(api.Name)))

	// Per-touch Before + After hooks
	// Named after touch kind + name + op (NOT flags, which don't exist in the new design)
	for _, step := range api.Steps {
		t := step.Touch
		suffix := hookSuffix(touchKindStr(t), touchName(t), touchOp(t))
		ri.Functions = append(ri.Functions,
			hookSig("Before"+suffix),
			hookSig("After"+suffix),
		)
	}

	// Fixed exit hook
	ri.Functions = append(ri.Functions, hookSig("BeforeResponse"))

	return ri
}

// ─── buildMapperInterface ─────────────────────────────────────────────────────

// buildMapperInterface builds the MapperInterface for one APIAST.
// One MapXxxInput method per step — returns the actual ExternalInput or TableModel type directly
// (not a redundant StepInput copy), plus one MapResponse method returning the ResponseDTO.
func buildMapperInterface(api spec.APIAST, group spec.ResourceGroupAST, ctxObj *spec.ResolvedObject, externals []spec.ExternalAST) spec.ResolvedInterface {
	apiName := toPascalCase(api.Name)
	ctxType := apiName + "Context"

	ri := spec.ResolvedInterface{
		Name:    apiName + "Mappers",
		Path:    fmt.Sprintf("generated/handler/%s/mappers.go", toSnakeCase(group.Group)),
		Kind:    spec.MapperInterface,
		Imports: make(map[spec.Lang]spec.ImportSet),
	}

	mapperSig := func(fnName, retType string) spec.ResolvedFunction {
		return spec.ResolvedFunction{
			Name:   fnName,
			Params: []spec.ResolvedParam{ptrParam("shared", ctxType)},
			Returns: []spec.ResolvedReturn{
				{Type: spec.TypeDescriptor{Kind: spec.TypeCustom, GoType: retType, IsCustom: true}},
			},
		}
	}

	// One MapXxxInput per step — return type is the underlying ExternalInput or TableModel
	for _, step := range api.Steps {
		fnName := "Map" + toPascalCase(step.ID) + "Input"
		inputTypeName := stepInputReturnType(step, externals)
		ri.Functions = append(ri.Functions, mapperSig(fnName, inputTypeName))
	}

	// MapResponse — returns the ResponseDTO
	respTypeName := apiName + "Response"
	ri.Functions = append(ri.Functions, mapperSig("MapResponse", respTypeName))

	return ri
}

// stepInputReturnType derives the Go type name the mapper should return for a step.
// External touch → the ExternalInput body type. Table touch → the TableModel type.
func stepInputReturnType(step spec.StepAST, externals []spec.ExternalAST) string {
	t := step.Touch
	if t.External != "" {
		if bodyName := findExternalCallBodyName(t.External, t.Method, externals); bodyName != "" {
			return bodyName
		}
		return "interface{}"
	}
	if t.Table != "" {
		return modelName(t.Table)
	}
	return "interface{}"
}

// ─── buildServiceInterface ────────────────────────────────────────────────────

func buildServiceInterface(group spec.ResourceGroupAST, objects []*spec.ResolvedObject) spec.ResolvedInterface {
	ri := spec.ResolvedInterface{
		Name:    toPascalCase(group.Group) + "Service",
		Path:    fmt.Sprintf("generated/handler/%s/service.go", toSnakeCase(group.Group)),
		Kind:    spec.ServiceInterface,
		Imports: make(map[spec.Lang]spec.ImportSet),
	}

	for _, api := range group.APIs {
		reqName := toPascalCase(api.Name) + "Request"
		respName := toPascalCase(api.Name) + "Response"

		var params []spec.ResolvedParam
		hasReq := false
		for _, obj := range objects {
			if obj != nil && obj.Name == reqName {
				hasReq = true
				break
			}
		}
		if hasReq {
			params = append(params, ptrParam("req", reqName))
		} else {
			params = append(params, primitiveParam("req", "interface{}"))
		}

		var returns []spec.ResolvedReturn
		hasResp := false
		for _, obj := range objects {
			if obj != nil && obj.Name == respName {
				hasResp = true
				break
			}
		}
		if hasResp {
			returns = []spec.ResolvedReturn{ptrReturn(respName)}
		} else {
			returns = []spec.ResolvedReturn{primitiveReturn("interface{}")}
		}

		fn := spec.ResolvedFunction{
			Name:    toPascalCase(api.Name),
			Params:  params,
			Returns: returns,
		}
		ri.Functions = append(ri.Functions, fn)
	}

	return ri
}
