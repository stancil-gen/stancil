package external

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"stencil/internal/emitter"
	"stencil/internal/generator"
	"stencil/internal/generators/go/shared"
	"stencil/internal/spec"
	"stencil/internal/template"
)

// ─── Path-param regex ───────────────────────────────────────────────────────

var pathParamRe = regexp.MustCompile(`\{([^}]+)\}`)

// ─── Template data structs ──────────────────────────────────────────────────

// ExternalData is the top-level data passed to the template.
type ExternalData struct {
	Package       string
	Module        string
	Imports       []string
	InterfaceName string
	ImplName      string
	AuthKind      string // "bearer_token", "api_key", ""

	BaseURLField   string // cfg field name derived from base_url, e.g. "StripeUrl"
	AuthTokenField string // cfg field name derived from auth_token, e.g. "StripeSecretKey"
	TimeoutSeconds int    // default 30 if not specified

	// Retry configuration for the constructor
	RetryAttempts int
	RetryBackoff  string
	RetryOnStatus []int

	// Static headers from ExternalAST.Headers (value-based, set on every request)
	StaticHeaders []StaticHeader

	// Per-call types and methods
	BodyTypes   []IOStruct
	InputTypes  []IOStruct
	OutputTypes []IOStruct
	Methods     []ExternalMethod
	ErrorVars   []ErrorVar
}

// StaticHeader is a header with a fixed value set on every request.
type StaticHeader struct {
	Name  string // HTTP header name, e.g. "X-Request-Source"
	Value string // fixed value, e.g. "stencil"
}

// IOStruct renders as a Go struct declaration.
type IOStruct struct {
	Name   string
	Fields []IOField
}

// IOField is one field in an IOStruct.
type IOField struct {
	Name string
	Type string
	Tag  string
}

// ExternalMethod holds everything the template needs to render one client method.
type ExternalMethod struct {
	Name         string
	HTTPMethod   string
	Path         string // raw path template, e.g. "/v1/users/{user_id}"
	HasBody      bool
	BodyType     string // "{CallName}Body"
	HasResponse  bool
	ResponseType string // "{CallName}Response"
	InputType    string // "{CallName}Input"

	// Path params extracted from {placeholders} in the path
	PathParams []PathParam

	// Query params from ExternalCallAST.QueryParams
	QueryParams []QueryParam

	// Dynamic headers from ExternalCallAST.Headers where Type != ""
	DynamicHeaders []DynamicHeader

	// Status error mappings
	Errors []MethodError
}

// PathParam is one {placeholder} extracted from the URL path.
type PathParam struct {
	Placeholder string // original name in braces, e.g. "user_id"
	FieldName   string // PascalCase Go field name, e.g. "UserID"
}

// QueryParam is a query string parameter from the AST.
type QueryParam struct {
	ParamName string // original name, e.g. "page_size"
	FieldName string // PascalCase Go field name, e.g. "PageSize"
	GoType    string // Go type, e.g. "int", "string"
	IsString  bool   // true when GoType is "string" (no strconv needed)
}

// DynamicHeader is a header whose value comes from an input field.
type DynamicHeader struct {
	HeaderName string // HTTP header name, e.g. "Idempotency-Key"
	FieldName  string // PascalCase field on the Input struct, e.g. "IdempotencyKey"
}

// MethodError maps a status code to a sentinel error variable.
type MethodError struct {
	Status  int
	VarName string // e.g. "ErrCardDeclined"
}

// ErrorVar is a top-level sentinel error declaration.
type ErrorVar struct {
	VarName string
	Status  int
	Message string
}

// ─── Generator ──────────────────────────────────────────────────────────────

type externalGenerator struct {
	engine *template.Engine
}

func NewExternalGenerator(e *template.Engine) generator.Generator {
	return &externalGenerator{engine: e}
}

func (g *externalGenerator) ID() string { return "go.external" }

