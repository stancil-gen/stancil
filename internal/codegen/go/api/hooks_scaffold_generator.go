package api

import (
	"fmt"
	"strings"

	"stencil/internal/emit"
	generator "stencil/internal/generate"
	"stencil/internal/spec"
	"stencil/internal/template"
)

// ─── Hook scaffold template data ─────────────────────────────────────────────

type HookScaffoldData struct {
	Package    string // e.g. "hooks"
	Module     string
	APIPkg     string // e.g. "user_ap_is"
	APIImport  string // e.g. "full-ecommerce/generated/apis/user_ap_is"
	APIName    string // e.g. "CreateUser"
	ImplName   string // e.g. "CreateUserHookImpl"
	HooksType  string // e.g. "CreateUserHooks"
	CtxType    string // e.g. "CreateUserContext"
	Methods    []HookScaffoldMethod
}

type HookScaffoldMethod struct {
	HookName    string // e.g. "BeforeTableUsersCreate"
	Comment     string // what this hook is for
	Body        string // pre-filled implementation body
}

// ─── Generator ───────────────────────────────────────────────────────────────

type hookScaffoldGenerator struct {
	engine *template.Engine
}

func NewHookScaffoldGenerator(e *template.Engine) generator.Generator {
	return &hookScaffoldGenerator{engine: e}
}

func (g *hookScaffoldGenerator) ID() string { return "go.api.hooks.scaffold" }

func (g *hookScaffoldGenerator) Generate(ctx generator.GeneratorContext) ([]emitter.File, error) {
	impl, ok := ctx.Payload.(*spec.ResolvedImplementation)
	if !ok || impl.Kind != spec.ServiceImpl {
		return nil, nil
	}

	pkg := derivePackageName(impl.Name)
	module := ctx.Spec.Module

	var files []emitter.File

	for _, method := range impl.Methods {
		apiName := method.FunctionName
		ctxType := apiName + "Context"
		if method.SharedContext != nil {
			ctxType = method.SharedContext.Name
		}

		hooksType := apiName + "Hooks"
		implName := apiName + "HookImpl"

		data := HookScaffoldData{
			Package:   "hooks",
			Module:    module,
			APIPkg:    pkg,
			APIImport: module + "/generated/apis/" + pkg,
			APIName:   apiName,
			ImplName:  implName,
			HooksType: hooksType,
			CtxType:   ctxType,
		}

		// Build the BeforeResponse hook body by auto-mapping response fields from context
		respTypeName := apiName + "Response"
		respObj := ctx.Spec.ObjectByName(respTypeName)
		if respObj != nil {
			data.Methods = append(data.Methods, HookScaffoldMethod{
				HookName: "BeforeResponse",
				Comment:  "maps step outputs to the " + respTypeName + " DTO",
				Body:     buildBeforeResponseBody(respObj, method, ctx.Spec, pkg),
			})
		}

		// Add AfterTableXCreate hook if there's a create step — common pattern for setting defaults
		for _, touch := range method.Touches {
			if touch.Kind == spec.TouchKindTable && touch.Op == "create" {
				data.Methods = append(data.Methods, HookScaffoldMethod{
					HookName: "Before" + hookSuffix("table", touch.TableRef.TableName, touch.Op),
					Comment:  "runs before writing to " + touch.TableRef.TableName + " — set any fields the mapper can't infer",
					Body:     "\t// shared." + toPascalCase(touch.StepID) + "Input is populated by the mapper.\n\t// Modify fields here before the DB write.",
				})
				break
			}
		}

		out, err := g.engine.Render("go/api/hooks_scaffold.go.tmpl", data)
		if err != nil {
			return nil, fmt.Errorf("hook scaffold generator %s: %w", apiName, err)
		}

		fileName := fmt.Sprintf("hooks/%s_hooks.go", toSnakeCase(apiName))
		files = append(files, emitter.File{
			Path:    fileName,
			Content: out,
			Scaffold: true, // written once, never overwritten
		})
	}

	return files, nil
}

// buildBeforeResponseBody auto-maps response DTO fields from the shared context.
func buildBeforeResponseBody(respObj *spec.ResolvedObject, method spec.ResolvedMethod, s *spec.ResolvedSpec, pkg string) string {
	var lines []string
	lines = append(lines, "\tshared.Response = &"+pkg+"."+respObj.Name+"{")

	for _, f := range respObj.Fields {
		source := findFieldSource(f.Name, f.Type.Kind, method, s)
		if source != "" {
			lines = append(lines, fmt.Sprintf("\t\t%s: %s,", f.Name, source))
		} else {
			lines = append(lines, fmt.Sprintf("\t\t// %s: ???, // TODO: map this field", f.Name))
		}
	}

	lines = append(lines, "\t}")
	return strings.Join(lines, "\n")
}

// findFieldSource searches step outputs and the request for a matching field by name+kind.
func findFieldSource(fieldName string, kind spec.TypeKind, method spec.ResolvedMethod, s *spec.ResolvedSpec) string {
	// Check each step output in order
	for _, touch := range method.Touches {
		if touch.TableRef == nil {
			continue
		}
		outputField := toPascalCase(touch.StepID) + "Output"
		model := s.ObjectByName(touch.TableRef.Name)
		if model == nil {
			continue
		}
		for _, mf := range model.Fields {
			if mf.Name == fieldName && mf.Type.Kind == kind {
				return "shared." + outputField + "." + fieldName
			}
		}
	}
	// Check request
	reqName := method.FunctionName + "Request"
	reqObj := s.ObjectByName(reqName)
	if reqObj != nil {
		for _, rf := range reqObj.Fields {
			if rf.Name == fieldName && rf.Type.Kind == kind {
				return "shared.Request." + fieldName
			}
		}
	}
	return ""
}
