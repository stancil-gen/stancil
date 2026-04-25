package resolver

import (
	"fmt"
	"strings"

	"stencil/internal/spec"
)

// ─── buildRepositoryImpl ──────────────────────────────────────────────────────

func buildRepositoryImpl(table spec.TableAST, tableObj *spec.ResolvedObject, repoIface *spec.ResolvedInterface) spec.ResolvedImplementation {
	name := modelName(table.Name)
	impl := spec.ResolvedImplementation{
		Name:       name + "RepositoryImpl",
		Path:       fmt.Sprintf("generated/repo/%s/repo_impl.go", table.Name),
		Kind:       spec.RepositoryImpl,
		Implements: repoIface,
		Imports:    make(map[spec.Lang]spec.ImportSet),
		Dependencies: []spec.ResolvedDependency{
			{FieldName: "db", TypeName: "*sql.DB", Import: "database/sql"},
		},
	}

	// Standard CRUD methods
	impl.Methods = append(impl.Methods,
		repoMethod(fmt.Sprintf("Create%s", name), tableObj, spec.QueryCreate, nil, ""),
		repoMethod(fmt.Sprintf("Get%sByID", name), tableObj, spec.QueryGet, nil, ""),
		repoMethod(fmt.Sprintf("Update%s", name), tableObj, spec.QueryUpdate, nil, ""),
		repoMethod(fmt.Sprintf("Delete%s", name), tableObj, spec.QueryDelete, nil, ""),
	)

	// Query shorthand methods
	for _, q := range table.Queries {
		methods := buildRepoImplQueryMethods(q, name, tableObj)
		impl.Methods = append(impl.Methods, methods...)
	}

	return impl
}

func repoMethod(funcName string, tableObj *spec.ResolvedObject, qk spec.QueryKind, filterFields []spec.ResolvedField, customSQL string) spec.ResolvedMethod {
	touch := spec.ResolvedTouch{
		Kind:        spec.TouchKindQuery,
		TableRef:    tableObj,
		QueryKind:   qk,
		FilterFields: filterFields,
		CustomSQL:   customSQL,
	}
	return spec.ResolvedMethod{
		FunctionName: funcName,
		Touches:      []spec.ResolvedTouch{touch},
	}
}

func buildRepoImplQueryMethods(q spec.QueryAST, modelName string, tableObj *spec.ResolvedObject) []spec.ResolvedMethod {
	var methods []spec.ResolvedMethod

	if q.SoftDelete {
		methods = append(methods, repoMethod(
			fmt.Sprintf("SoftDelete%s", modelName),
			tableObj, spec.QuerySoftDelete, nil, "",
		))
		return methods
	}

	if q.BulkCreate {
		methods = append(methods, repoMethod(
			fmt.Sprintf("BatchCreate%ss", modelName),
			tableObj, spec.QueryBulkCreate, nil, "",
		))
		return methods
	}

	if len(q.FindBy) > 0 {
		suffix := "By" + joinPascal(q.FindBy)
		filterFields := resolveFilterFields(q.FindBy, tableObj)
		m := repoMethod(
			fmt.Sprintf("Get%ss%s", modelName, suffix),
			tableObj, spec.QueryFindBy, filterFields, "",
		)
		m.Touches[0].Op = q.Op
		m.Touches[0].ReturnsMany = q.Returns != "single"
		methods = append(methods, m)
		return methods
	}

	if len(q.Exists) > 0 {
		suffix := "By" + joinPascal(q.Exists)
		filterFields := resolveFilterFields(q.Exists, tableObj)
		m := repoMethod(
			fmt.Sprintf("%sExists%s", modelName, suffix),
			tableObj, spec.QueryExists, filterFields, "",
		)
		methods = append(methods, m)
		return methods
	}

	if len(q.Count) > 0 {
		suffix := "By" + joinPascal(q.Count)
		filterFields := resolveFilterFields(q.Count, tableObj)
		m := repoMethod(
			fmt.Sprintf("Count%ss%s", modelName, suffix),
			tableObj, spec.QueryCount, filterFields, "",
		)
		methods = append(methods, m)
		return methods
	}

	if q.Paginate != nil {
		paginateKind := "cursor"
		if s, ok := q.Paginate.(string); ok {
			paginateKind = s
		}
		m := repoMethod(fmt.Sprintf("List%ss", modelName), tableObj, spec.QueryPaginate, nil, "")
		m.Touches[0].PaginationKind = paginateKind
		if len(q.OrderBy) > 0 {
			ob := q.OrderBy[0]
			if fn, ok := ob["field"]; ok {
				for i := range tableObj.Fields {
					if tableObj.Fields[i].Name == toPascalCase(fn) {
						m.Touches[0].OrderByField = &tableObj.Fields[i]
						break
					}
				}
			}
			if dir, ok := ob["direction"]; ok {
				m.Touches[0].OrderDir = dir
			}
		}
		m.Touches[0].DefaultLimit = q.DefaultLimit
		methods = append(methods, m)
		return methods
	}

	if q.Custom != "" {
		m := repoMethod(toPascalCase(q.Custom), tableObj, spec.QueryCustom, nil, q.SQL)
		m.Touches[0].ReturnsMany = q.Returns != "single"
		for _, p := range q.Params {
			td := MapType(p.Type, spec.LangGo, spec.DBPostgres, false)
			m.Touches[0].CustomParams = append(m.Touches[0].CustomParams, spec.ResolvedParam{Name: p.Name, Type: td})
		}
		if q.SQL != "" && strings.Contains(strings.ToUpper(q.SQL), "RETURNING") {
			m.Touches[0].ScanInto = &spec.ResolvedParam{Name: "result", Type: spec.TypeDescriptor{GoType: modelName}}
		}
		methods = append(methods, m)
		return methods
	}

	return methods
}

