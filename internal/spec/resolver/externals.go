package resolver

import (
	"fmt"
	"regexp"
	"strings"

	"stencil/internal/spec"
)

// ExternalsFeature resolves the `externals:` block.
// Owns all three levels for each external client:
//   - Level 1: request/response body objects (ExternalInput, ExternalOutput)
//   - Level 2: external client interface
//   - Level 3: external client implementation
type ExternalsFeature struct{}

func (f *ExternalsFeature) Name() string { return "externals" }

func (f *ExternalsFeature) Validate(ast *spec.SpecAST) []error { return nil }

func (f *ExternalsFeature) Resolve(ast *spec.SpecAST, ir *spec.ResolvedSpec) error {
	// Collect all objects for cross-referencing in buildExternalImpl
	var allObjects []*spec.ResolvedObject
	for i := range ir.Objects {
		allObjects = append(allObjects, &ir.Objects[i])
	}

	for _, ext := range ast.Externals {
		// Level 1: body + response objects for each call
		for _, call := range ext.Calls {
			if inp := buildExternalInput(ext, call); inp != nil {
				ir.Objects = append(ir.Objects, *inp)
				allObjects = append(allObjects, &ir.Objects[len(ir.Objects)-1])
			}
			if out := buildExternalOutput(ext, call); out != nil {
				ir.Objects = append(ir.Objects, *out)
				allObjects = append(allObjects, &ir.Objects[len(ir.Objects)-1])
			}
		}

		// Level 2: interface
		iface := buildExternalInterface(ext, allObjects)
		ir.Interfaces = append(ir.Interfaces, iface)
		ifacePtr := &ir.Interfaces[len(ir.Interfaces)-1]

		// Level 3: implementation + mock
		impl := buildExternalImpl(ext, ifacePtr, allObjects)
		ir.Implementations = append(ir.Implementations, impl)

		mock := buildExternalMock(ext, ifacePtr)
		ir.Implementations = append(ir.Implementations, mock)
	}
	return nil
}

// ─── Path param regex ─────────────────────────────────────────────────────────

var pathParamRe = regexp.MustCompile(`\{([^}]+)\}`)

// ─── Level 1: buildExternalInput / buildExternalOutput ───────────────────────

func buildExternalInput(ext spec.ExternalAST, call spec.ExternalCallAST) *spec.ResolvedObject {
	if call.Body == nil {
		return nil
	}
	obj := &spec.ResolvedObject{
		Name: toPascalCase(call.Body.Name),
		Path: fmt.Sprintf("generated/external/%s/types.go", toSnakeCase(ext.Name)),
		Kind: spec.ExternalInput,
	}
	for _, f := range call.Body.Fields {
		obj.Fields = append(obj.Fields, buildField(f))
	}
	return obj
}

func buildExternalOutput(ext spec.ExternalAST, call spec.ExternalCallAST) *spec.ResolvedObject {
	if call.Response == nil {
		return nil
	}
	// Use the spec-declared response name, not the call name.
	// The external generator may rename for its output files, but the IR uses spec names.
	obj := &spec.ResolvedObject{
		Name: toPascalCase(call.Response.Name),
		Path: fmt.Sprintf("generated/external/%s/types.go", toSnakeCase(ext.Name)),
		Kind: spec.ExternalOutput,
	}
	for _, f := range call.Response.Fields {
		obj.Fields = append(obj.Fields, buildField(f))
	}
	return obj
}

// ─── Level 2: buildExternalInterface ─────────────────────────────────────────

func buildExternalInterface(ext spec.ExternalAST, objects []*spec.ResolvedObject) spec.ResolvedInterface {
	ri := spec.ResolvedInterface{
		Name: toPascalCase(ext.Name),
		Path: fmt.Sprintf("generated/external/%s/client.go", toSnakeCase(ext.Name)),
		Kind: spec.ExternalInterface,
	}

	for _, call := range ext.Calls {
		// Input param: the body type (if any)
		var params []spec.ResolvedParam
		if call.Body != nil {
			bodyName := toPascalCase(call.Body.Name)
			bodyObj := findObjectByName(objects, bodyName)
			params = append(params, spec.ResolvedParam{
				Name:    "req",
				Type:    spec.TypeDescriptor{Kind: spec.TypeCustom, CustomName: bodyName, IsCustom: true},
				TypeRef: bodyObj,
			})
		}

		// Return: the response type (if any)
		var returns []spec.ResolvedReturn
		if call.Response != nil {
			// Use the spec-declared response name (same as buildExternalOutput)
			respName := toPascalCase(call.Response.Name)
			respObj := findObjectByName(objects, respName)
			returns = []spec.ResolvedReturn{
				{
					Type:    spec.TypeDescriptor{Kind: spec.TypeCustom, CustomName: respName, IsCustom: true, Nullable: true},
					TypeRef: respObj,
				},
			}
		}

		ri.Functions = append(ri.Functions, spec.ResolvedFunction{
			Name:    toPascalCase(call.Name),
			Params:  params,
			Returns: returns,
		})
	}

	return ri
}

// ─── Level 3: buildExternalImpl ──────────────────────────────────────────────

