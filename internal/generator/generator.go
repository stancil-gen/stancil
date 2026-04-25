package generator

import (
	"stencil/internal/emitter"
	"stencil/internal/spec"
)

type GeneratorContext struct {
	Spec    *spec.ResolvedSpec // Contextual root required by things like go.wire
	Payload interface{}        // The deeply abstracted model from the DAG Topographical map 
}

// Generator provides the underlying generic contract enforcing universal interface implementations 
// regardless of target execution bounds (Python/Go/Postgres...)
type Generator interface {
	ID() string
	Generate(ctx GeneratorContext) ([]emitter.File, error)
}
