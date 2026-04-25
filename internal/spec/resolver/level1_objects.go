package resolver

import (
	"fmt"
	"strings"

	"stencil/internal/spec"
)

// ─── Level 1A: linkTypeRefs ───────────────────────────────────────────────────

// linkTypeRefs is a second pass that fills TypeRef pointers on all custom-type fields.
// Called once after all Level 1 objects are built.
// Phase 1 brute force — if a TypeRef can't be found, it stays nil (validator caught bad refs).
func linkTypeRefs(objects []spec.ResolvedObject) {
	index := make(map[string]*spec.ResolvedObject, len(objects))
	for i := range objects {
		index[objects[i].Name] = &objects[i]
	}

	for i := range objects {
		for j := range objects[i].Fields {
			f := &objects[i].Fields[j]
			if f.Type.IsCustom {
				// GoType is "*Money" — strip the "*" to get the name
				name := strings.TrimPrefix(f.Type.GoType, "*")
				f.TypeRef = index[name]
			}
		}
	}
}

// ─── buildTypeObject ─────────────────────────────────────────────────────────

func buildTypeObject(t spec.CustomTypeAST, lang spec.Lang, db spec.DBDriver) spec.ResolvedObject {
	obj := spec.ResolvedObject{
		Name:    toPascalCase(t.Name),
		Path:    "generated/types/types.go",
		Kind:    spec.TypeObject,
		Imports: make(map[spec.Lang]spec.ImportSet),
	}

	for _, f := range t.Fields {
		obj.Fields = append(obj.Fields, buildField(f, lang, db))
	}

	return obj
}

// ─── buildTableModel ─────────────────────────────────────────────────────────