func buildExternalImpl(ext spec.ExternalAST, extIface *spec.ResolvedInterface, objects []*spec.ResolvedObject) spec.ResolvedImplementation {
	impl := spec.ResolvedImplementation{
		Name:       toPascalCase(ext.Name) + "Impl",
		Path:       fmt.Sprintf("generated/external/%s/client_impl.go", toSnakeCase(ext.Name)),
		Kind:       spec.ExternalImpl,
		Implements: extIface,
	}

	// Dependencies
	impl.Dependencies = []spec.ResolvedDependency{
		{FieldName: "client", TypeName: "*http.Client", Import: "net/http"},
		{FieldName: "baseURL", TypeName: "string"},
	}
	if ext.Auth == "bearer_token" {
		impl.Dependencies = append(impl.Dependencies, spec.ResolvedDependency{FieldName: "token", TypeName: "string"})
	} else if ext.Auth == "api_key" {
		impl.Dependencies = append(impl.Dependencies, spec.ResolvedDependency{FieldName: "apiKey", TypeName: "string"})
	}

	// Retry config
	retryAttempts := 0
	retryBackoff := ""
	var retryOnStatus []int
	if ext.Retry != nil {
		retryAttempts = ext.Retry.Attempts
		retryBackoff = ext.Retry.Backoff
		retryOnStatus = ext.Retry.OnStatus
	}

	// Resolve config field refs
	baseURLField := resolveConfigRef(ext.BaseURL)
	authConfigField := ""
	if ext.AuthToken != "" {
		authConfigField = resolveConfigRef(ext.AuthToken)
	}

	// Service-level static headers
	serviceStaticHeaders := map[string]string{}
	for _, h := range ext.Headers {
		if h.Value != "" {
			serviceStaticHeaders[h.Name] = h.Value
		}
	}

	for _, call := range ext.Calls {
		// Find request/response body objects
		var reqBodyRef, respBodyRef *spec.ResolvedObject
		if call.Body != nil {
			reqBodyRef = findObjectByName(objects, toPascalCase(call.Body.Name))
		}
		if call.Response != nil {
			respBodyRef = findObjectByName(objects, toPascalCase(call.Response.Name))
		}

		// Extract path params from PathTemplate
		var pathParams []string
		for _, m := range pathParamRe.FindAllStringSubmatch(call.Path, -1) {
			pathParams = append(pathParams, m[1])
		}

		// Query params — resolved as ResolvedFields (language-agnostic)
		var queryParamFields []spec.ResolvedField
		for _, qp := range call.QueryParams {
			queryParamFields = append(queryParamFields, buildField(qp))
		}

		// Call-level headers: split into dynamic (per-request) and static (fixed)
		// Merge service-level static headers first
		callStaticHeaders := map[string]string{}
		for k, v := range serviceStaticHeaders {
			callStaticHeaders[k] = v
		}
		var dynamicHeaders []spec.ResolvedField
		for _, h := range call.Headers {
			if h.Type != "" {
				// Dynamic header — value provided per call
				dynamicHeaders = append(dynamicHeaders, spec.ResolvedField{
					Name:     toPascalCase(strings.ReplaceAll(h.Name, "-", "_")),
					DBColumn: strings.ToLower(h.Name),
					Type:     MapType("str", false),
					Required: h.Required,
				})
			} else if h.Value != "" {
				// Static header — fixed value
				callStaticHeaders[h.Name] = h.Value
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
			Kind:             spec.TouchKindHTTPCall,
			HTTPMethod:       call.Method,
			PathTemplate:     call.Path,
			PathParams:       pathParams,
			QueryParamFields: queryParamFields,
			DynamicHeaders:   dynamicHeaders,
			StaticHeaders:    callStaticHeaders,
			RequestBodyRef:   reqBodyRef,
			ResponseBodyRef:  respBodyRef,
			StatusErrors:     statusErrors,
			RetryAttempts:    retryAttempts,
			RetryBackoff:     retryBackoff,
			RetryOnStatus:    retryOnStatus,
			AuthKind:         ext.Auth,
			AuthConfigField:  authConfigField,
			BaseURLField:     baseURLField,
			Timeout:          parseDuration(ext.Timeout),
		}

		impl.Methods = append(impl.Methods, spec.ResolvedMethod{
			FunctionName: toPascalCase(call.Name),
			Touches:      []spec.ResolvedTouch{touch},
		})
	}

	return impl
}

// buildExternalMock builds the mock implementation for an external interface.
// No methods or dependencies — the generator derives everything from the interface's Functions list.
func buildExternalMock(ext spec.ExternalAST, extIface *spec.ResolvedInterface) spec.ResolvedImplementation {
	return spec.ResolvedImplementation{
		Name:       toPascalCase(ext.Name) + "Mock",
		Path:       fmt.Sprintf("generated/external/%s/client_mock.go", toSnakeCase(ext.Name)),
		Kind:       spec.ExternalMockImpl,
		Implements: extIface,
	}
}

// resolveConfigRef strips the ${...} wrapper and converts to PascalCase config field name.
// "${STRIPE_URL}" → "StripeUrl", "${STRIPE_SECRET_KEY}" → "StripeSecretKey"
func resolveConfigRef(ref string) string {
	if !strings.HasPrefix(ref, "${") {
		return toPascalCase(ref)
	}
	inner := strings.TrimPrefix(strings.TrimSuffix(ref, "}"), "${")
	// Convert UPPER_SNAKE_CASE to PascalCase
	parts := strings.Split(strings.ToLower(inner), "_")
	var sb strings.Builder
	for _, p := range parts {
		if len(p) > 0 {
			sb.WriteString(strings.ToUpper(p[:1]) + p[1:])
		}
	}
	return sb.String()
}
