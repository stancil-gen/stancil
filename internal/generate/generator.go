package generate

import (
	"stencil/internal/emit"
	"stencil/internal/lang"
	"stencil/internal/spec"
)

// GeneratorContext is passed to every Generator.Generate() call.
// It carries the full IR (Spec), the language rendering pack (Lang),
// and the generator-specific payload from the DAG planner.
type GeneratorContext struct {
	Spec    *spec.ResolvedSpec // full IR — all generators can read the whole spec
	Lang    lang.LangPack      // language-specific rendering (type names, field tags, etc.)
	Payload interface{}        // generator-specific data from the DAG node
}

// Generator is the universal plugin contract.
// Each generator produces one or more output files from its assigned DAG node.
type Generator interface {
	ID() string
	Generate(ctx GeneratorContext) ([]emitter.File, error)
}