func (g *externalGenerator) Generate(ctx generator.GeneratorContext) ([]emitter.File, error) {
	impl, ok := ctx.Payload.(*spec.ResolvedImplementation)
	if !ok || impl == nil {
		return nil, nil
	}
	if impl.Kind != spec.ExternalImpl {
		return nil, nil
	}

	data := g.buildData(impl, ctx.Spec)

	content, err := g.engine.Render("go/external/external.go.tmpl", data)
	if err != nil {
		return nil, fmt.Errorf("external generator: %w", err)
	}

	fileName := toSnakeCase(strings.TrimSuffix(impl.Name, "Impl")) + ".go"
	return []emitter.File{{Path: "externals/" + fileName, Content: content}}, nil
}

func (g *externalGenerator) buildData(impl *spec.ResolvedImplementation, s *spec.ResolvedSpec) ExternalData {
	ifaceName := ""
	if impl.Implements != nil {
		ifaceName = impl.Implements.Name
	}

	// Derive the external name from the impl name (e.g. "StripeClientImpl" → "StripeClient")
	externalName := strings.TrimSuffix(impl.Name, "Impl")

	// Find the matching ExternalAST for query params, headers, etc.
	var extAST *spec.ExternalAST
	for i := range s.RawExternals {
		if toPascalCase(s.RawExternals[i].Name) == externalName {
			extAST = &s.RawExternals[i]
			break
		}
	}

	data := ExternalData{
		Package:        "externals",
		Module:         s.Module,
		InterfaceName:  ifaceName,
		ImplName:       impl.Name,
		TimeoutSeconds: 30, // default
	}

	// Resolve BaseURLField and AuthTokenField from the AST
	if extAST != nil {
		data.BaseURLField = resolveConfigRef(extAST.BaseURL)
		if extAST.AuthToken != "" {
			data.AuthTokenField = resolveConfigRef(extAST.AuthToken)
		}
		// Parse timeout string: "10s" → 10, "5s" → 5
		if extAST.Timeout != "" {
			secs := parseTimeoutSeconds(extAST.Timeout)
			if secs > 0 {
				data.TimeoutSeconds = secs
			}
		}
	}

	// Collect static headers from the ExternalAST (external-level headers with Value set)
	if extAST != nil {
		for _, h := range extAST.Headers {
			if h.Value != "" {
				data.StaticHeaders = append(data.StaticHeaders, StaticHeader{
					Name:  h.Name,
					Value: h.Value,
				})
			}
		}
	}

	// Track seen types and errors for deduplication
	seenIO := make(map[string]bool)
	seenErrors := make(map[string]bool)

	for mi, method := range impl.Methods {
		if len(method.Touches) == 0 {
			continue
		}
		touch := method.Touches[0]
		if touch.Kind != spec.TouchKindHTTPCall {
			continue
		}

		// Auth and retry (same for all methods, read from first touch)
		if data.AuthKind == "" {
			data.AuthKind = touch.AuthKind
		}
		if mi == 0 {
			data.RetryAttempts = touch.RetryAttempts
			data.RetryBackoff = touch.RetryBackoff
			data.RetryOnStatus = touch.RetryOnStatus
		}

		em := ExternalMethod{
			Name:       method.FunctionName,
			HTTPMethod: touch.HTTPMethod,
			Path:       touch.PathTemplate,
			InputType:  method.FunctionName + "Input",
		}

		// ── Path params from {placeholders} in the path template ──
		matches := pathParamRe.FindAllStringSubmatch(touch.PathTemplate, -1)
		for _, m := range matches {
			em.PathParams = append(em.PathParams, PathParam{
				Placeholder: m[1],
				FieldName:   toPascalCase(m[1]),
			})
		}

		// ── Query params from the AST ──
		if extAST != nil {
			callAST := findCallAST(extAST, method.FunctionName)
			if callAST != nil {
				for _, qp := range callAST.QueryParams {
					goType := mapSimpleType(qp.Type)
					em.QueryParams = append(em.QueryParams, QueryParam{
						ParamName: qp.Name,
						FieldName: toPascalCase(qp.Name),
						GoType:    goType,
						IsString:  goType == "string",
					})
				}
				// Dynamic headers (call-level headers where Type != "")
				for _, h := range callAST.Headers {
					if h.Type != "" {
						em.DynamicHeaders = append(em.DynamicHeaders, DynamicHeader{
							HeaderName: h.Name,
							FieldName:  toPascalCase(strings.ReplaceAll(h.Name, "-", "_")),
						})
					}
				}
			}
		}

		// ── Body type (only for POST/PUT/PATCH) ──
		if touch.RequestBodyRef != nil {
			bodyTypeName := method.FunctionName + "Body"
			em.HasBody = true
			em.BodyType = bodyTypeName
			if !seenIO[bodyTypeName] {
				seenIO[bodyTypeName] = true
				data.BodyTypes = append(data.BodyTypes, buildIOStruct(bodyTypeName, touch.RequestBodyRef, "json"))
			}
		}

		// ── Response type ──
		if touch.ResponseBodyRef != nil {
			respTypeName := method.FunctionName + "Response"
			em.HasResponse = true
			em.ResponseType = respTypeName
			if !seenIO[respTypeName] {
				seenIO[respTypeName] = true
				data.OutputTypes = append(data.OutputTypes, buildIOStruct(respTypeName, touch.ResponseBodyRef, "json"))
			}
		}

		// ── Build Input type ──
		// Input wraps Body (if present) + path params + query params + dynamic headers.
		// Non-body fields get `json:"-"`.
		inputFields := buildInputFields(em)
		data.InputTypes = append(data.InputTypes, IOStruct{
			Name:   em.InputType,
			Fields: inputFields,
		})

		// ── Status errors ──
		for _, se := range touch.StatusErrors {
			varName := "Err" + se.ErrorName
			em.Errors = append(em.Errors, MethodError{
				Status:  se.Status,
				VarName: varName,
			})
			if !seenErrors[varName] {
				seenErrors[varName] = true
				data.ErrorVars = append(data.ErrorVars, ErrorVar{
					VarName: varName,
					Status:  se.Status,
					Message: fmt.Sprintf("%s (%d)", toSnakeCase(se.ErrorName), se.Status),
				})
			}
		}

		data.Methods = append(data.Methods, em)
	}

	// ── Collect imports from all IO type fields ──
	var allTypes []string
	for _, t := range data.BodyTypes {
		for _, f := range t.Fields {
			allTypes = append(allTypes, f.Type)
		}
	}
	for _, t := range data.OutputTypes {
		for _, f := range t.Fields {
			allTypes = append(allTypes, f.Type)
		}
	}
	for _, t := range data.InputTypes {
		for _, f := range t.Fields {
			allTypes = append(allTypes, f.Type)
		}
	}
	data.Imports = shared.CollectImports(allTypes)

	return data
}

