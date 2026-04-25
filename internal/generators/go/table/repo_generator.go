// Phase 14 migration target — generator stubs to ensure compilation while resolver is being built.
// These will be re-implemented in Phase 14 to read the new ResolvedSpec.
package table

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

func NewModelGenerator(_ *template.Engine) generator.Generator {
	return &stubGenerator{id: "go.table.model"}
}

func NewRepoGenerator(_ *template.Engine) generator.Generator {
	return &stubGenerator{id: "go.table.repo"}
}