// resolveFilterFields looks up the resolved fields for a list of field names.
func resolveFilterFields(fieldNames []string, tableObj *spec.ResolvedObject) []spec.ResolvedField {
	if tableObj == nil {
		return nil
	}
	var out []spec.ResolvedField
	for _, fn := range fieldNames {
		pascal := toPascalCase(fn)
		for _, f := range tableObj.Fields {
			if f.Name == pascal {
				out = append(out, f)
				break
			}
		}
	}
	return out
}

// ─── buildCacheImpl ───────────────────────────────────────────────────────────

func buildCacheImpl(iface spec.CacheInterfaceAST, cacheInterface *spec.ResolvedInterface, valueTypeObj *spec.ResolvedObject) spec.ResolvedImplementation {
	impl := spec.ResolvedImplementation{
		Name:       iface.Name + "Impl",
		Path:       fmt.Sprintf("generated/cache/%s/cache_impl.go", toSnakeCase(iface.Name)),
		Kind:       spec.CacheImpl,
		Implements: cacheInterface,
		Imports:    make(map[spec.Lang]spec.ImportSet),
		Dependencies: []spec.ResolvedDependency{
			{FieldName: "BaseCache", TypeName: "cache.BaseCache", Import: "github.com/stencil-run/stencil-go/cache"},
		},
	}

	// Resolve key params from the key_template
	paramNames := parseKeyTemplate(iface.KeyTemplate)
	var kParams []spec.ResolvedParam
	var kp []keyParam
	for _, pn := range paramNames {
		var td spec.TypeDescriptor
		verb := "%v"
		uuidStr := false
		if valueTypeObj != nil {
			for _, f := range valueTypeObj.Fields {
				if f.Name == toPascalCase(pn) || toSnakeCase(f.Name) == pn {
					td = f.Type
					verb = FormatVerb(f.Type.Kind)
					uuidStr = f.Type.Kind == spec.TypeUUID
					break
				}
			}
		}
		kParams = append(kParams, spec.ResolvedParam{Name: pn, Type: td})
		kp = append(kp, keyParam{name: pn, verb: verb, uuidStr: uuidStr})
	}

	keyFunc := renderKeyFunc(iface.KeyTemplate, kp)
	defaultTTL := parseDuration(iface.DefaultTTL)

	for _, method := range iface.Methods {
		var cacheOp spec.CacheOpKind
		switch method {
		case "Get":
			cacheOp = spec.CacheOpGet
		case "Set":
			cacheOp = spec.CacheOpSet
		case "Delete":
			cacheOp = spec.CacheOpDelete
		case "Invalidate":
			cacheOp = spec.CacheOpInvalidate
		}

		touch := spec.ResolvedTouch{
			Kind:         spec.TouchKindCacheOp,
			CacheRef:     cacheInterface,
			CacheOp:      cacheOp,
			ValueTypeRef: valueTypeObj,
			KeyParams:    kParams,
			KeyTemplate:  iface.KeyTemplate,
			KeyFunc:      keyFunc,
			DefaultTTL:   defaultTTL,
		}
		impl.Methods = append(impl.Methods, spec.ResolvedMethod{
			FunctionName: method,
			Touches:      []spec.ResolvedTouch{touch},
		})
	}

	return impl
}

