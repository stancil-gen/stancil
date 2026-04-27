package resolver

import (
	"fmt"
	"strings"

	"stencil/internal/spec"
)

// TablesFeature resolves the `tables:` block.
// Owns all three levels for each table:
//   - Level 1: table model (ResolvedObject of kind TableModel)
//   - Level 2: repository interface (ResolvedInterface of kind RepositoryInterface)
//   - Level 3: repository implementation (ResolvedImplementation of kind RepositoryImpl)
type TablesFeature struct{}

func (f *TablesFeature) Name() string { return "tables" }

func (f *TablesFeature) Validate(ast *spec.SpecAST) []error { return nil }

func (f *TablesFeature) Resolve(ast *spec.SpecAST, ir *spec.ResolvedSpec) error {
	for _, t := range ast.Tables {
		// Level 1: model object
		modelObj := buildTableModel(t)
		ir.Objects = append(ir.Objects, modelObj)
		modelPtr := &ir.Objects[len(ir.Objects)-1]

		// Level 2: repository interface
		repoIface := buildRepositoryInterface(t, modelPtr)
		ir.Interfaces = append(ir.Interfaces, repoIface)
		ifacePtr := &ir.Interfaces[len(ir.Interfaces)-1]

		// Level 3: repository implementation
		repoImpl := buildRepositoryImpl(t, modelPtr, ifacePtr)
		ir.Implementations = append(ir.Implementations, repoImpl)
	}
	return nil
}

// ─── Level 1: buildTableModel ─────────────────────────────────────────────────

func buildTableModel(t spec.TableAST) spec.ResolvedObject {
	name := modelName(t.Name)
	obj := spec.ResolvedObject{
		Name:       name,
		Path:       fmt.Sprintf("generated/repo/%s/model.go", t.Name),
		Kind:       spec.TableModel,
		TableName:  t.Name,
		PrimaryKey: "ID",
		SoftDelete: t.SoftDelete,
		BulkCreate: t.BulkCreate,
	}

	// Inject implicit ID field first
	obj.Fields = append(obj.Fields, spec.ResolvedField{
		Name:     "ID",
		DBColumn: "id",
		Type:     spec.TypeDescriptor{Kind: spec.TypeUUID, DBType: "UUID"},
		Required: true,
		Unique:   true,
	})

	// User-declared fields
	for _, f := range t.Fields {
		obj.Fields = append(obj.Fields, buildField(f))
	}

	// Inject timestamps
	tsType := spec.TypeDescriptor{Kind: spec.TypeTimestamp, DBType: "TIMESTAMP"}
	obj.Fields = append(obj.Fields,
		spec.ResolvedField{Name: "CreatedAt", DBColumn: "created_at", Type: tsType},
		spec.ResolvedField{Name: "UpdatedAt", DBColumn: "updated_at", Type: tsType},
	)

	// Inject soft delete field
	if t.SoftDelete {
		obj.Fields = append(obj.Fields, spec.ResolvedField{
			Name:     "DeletedAt",
			DBColumn: "deleted_at",
			Type:     spec.TypeDescriptor{Kind: spec.TypeTimestamp, DBType: "TIMESTAMP", Nullable: true},
			Nullable: true,
		})
	}

	// Table error declarations
	for _, e := range t.Errors {
		obj.Errors = append(obj.Errors, spec.ResolvedError{
			Code:    e.Code,
			Name:    e.Name,
			VarName: "Err" + name + e.Name,
			Message: toSnakeCase(name) + ": " + toErrorMessage(e.Name),
		})
	}

	// Derive indexes from find_by queries
	for _, q := range t.Queries {
		if len(q.FindBy) > 0 {
			fieldNames := make([]string, len(q.FindBy))
			for i, f := range q.FindBy {
				fieldNames[i] = toSnakeCase(toPascalCase(f))
			}
			obj.Indexes = append(obj.Indexes, spec.ResolvedIndex{
				Fields: fieldNames,
				Unique: false,
				Name:   fmt.Sprintf("idx_%s_%s", t.Name, strings.Join(fieldNames, "_")),
			})
		}
	}

	return obj
}

// ─── Level 2: buildRepositoryInterface ───────────────────────────────────────

