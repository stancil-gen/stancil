package api

import (
	"fmt"

	"stencil/internal/emit"
	generator "stencil/internal/generate"
	"stencil/internal/spec"
	"stencil/internal/template"
)

// ─── Hooks template data ────────────────────────────────────────────────────

type HooksData struct {
	Package string
	Module  string
	Hooks   []HookStruct
}

type HookStruct struct {
	Name        string
	ContextType string
	Functions   []HookFunc
}

type HookFunc struct {
	Name string
}

// ─── Generator ──────────────────────────────────────────────────────────────

type hooksGenerator struct {
	engine *template.Engine
}

func NewHooksGenerator(e *template.Engine) generator.Generator {
	return &hooksGenerator{engine: e}
}

func (g *hooksGenerator) ID() string { return "go.api.hooks" }

func (g *hooksGenerator) Generate(ctx generator.GeneratorContext) ([]emitter.File, error) {
	impl, ok := ctx.Payload.(*spec.ResolvedImplementation)
	if !ok || impl.Kind != spec.ServiceImpl {
		return nil, nil
	}

	pkg := derivePackageName(impl.Name)

	seen := map[string]bool{}
	var hooks []HookStruct

	for _, method := range impl.Methods {
		hookIface := ctx.Spec.InterfaceByName(method.FunctionName + "Hooks")
		if hookIface == nil || seen[hookIface.Name] {
			continue
		}
		seen[hookIface.Name] = true

		// Determine the context type for this hook
		contextType := method.FunctionName + "Context"
		if method.SharedContext != nil {
			contextType = method.SharedContext.Name
		}

		hs := HookStruct{
			Name:        hookIface.Name,
			ContextType: contextType,
		}
		for _, fn := range hookIface.Functions {
			hs.Functions = append(hs.Functions, HookFunc{Name: fn.Name})
		}
		hooks = append(hooks, hs)
	}

	if len(hooks) == 0 {
		return nil, nil
	}

	data := HooksData{
		Package: pkg,
		Module:  ctx.Spec.Module,
		Hooks:   hooks,
	}

	out, err := g.engine.Render("go/api/hooks.go.tmpl", data)
	if err != nil {
		return nil, fmt.Errorf("hooks generator: %w", err)
	}

	return []emitter.File{
		{Path: fmt.Sprintf("apis/%s/hooks.go", pkg), Content: out},
	}, nil
}