// ─── buildExternalImpl ────────────────────────────────────────────────────────

func buildExternalImpl(ext spec.ExternalAST, extIface *spec.ResolvedInterface, objects []*spec.ResolvedObject) spec.ResolvedImplementation {
	impl := spec.ResolvedImplementation{
		Name:       toPascalCase(ext.Name) + "Impl",
		Path:       fmt.Sprintf("generated/external/%s/client_impl.go", toSnakeCase(ext.Name)),
		Kind:       spec.ExternalImpl,
		Implements: extIface,
		Imports:    make(map[spec.Lang]spec.ImportSet),
	}

	// Base dependencies
	impl.Dependencies = []spec.ResolvedDependency{
		{FieldName: "client", TypeName: "*http.Client", Import: "net/http"},
		{FieldName: "baseURL", TypeName: "string"},
	}
	// Auth dependencies
	switch ext.Auth {
	case "bearer_token":
		impl.Dependencies = append(impl.Dependencies, spec.ResolvedDependency{FieldName: "token", TypeName: "string"})
	case "api_key":
		impl.Dependencies = append(impl.Dependencies, spec.ResolvedDependency{FieldName: "apiKey", TypeName: "string"})
	}

	// Retry config from AST
	retryAttempts := 0
	retryBackoff := ""
	var retryOnStatus []int
	if ext.Retry != nil {
		retryAttempts = ext.Retry.Attempts
		retryBackoff = ext.Retry.Backoff
		retryOnStatus = ext.Retry.OnStatus
	}

	for _, call := range ext.Calls {
		// Resolve request/response body objects
		var reqBodyRef *spec.ResolvedObject
		var respBodyRef *spec.ResolvedObject
		for _, obj := range objects {
			if obj == nil {
				continue
			}
			if call.Body != nil && obj.Name == toPascalCase(call.Body.Name) {
				reqBodyRef = obj
			}
			if call.Response != nil && obj.Name == toPascalCase(call.Response.Name) {
				respBodyRef = obj
			}
		}

		// Status errors
		var statusErrors []spec.ResolvedStatusError
		for _, e := range call.Errors {
			statusErrors = append(statusErrors, spec.ResolvedStatusError{
				Status:    e.Status,
				ErrorName: e.Error,
			})
		}

		touch := spec.ResolvedTouch{
			Kind:            spec.TouchKindHTTPCall,
			HTTPMethod:      call.Method,
			PathTemplate:    call.Path,
			RequestBodyRef:  reqBodyRef,
			ResponseBodyRef: respBodyRef,
			StatusErrors:    statusErrors,
			RetryAttempts:   retryAttempts,
			RetryBackoff:    retryBackoff,
			RetryOnStatus:   retryOnStatus,
			AuthKind:        ext.Auth,
			Timeout:         parseDuration(ext.Timeout),
		}

		impl.Methods = append(impl.Methods, spec.ResolvedMethod{
			FunctionName: toPascalCase(call.Name),
			Touches:      []spec.ResolvedTouch{touch},
		})
	}

	return impl
}

// ─── buildExternalMock ────────────────────────────────────────────────────────

func buildExternalMock(ext spec.ExternalAST, extIface *spec.ResolvedInterface) spec.ResolvedImplementation {
	return spec.ResolvedImplementation{
		Name:       toPascalCase(ext.Name) + "Mock",
		Path:       fmt.Sprintf("generated/external/%s/client_mock.go", toSnakeCase(ext.Name)),
		Kind:       spec.ExternalMockImpl,
		Implements: extIface,
		Imports:    make(map[spec.Lang]spec.ImportSet),
		// No Methods, no Dependencies — generator derives from the interface's Functions list
	}
}