func buildRepositoryInterface(table spec.TableAST, tableObj *spec.ResolvedObject) spec.ResolvedInterface {
	name := modelName(table.Name)
	iface := spec.ResolvedInterface{
		Name: name + "Repository",
		Path: fmt.Sprintf("generated/repo/%s/repo.go", table.Name),
		Kind: spec.RepositoryInterface,
	}

	// Standard CRUD — always generated
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

	// Query shorthands
	for _, q := range table.Queries {
		fns := expandQuery(q, name, tableObj)
		iface.Functions = append(iface.Functions, fns...)
	}

	return iface
}

// expandQuery converts a QueryAST shorthand into one or more ResolvedFunctions.
func expandQuery(q spec.QueryAST, modelName string, tableObj *spec.ResolvedObject) []spec.ResolvedFunction {
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
			[]spec.ResolvedReturn{kindReturn(spec.TypeBool)},
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
			[]spec.ResolvedReturn{kindReturn(spec.TypeInt)},
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
			params = []spec.ResolvedParam{
				kindParam("page", spec.TypeInt),
				kindParam("limit", spec.TypeInt),
			}
			// Returns []*Model + int (total count)
			returns = []spec.ResolvedReturn{slicePtrReturn(modelName), kindReturn(spec.TypeInt)}
		} else {
			params = []spec.ResolvedParam{
				kindParam("cursor", spec.TypeStr),
				kindParam("limit", spec.TypeInt),
			}
			// Returns []*Model + string (next cursor)
			returns = []spec.ResolvedReturn{slicePtrReturn(modelName), kindReturn(spec.TypeStr)}
		}
		fns = append(fns, repoFunc(fmt.Sprintf("List%ss", modelName), params, returns, spec.QueryPaginate))
		return fns
	}

	if q.Custom != "" {
		var params []spec.ResolvedParam
		for _, p := range q.Params {
			td := MapType(p.Type, false)
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
		p := kindParam(fn, spec.TypeAny)
		if tableObj != nil {
			fieldPascal := toPascalCase(fn)
			for _, f := range tableObj.Fields {
				if f.Name == fieldPascal {
					// Use the field's type but non-nullable for query params
					td := f.Type
					td.Nullable = false
					td.IsList = false
					p = spec.ResolvedParam{Name: fn, Type: td}
					break
				}
			}
		}
		params = append(params, p)
	}
	return params
}

// repoFunc builds a ResolvedFunction with typed params, returns, and a QueryKind tag.
func repoFunc(name string, params []spec.ResolvedParam, returns []spec.ResolvedReturn, qk spec.QueryKind) spec.ResolvedFunction {
	return spec.ResolvedFunction{
		Name:      name,
		Params:    params,
		Returns:   returns,
		QueryKind: &qk,
	}
}

// ─── Level 3: buildRepositoryImpl ────────────────────────────────────────────

func buildRepositoryImpl(table spec.TableAST, tableObj *spec.ResolvedObject, repoIface *spec.ResolvedInterface) spec.ResolvedImplementation {
	name := modelName(table.Name)
	impl := spec.ResolvedImplementation{
		Name:       name + "RepositoryImpl",
		Path:       fmt.Sprintf("generated/repo/%s/repo_impl.go", table.Name),
		Kind:       spec.RepositoryImpl,
		Implements: repoIface,
		Dependencies: []spec.ResolvedDependency{
			{FieldName: "db", TypeName: "*gorm.DB", Import: "gorm.io/gorm"},
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
		Kind:         spec.TouchKindQuery,
		TableRef:     tableObj,
		QueryKind:    qk,
		FilterFields: filterFields,
		CustomSQL:    customSQL,
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
		var funcName string
		if q.Returns == "single" {
			funcName = fmt.Sprintf("Get%s%s", modelName, suffix)
		} else {
			funcName = fmt.Sprintf("Get%ss%s", modelName, suffix)
		}
		m := repoMethod(funcName, tableObj, spec.QueryFindBy, filterFields, "")
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
			td := MapType(p.Type, false)
			m.Touches[0].CustomParams = append(m.Touches[0].CustomParams, spec.ResolvedParam{Name: p.Name, Type: td})
		}
		if q.SQL != "" && strings.Contains(strings.ToUpper(q.SQL), "RETURNING") {
			m.Touches[0].ScanInto = &spec.ResolvedParam{
				Name: "result",
				Type: spec.TypeDescriptor{Kind: spec.TypeCustom, CustomName: modelName, IsCustom: true},
			}
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
