package api

import (
	"fmt"

	"stencil/internal/emitter"
	"stencil/internal/generator"
	"stencil/internal/spec"
	"stencil/internal/template"
)

// ─── Handler template data ──────────────────────────────────────────────────

type HandlerData struct {
	Package     string
	Module      string
	ServiceType string
	ImplName    string
	Handlers    []HandlerEntry
}

type HandlerEntry struct {
	Name         string
	RequestType  string
	ResponseType string
	HTTPMethod   string
	HasBody      bool
	HasRequest   bool
	StatusCode   int
}

// ─── Generator ──────────────────────────────────────────────────────────────

type handlerGenerator struct {
	engine *template.Engine
}

func NewHandlerGenerator(e *template.Engine) generator.Generator {
	return &handlerGenerator{engine: e}
}

func (g *handlerGenerator) ID() string { return "go.api.handler" }

func (g *handlerGenerator) Generate(ctx generator.GeneratorContext) ([]emitter.File, error) {
	impl, ok := ctx.Payload.(*spec.ResolvedImplementation)
	if !ok || impl.Kind != spec.ServiceImpl {
		return nil, nil
	}

	pkg := derivePackageName(impl.Name)

	// Service interface type
	serviceType := ""
	if impl.Implements != nil {
		serviceType = impl.Implements.Name
	}

	var handlers []HandlerEntry
	for _, method := range impl.Methods {
		h := HandlerEntry{
			Name:         method.FunctionName,
			RequestType:  method.FunctionName + "Request",
			ResponseType: method.FunctionName + "Response",
			HTTPMethod:   method.HTTPMethod,
		}

		// HasBody: POST, PUT, PATCH have request bodies
		switch method.HTTPMethod {
		case "POST", "PUT", "PATCH":
			h.HasBody = true
		}

		// HasRequest: check if a request DTO actually exists
		if ctx.Spec.ObjectByName(h.RequestType) != nil {
			h.HasRequest = true
		}

		// Status code
		switch method.HTTPMethod {
		case "POST":
			h.StatusCode = 201
		case "DELETE":
			h.StatusCode = 204
		default:
			h.StatusCode = 200
		}

		handlers = append(handlers, h)
	}

	if len(handlers) == 0 {
		return nil, nil
	}

	data := HandlerData{
		Package:     pkg,
		Module:      ctx.Spec.Module,
		ServiceType: serviceType,
		ImplName:    impl.Name,
		Handlers:    handlers,
	}

	out, err := g.engine.Render("go/api/handler.go.tmpl", data)
	if err != nil {
		return nil, fmt.Errorf("handler generator: %w", err)
	}

	return []emitter.File{
		{Path: fmt.Sprintf("apis/%s/handler.go", pkg), Content: out},
	}, nil
}
