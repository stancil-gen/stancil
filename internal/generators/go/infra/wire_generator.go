// Phase 14 migration target — infra generator stubs.
package infra

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

func NewWireGenerator(_ *template.Engine) generator.Generator { return &stubGenerator{"go.wire"} }