// ─── Helpers ────────────────────────────────────────────────────────────────

// resolveConfigRef converts a "${STRIPE_URL}" reference to a PascalCase config field name "StripeUrl".
func resolveConfigRef(ref string) string {
	inner := strings.TrimPrefix(strings.TrimSuffix(strings.TrimSpace(ref), "}"), "${")
	return configToPascalCase(inner)
}

// configToPascalCase converts UPPER_SNAKE_CASE to PascalCase with simple word casing (not acronym-aware).
// e.g. "STRIPE_SECRET_KEY" → "StripeSecretKey", "DATABASE_URL" → "DatabaseUrl"
func configToPascalCase(s string) string {
	parts := strings.FieldsFunc(s, func(r rune) bool {
		return r == '_' || r == '-'
	})
	var result strings.Builder
	for _, part := range parts {
		if len(part) == 0 {
			continue
		}
		runes := []rune(strings.ToLower(part))
		runes[0] = unicode.ToUpper(runes[0])
		result.WriteString(string(runes))
	}
	return result.String()
}

// parseTimeoutSeconds parses a timeout string like "10s" or "5s" into seconds.
func parseTimeoutSeconds(s string) int {
	s = strings.TrimSpace(s)
	if strings.HasSuffix(s, "s") {
		s = strings.TrimSuffix(s, "s")
		var n int
		for _, r := range s {
			if r >= '0' && r <= '9' {
				n = n*10 + int(r-'0')
			}
		}
		return n
	}
	return 0
}