func buildTableModel(t spec.TableAST, lang spec.Lang, db spec.DBDriver) spec.ResolvedObject {
	name := modelName(t.Name)
	obj := spec.ResolvedObject{
		Name:       name,
		Path:       fmt.Sprintf("generated/repo/%s/model.go", t.Name),
		Kind:       spec.TableModel,
		TableName:  t.Name,
		PrimaryKey: "ID",
		SoftDelete: t.SoftDelete,
		BulkCreate: t.BulkCreate,
		Imports:    make(map[spec.Lang]spec.ImportSet),
	}

	// Inject implicit ID field first
	idTd := MapType("uuid", lang, db, false)
	obj.Fields = append(obj.Fields, spec.ResolvedField{
		Name:        "ID",
		DBColumn:    "id",
		Type:        idTd,
		Required:    true,
		GoStructTag: deriveGoStructTag("ID", true, true, nil),
	})

	// User-declared fields
	for _, f := range t.Fields {
		obj.Fields = append(obj.Fields, buildField(f, lang, db))
	}

	// Inject timestamps
	tsTd := MapType("timestamp", lang, db, false)
	obj.Fields = append(obj.Fields,
		spec.ResolvedField{
			Name:        "CreatedAt",
			DBColumn:    "created_at",
			Type:        tsTd,
			GoStructTag: deriveGoStructTag("CreatedAt", false, false, nil),
		},
		spec.ResolvedField{
			Name:        "UpdatedAt",
			DBColumn:    "updated_at",
			Type:        tsTd,
			GoStructTag: deriveGoStructTag("UpdatedAt", false, false, nil),
		},
	)

	// Inject soft delete field
	if t.SoftDelete {
		delTd := MapType("timestamp", lang, db, true) // nullable
		obj.Fields = append(obj.Fields, spec.ResolvedField{
			Name:        "DeletedAt",
			DBColumn:    "deleted_at",
			Type:        delTd,
			Nullable:    true,
			GoStructTag: deriveGoStructTag("DeletedAt", false, false, nil),
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

// ─── buildRequestDTO / buildResponseDTO ──────────────────────────────────────

func buildRequestDTO(api spec.APIAST, group spec.ResourceGroupAST, lang spec.Lang, db spec.DBDriver) *spec.ResolvedObject {
	if api.DTOs == nil || api.DTOs.Request == nil {
		return nil
	}
	dto := api.DTOs.Request
	obj := spec.ResolvedObject{
		Name:    toPascalCase(dto.Name),
		Path:    fmt.Sprintf("generated/handler/%s/types.go", toSnakeCase(group.Group)),
		Kind:    spec.RequestDTO,
		Imports: make(map[spec.Lang]spec.ImportSet),
	}
	for _, f := range dto.Fields {
		obj.Fields = append(obj.Fields, buildDTOField(f, lang, db))
	}
	return &obj
}

func buildResponseDTO(api spec.APIAST, group spec.ResourceGroupAST, lang spec.Lang, db spec.DBDriver) *spec.ResolvedObject {
	if api.DTOs == nil || api.DTOs.Response == nil {
		return nil
	}
	dto := api.DTOs.Response
	obj := spec.ResolvedObject{
		Name:    toPascalCase(dto.Name),
		Path:    fmt.Sprintf("generated/handler/%s/types.go", toSnakeCase(group.Group)),
		Kind:    spec.ResponseDTO,
		Imports: make(map[spec.Lang]spec.ImportSet),
	}
	for _, f := range dto.Fields {
		obj.Fields = append(obj.Fields, buildDTOField(f, lang, db))
	}
	return &obj
}

// ─── buildSharedContext ───────────────────────────────────────────────────────

// buildSharedContext builds the per-API execution context object.
// SharedContext is pure data flow — NO flags.
// Fields: Request (incoming) + per-step Output (result from each step) + Response (outgoing)
func buildSharedContext(api spec.APIAST, group spec.ResourceGroupAST, lang spec.Lang, db spec.DBDriver) spec.ResolvedObject {
	name := toPascalCase(api.Name) + "Context"
	obj := spec.ResolvedObject{
		Name:    name,
		Path:    fmt.Sprintf("generated/handler/%s/types.go", toSnakeCase(group.Group)),
		Kind:    spec.SharedContext,
		Imports: make(map[spec.Lang]spec.ImportSet),
	}

	// 1. Request field
	if api.DTOs != nil && api.DTOs.Request != nil {
		reqName := toPascalCase(api.DTOs.Request.Name)
		obj.Fields = append(obj.Fields, spec.ResolvedField{
			Name:     "Request",
			DBColumn: "request",
			Type: spec.TypeDescriptor{
				Kind:      spec.TypeCustom,
				GoType:    "*" + reqName,
				IsCustom:  true,
				GoPointer: true,
			},
			GoStructTag: "`json:\"request\"`",
		})
	}

	// 2. Per-step Output fields (no flags — control flow lives in user's graph engine)
	for _, step := range api.Steps {
		outputFieldName := contextOutputFieldName(step.ID, touchKindStr(step.Touch), touchName(step.Touch), touchOp(step.Touch))
		outputType := deriveStepOutputType(step.Touch, lang, db)
		if outputType.GoType == "" {
			continue // skip steps that produce no meaningful output (e.g. delete)
		}
		obj.Fields = append(obj.Fields, spec.ResolvedField{
			Name:        outputFieldName,
			DBColumn:    toSnakeCase(outputFieldName),
			Type:        outputType,
			GoStructTag: deriveGoStructTag(outputFieldName, false, false, nil),
		})
	}

	// 3. Response field
	if api.DTOs != nil && api.DTOs.Response != nil {
		respName := toPascalCase(api.DTOs.Response.Name)
		obj.Fields = append(obj.Fields, spec.ResolvedField{
			Name:     "Response",
			DBColumn: "response",
			Type: spec.TypeDescriptor{
				Kind:      spec.TypeCustom,
				GoType:    "*" + respName,
				IsCustom:  true,
				GoPointer: true,
			},
			GoStructTag: "`json:\"response\"`",
		})
	}

	return obj
}

// deriveStepOutputType returns the TypeDescriptor for what a step produces.
func deriveStepOutputType(touch spec.TouchAST, lang spec.Lang, db spec.DBDriver) spec.TypeDescriptor {
	if touch.Table != "" {
		name := modelName(touch.Table)
		switch touch.Op {
		case "list":
			return spec.TypeDescriptor{Kind: spec.TypeCustom, GoType: "[]*" + name, IsCustom: true}
		case "delete":
			return spec.TypeDescriptor{} // no output
		default: // create, get, update
			return spec.TypeDescriptor{Kind: spec.TypeCustom, GoType: "*" + name, IsCustom: true, GoPointer: true}
		}
	}
	if touch.External != "" {
		// Output is *{MethodResponseDTO} — we don't know the exact type without the call spec
		// Use interface{} as placeholder for Phase 1 when response type name isn't available
		return spec.TypeDescriptor{Kind: spec.TypeCustom, GoType: "interface{}", IsCustom: false}
	}
	if touch.Cache != "" {
		switch touch.Op {
		case "get":
			return spec.TypeDescriptor{Kind: spec.TypeCustom, GoType: "interface{}", IsCustom: false}
		default: // set, delete, invalidate
			return spec.TypeDescriptor{} // no output
		}
	}
	if touch.Transaction != "" {
		// Transaction result (Phase 1: use interface{})
		return spec.TypeDescriptor{Kind: spec.TypeCustom, GoType: "interface{}", IsCustom: false}
	}
	if touch.Message != "" {
		return spec.TypeDescriptor{} // no output from publish
	}
	return spec.TypeDescriptor{}
}

// Helper: what kind name string for a touch (for hookSuffix and field naming)
func touchKindStr(t spec.TouchAST) string {
	if t.Table != "" {
		return "table"
	}
	if t.External != "" {
		return "external"
	}
	if t.Cache != "" {
		return "cache"
	}
	if t.Transaction != "" {
		return "transaction"
	}
	if t.Message != "" {
		return "message"
	}
	return "unknown"
}

func touchName(t spec.TouchAST) string {
	if t.Table != "" {
		return t.Table
	}
	if t.External != "" {
		return t.External
	}
	if t.Cache != "" {
		return t.Cache
	}
	if t.Transaction != "" {
		return t.Transaction
	}
	if t.Message != "" {
		return t.Message
	}
	return ""
}

func touchOp(t spec.TouchAST) string {
	if t.Op != "" {
		return t.Op
	}
	if t.Method != "" {
		return t.Method
	}
	if t.External != "" {
		return t.Method
	}
	return ""
}

// ─── buildExternalInput / buildExternalOutput ─────────────────────────────────

func buildExternalInput(ext spec.ExternalAST, call spec.ExternalCallAST, lang spec.Lang, db spec.DBDriver) *spec.ResolvedObject {
	if call.Body == nil {
		return nil
	}
	obj := &spec.ResolvedObject{
		Name:    toPascalCase(call.Body.Name),
		Path:    fmt.Sprintf("generated/external/%s/types.go", ext.Name),
		Kind:    spec.ExternalInput,
		Imports: make(map[spec.Lang]spec.ImportSet),
	}
	for _, f := range call.Body.Fields {
		obj.Fields = append(obj.Fields, buildField(f, lang, db))
	}
	return obj
}

func buildExternalOutput(ext spec.ExternalAST, call spec.ExternalCallAST, lang spec.Lang, db spec.DBDriver) *spec.ResolvedObject {
	if call.Response == nil {
		return nil
	}
	obj := &spec.ResolvedObject{
		Name:    toPascalCase(call.Response.Name),
		Path:    fmt.Sprintf("generated/external/%s/types.go", ext.Name),
		Kind:    spec.ExternalOutput,
		Imports: make(map[spec.Lang]spec.ImportSet),
	}
	for _, f := range call.Response.Fields {
		obj.Fields = append(obj.Fields, buildField(f, lang, db))
	}
	return obj
}

// ─── buildTransactionParams ───────────────────────────────────────────────────

func buildTransactionParams(tx spec.TransactionAST, lang spec.Lang, db spec.DBDriver) spec.ResolvedObject {
	name := toPascalCase(tx.Name) + "Params"
	obj := spec.ResolvedObject{
		Name:    name,
		Path:    fmt.Sprintf("generated/tx/%s/types.go", toSnakeCase(tx.Name)),
		Kind:    spec.TransactionParams,
		Imports: make(map[spec.Lang]spec.ImportSet),
	}

	// Deduplicated union of all step params
	seen := make(map[string]bool)
	for _, step := range tx.Steps {
		for _, p := range step.Params {
			if seen[p.Name] {
				continue
			}
			seen[p.Name] = true
			td := MapType(p.Type, lang, db, false)
			obj.Fields = append(obj.Fields, spec.ResolvedField{
				Name:        toPascalCase(p.Name),
				DBColumn:    toSnakeCase(p.Name),
				Type:        td,
				GoStructTag: deriveGoStructTag(p.Name, false, false, nil),
			})
		}
	}

	return obj
}

// ─── buildField / buildDTOField ───────────────────────────────────────────────

func buildField(f spec.FieldAST, lang spec.Lang, db spec.DBDriver) spec.ResolvedField {
	td := MapType(f.Type, lang, db, f.Nullable)
	if f.Type == "enum" {
		td.IsEnum = true
		td.EnumValues = f.Values
	}

	var validateRules []string
	for _, r := range f.Rules {
		switch r.Type {
		case "min_length":
			validateRules = append(validateRules, fmt.Sprintf("min=%v", r.Value))
		case "max_length":
			validateRules = append(validateRules, fmt.Sprintf("max=%v", r.Value))
		case "email":
			validateRules = append(validateRules, "email")
		case "min":
			validateRules = append(validateRules, fmt.Sprintf("min=%v", r.Value))
		case "max":
			validateRules = append(validateRules, fmt.Sprintf("max=%v", r.Value))
		}
	}

	return spec.ResolvedField{
		Name:        toPascalCase(f.Name),
		DBColumn:    toSnakeCase(toPascalCase(f.Name)),
		Type:        td,
		Required:    f.Required,
		Unique:      f.Unique,
		Nullable:    f.Nullable,
		Private:     f.Private,
		Default:     f.Default,
		Values:      f.Values,
		GoStructTag: deriveGoStructTag(f.Name, f.Required, f.Unique, validateRules),
	}
}

func buildDTOField(f spec.DTOFieldAST, lang spec.Lang, db spec.DBDriver) spec.ResolvedField {
	td := MapType(f.Type, lang, db, false)
	return spec.ResolvedField{
		Name:        toPascalCase(f.Name),
		DBColumn:    toSnakeCase(toPascalCase(f.Name)),
		Type:        td,
		Required:    f.Required,
		Private:     f.Private,
		Compute:     f.Compute,
		GoStructTag: deriveGoStructTag(f.Name, f.Required, false, nil),
	}
}

// findExternalCallBodyName looks up the body type name for a given external + method.
func findExternalCallBodyName(externalName, method string, externals []spec.ExternalAST) string {
	for _, ext := range externals {
		if !strings.EqualFold(ext.Name, externalName) {
			continue
		}
		for _, call := range ext.Calls {
			if strings.EqualFold(call.Name, method) {
				if call.Body != nil {
					return toPascalCase(call.Body.Name)
				}
				return ""
			}
		}
	}
	return ""
}