// ─── buildCacheMock ───────────────────────────────────────────────────────────

func buildCacheMock(iface spec.CacheInterfaceAST, cacheInterface *spec.ResolvedInterface) spec.ResolvedImplementation {
	return spec.ResolvedImplementation{
		Name:       iface.Name + "Mock",
		Path:       fmt.Sprintf("generated/cache/%s/cache_mock.go", toSnakeCase(iface.Name)),
		Kind:       spec.CacheMockImpl,
		Implements: cacheInterface,
		Imports:    make(map[spec.Lang]spec.ImportSet),
		// No Methods, no Dependencies — generator derives from the interface's Functions list
	}
}

// ─── buildDefaultMapperImpl ───────────────────────────────────────────────────

// buildDefaultMapperImpl produces the DefaultMapperImpl for one API.
// It holds FieldMappings computed by resolveFieldMappings — the generator reads
// these to render each MapXxxInput / MapResponse method body.
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
		Imports:       make(map[spec.Lang]spec.ImportSet),
		FieldMappings: resolveFieldMappings(api, ctxObj, objects, externals),
	}
}

// ─── resolveFieldMappings ─────────────────────────────────────────────────────

// resolveFieldMappings runs the field-matching algorithm for one API.
// For each step's StepInput and for the ResponseDTO, it tries to find a source
// in the Request or in earlier steps' outputs. Unmatchable fields get MustOverride=true.
func resolveFieldMappings(
	api spec.APIAST,
	ctxObj *spec.ResolvedObject,
	objects []*spec.ResolvedObject,
	externals []spec.ExternalAST,
) []spec.ResolvedFieldMapping {
	apiName := toPascalCase(api.Name)

	// Request fields available as sources from the start
	reqObj := findObjectByName(objects, apiName+"Request")

	var mappings []spec.ResolvedFieldMapping

	// Track outputs available from steps that have already run (in declaration order)
	var availableOutputs []*spec.ResolvedObject

	systemField := func(name string) bool {
		return name == "ID" || name == "CreatedAt" || name == "UpdatedAt" || name == "DeletedAt"
	}

	for _, step := range api.Steps {
		// Resolve the actual input object for this step (ExternalInput or TableModel directly)
		var inputObj *spec.ResolvedObject
		var skipSystemFields bool
		t := step.Touch
		if t.External != "" {
			if bodyName := findExternalCallBodyName(t.External, t.Method, externals); bodyName != "" {
				inputObj = findObjectByName(objects, bodyName)
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

// inferFieldMapping tries to find a source for one target field.
// Rule 1: same Name + compatible Kind in Request.
// Rule 2: same Name + compatible Kind in any earlier step output.
// Rule 3: if neither matches, mark MustOverride.
func inferFieldMapping(
	methodName string,
	target spec.ResolvedField,
	reqObj *spec.ResolvedObject,
	availableOutputs []*spec.ResolvedObject,
) spec.ResolvedFieldMapping {
	// Rule 1: match in request
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

	// Rule 3: cannot infer
	return spec.ResolvedFieldMapping{
		MethodName:   methodName,
		TargetField:  target.Name,
		MustOverride: true,
		Reason:       fmt.Sprintf("no matching source found for field %q by name and type", target.Name),
	}
}

// matchFieldInObject returns the source path if a field with the same name and
// compatible TypeKind exists in the source object; otherwise returns "".
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

// ─── buildTransactionImpl ─────────────────────────────────────────────────────

func buildTransactionImpl(tx spec.TransactionAST, objects []*spec.ResolvedObject) spec.ResolvedImplementation {
	// Find the params object
	paramsName := toPascalCase(tx.Name) + "Params"
	var paramsObj *spec.ResolvedObject
	for _, obj := range objects {
		if obj != nil && obj.Name == paramsName {
			paramsObj = obj
			break
		}
	}

	impl := spec.ResolvedImplementation{
		Name:    toPascalCase(tx.Name) + "Tx",
		Path:    fmt.Sprintf("generated/tx/%s/tx_impl.go", toSnakeCase(tx.Name)),
		Kind:    spec.TransactionImpl,
		Imports: make(map[spec.Lang]spec.ImportSet),
		Dependencies: []spec.ResolvedDependency{
			{FieldName: "db", TypeName: "*sql.DB", Import: "database/sql"},
		},
	}

	// One Execute method with N TxStep touches
	executeMethod := spec.ResolvedMethod{
		FunctionName:   "Execute",
		InputParamsRef: paramsObj,
	}

	for _, step := range tx.Steps {
		// Build typed step params
		var stepParams []spec.ResolvedParam
		for _, p := range step.Params {
			td := MapType(p.Type, spec.LangGo, spec.DBPostgres, false)
			stepParams = append(stepParams, spec.ResolvedParam{
				Name: toPascalCase(p.Name),
				Type: td,
			})
		}

		touch := spec.ResolvedTouch{
			Kind:       spec.TouchKindTxStep,
			StepName:   step.Name,
			SQL:        step.SQL,
			StepParams: stepParams,
			ErrorName:  step.Error,
		}
		if step.ErrorIfRows != nil {
			n := *step.ErrorIfRows
			touch.ErrorIfRows = &n
		}

		executeMethod.Touches = append(executeMethod.Touches, touch)
	}

	impl.Methods = append(impl.Methods, executeMethod)
	return impl
}

// ─── buildServiceImpl ─────────────────────────────────────────────────────────

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
		Imports:    make(map[spec.Lang]spec.ImportSet),
	}

	// Dependency tracking
	depByKey := make(map[string]spec.ResolvedDependency)

	addDep := func(key, fieldName, typeName, importPath string) {
		if _, ok := depByKey[key]; !ok {
			depByKey[key] = spec.ResolvedDependency{
				FieldName: fieldName,
				TypeName:  typeName,
				Import:    importPath,
			}
		}
	}

	for _, api := range group.APIs {
		// Find SharedContext object for this API
		ctxName := toPascalCase(api.Name) + "Context"
		var ctxObj *spec.ResolvedObject
		for _, obj := range objects {
			if obj != nil && obj.Name == ctxName {
				ctxObj = obj
				break
			}
		}

		// Hooks dependency per API
		hooksTypeName := "*" + toPascalCase(api.Name) + "Hooks"
		hooksFieldName := toPascalCase(api.Name) + "Hooks"
		addDep("hooks:"+api.Name, strings.ToLower(string(hooksFieldName[0]))+hooksFieldName[1:], hooksTypeName, "")

		// Look up the MapperInterface for this API (present when api.Steps is non-empty)
		mapperIfaceName := toPascalCase(api.Name) + "Mappers"
		var mapperRef *spec.ResolvedInterface
		for _, iface := range interfaces {
			if iface != nil && iface.Name == mapperIfaceName {
				mapperRef = iface
				break
			}
		}

		method := spec.ResolvedMethod{
			FunctionName:   toPascalCase(api.Name),
			SharedContext:  ctxObj,
			ExecutionModel: spec.Sequential, // all APIs use sequential execution for now
			MapperRef:      mapperRef,
		}

		for _, step := range api.Steps {
			t := step.Touch
			outputField := contextOutputFieldName(step.ID, touchKindStr(t), touchName(t), touchOp(t))

			var touch spec.ResolvedTouch

			if t.Table != "" {
				// Find repo interface for this table
				repoName := modelName(t.Table) + "Repository"
				var repoIface *spec.ResolvedInterface
				for _, iface := range interfaces {
					if iface != nil && iface.Name == repoName {
						repoIface = iface
						break
					}
				}

				// Find the specific QueryRef function matching the op
				var queryRef *spec.ResolvedFunction
				if repoIface != nil {
					targetFnName := mapOpToRepoFunc(t.Op, modelName(t.Table), t)
					for i := range repoIface.Functions {
						if repoIface.Functions[i].Name == targetFnName {
							queryRef = &repoIface.Functions[i]
							break
						}
					}
				}

				// Add repo dependency
				repoFieldName := strings.ToLower(string(repoName[0])) + repoName[1:]
				addDep("repo:"+t.Table, repoFieldName, "*"+repoName, "")

				fatal := step.Fatal != nil && *step.Fatal
				touch = spec.ResolvedTouch{
					Kind:         spec.TouchKindTable,
					TableRef:     findObjectByName(objects, modelName(t.Table)),
					QueryRef:     queryRef,
					ExternalRef:  repoIface,
					Op:           t.Op,
					ResultField:  outputField,
					FatalError:   fatal,
				}

			} else if t.External != "" {
				// Find external interface
				extIfaceName := toPascalCase(t.External)
				var extIface *spec.ResolvedInterface
				for _, iface := range interfaces {
					if iface != nil && iface.Name == extIfaceName {
						extIface = iface
						break
					}
				}

				// Find the specific method
				var extMethod *spec.ResolvedFunction
				methodName := toPascalCase(t.Method)
				if extIface != nil {
					for i := range extIface.Functions {
						if extIface.Functions[i].Name == methodName {
							extMethod = &extIface.Functions[i]
							break
						}
					}
				}

				// Add external dependency
				extFieldName := strings.ToLower(string(extIfaceName[0])) + extIfaceName[1:]
				addDep("ext:"+t.External, extFieldName, "*"+extIfaceName+"Impl", "")

				fatal := step.Fatal != nil && *step.Fatal
				touch = spec.ResolvedTouch{
					Kind:           spec.TouchKindExternal,
					ExternalRef:    extIface,
					ExternalMethod: extMethod,
					Op:             t.Method,
					ResultField:    outputField,
					FatalError:     fatal,
				}

			} else if t.Cache != "" {
				// Find cache interface
				cacheIfaceName := t.Cache
				var cacheIface *spec.ResolvedInterface
				for _, iface := range interfaces {
					if iface != nil && iface.Name == cacheIfaceName {
						cacheIface = iface
						break
					}
				}

				// Find the specific cache method
				var cacheMethod *spec.ResolvedFunction
				if cacheIface != nil {
					methodName := toPascalCase(t.Op)
					for i := range cacheIface.Functions {
						if cacheIface.Functions[i].Name == methodName {
							cacheMethod = &cacheIface.Functions[i]
							break
						}
					}
				}

				// Add cache dependency
				cacheFieldName := strings.ToLower(string(cacheIfaceName[0])) + cacheIfaceName[1:]
				addDep("cache:"+t.Cache, cacheFieldName, "*"+cacheIfaceName+"Impl", "")

				fatal := step.Fatal != nil && *step.Fatal
				touch = spec.ResolvedTouch{
					Kind:        spec.TouchKindCache,
					CacheRef:    cacheIface,
					CacheMethod: cacheMethod,
					Op:          t.Op,
					ResultField: outputField,
					FatalError:  fatal,
				}

			} else if t.Transaction != "" {
				// Transactions: no dependency on interface, just on the impl
				txImplName := toPascalCase(t.Transaction) + "Tx"
				txFieldName := strings.ToLower(string(txImplName[0])) + txImplName[1:]
				addDep("tx:"+t.Transaction, txFieldName, "*"+txImplName, "")

				fatal := step.Fatal != nil && *step.Fatal
				touch = spec.ResolvedTouch{
					Kind:        spec.TouchKindTransaction,
					Op:          "Execute",
					ResultField: outputField,
					FatalError:  fatal,
				}

			} else if t.Message != "" {
				// Messaging producer
				addDep("producer", "producer", "*messaging.Producer", "")

				touch = spec.ResolvedTouch{
					Kind:        spec.TouchKindMessage,
					MessageName: t.Message,
					ResultField: outputField,
				}
			}

			method.Touches = append(method.Touches, touch)
		}

		impl.Methods = append(impl.Methods, method)
	}

	// Flatten dependency map preserving insertion order as much as possible
	// (Go map iteration is random, but for Phase 1 brute force this is acceptable)
	for _, dep := range depByKey {
		impl.Dependencies = append(impl.Dependencies, dep)
	}

	return impl
}

// mapOpToRepoFunc converts a touch op to the corresponding repository function name.
func mapOpToRepoFunc(op string, model string, t spec.TouchAST) string {
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

func findObjectByName(objects []*spec.ResolvedObject, name string) *spec.ResolvedObject {
	for _, obj := range objects {
		if obj != nil && obj.Name == name {
			return obj
		}
	}
	return nil
}
