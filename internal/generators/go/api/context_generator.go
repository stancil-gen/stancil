// Phase 14 migration target — API generator stubs.
package api

import (
	"stencil/internal/emitter"
	"stencil/internal/generator"
	"stencil/internal/template"
)

type stubGenerator struct{ id string }

func (s *stubGenerator) ID() string { return s.id }
func (s *stubGenerator) Generate(_ generator.GeneratorContext) ([]emitter.File, error) {
	return nil, nil
}

func NewDTOGenerator(_ *template.Engine) generator.Generator     { return &stubGenerator{"go.api.dto"} }
func NewContextGenerator(_ *template.Engine) generator.Generator { return &stubGenerator{"go.api.context"} }
func NewHooksGenerator(_ *template.Engine) generator.Generator   { return &stubGenerator{"go.api.hooks"} }
func NewServiceGenerator(_ *template.Engine) generator.Generator { return &stubGenerator{"go.api.service"} }
func NewHandlerGenerator(_ *template.Engine) generator.Generator { return &stubGenerator{"go.api.handler"} }