// buildInputFields creates the fields for a {CallName}Input struct.
func buildInputFields(em ExternalMethod) []IOField {
	var fields []IOField

	// Body field (embed by pointer)
	if em.HasBody {
		fields = append(fields, IOField{
			Name: "Body",
			Type: em.BodyType,
			Tag:  "`json:\"body\"`",
		})
	}

	// Path params
	for _, pp := range em.PathParams {
		fields = append(fields, IOField{
			Name: pp.FieldName,
			Type: "string",
			Tag:  "`json:\"-\"`",
		})
	}

	// Query params
	for _, qp := range em.QueryParams {
		fields = append(fields, IOField{
			Name: qp.FieldName,
			Type: qp.GoType,
			Tag:  "`json:\"-\"`",
		})
	}

	// Dynamic headers
	for _, dh := range em.DynamicHeaders {
		fields = append(fields, IOField{
			Name: dh.FieldName,
			Type: "string",
			Tag:  "`json:\"-\"`",
		})
	}

	return fields
}

// findCallAST finds the ExternalCallAST matching a function name.
func findCallAST(ext *spec.ExternalAST, funcName string) *spec.ExternalCallAST {
	for i := range ext.Calls {
		if toPascalCase(ext.Calls[i].Name) == funcName {
			return &ext.Calls[i]
		}
	}
	return nil
}

// mapSimpleType converts a YAML type name to a Go type for query params.
func mapSimpleType(yamlType string) string {
	switch yamlType {
	case "int":
		return "int"
	case "bool":
		return "bool"
	case "decimal":
		return "float64"
	default:
		return "string"
	}
}

var dbTagRe = regexp.MustCompile(` ?db:"[^"]*"`)

func stripDBTag(tag string) string {
	return dbTagRe.ReplaceAllString(tag, "")
}

// buildIOStruct creates an IOStruct from a ResolvedObject, using the given
// struct name (which may differ from the object's original name).
func buildIOStruct(name string, obj *spec.ResolvedObject, tagPrefix string) IOStruct {
	s := IOStruct{Name: name}
	for _, f := range obj.Fields {
		tag := stripDBTag(f.GoStructTag)
		if tagPrefix == "json" && tag == "" {
			tag = fmt.Sprintf("`json:\"%s\"`", toSnakeCase(f.Name))
		}
		s.Fields = append(s.Fields, IOField{
			Name: f.Name,
			Type: f.Type.GoType,
			Tag:  tag,
		})
	}
	return s
}

// ─── Case conversion helpers ────────────────────────────────────────────────

func toPascalCase(s string) string {
	parts := strings.FieldsFunc(s, func(r rune) bool {
		return r == '_' || r == '-' || r == ' '
	})
	var b strings.Builder
	for _, part := range parts {
		if len(part) == 0 {
			continue
		}
		upper := strings.ToUpper(part)
		switch upper {
		case "ID", "URL", "HTTP", "API", "SQL", "UUID", "JSON", "HTML", "CSS", "JWT", "DLQ", "FK", "PK", "DB":
			b.WriteString(upper)
		default:
			runes := []rune(part)
			runes[0] = unicode.ToUpper(runes[0])
			b.WriteString(string(runes))
		}
	}
	return b.String()
}

func toSnakeCase(s string) string {
	var b strings.Builder
	runes := []rune(s)
	for i, r := range runes {
		if unicode.IsUpper(r) {
			if i > 0 && !unicode.IsUpper(runes[i-1]) {
				b.WriteByte('_')
			} else if i > 0 && i+1 < len(runes) && unicode.IsUpper(runes[i-1]) && !unicode.IsUpper(runes[i+1]) {
				b.WriteByte('_')
			}
			b.WriteRune(unicode.ToLower(r))
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}
