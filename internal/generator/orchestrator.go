package generator

import (
	"context"
	"fmt"

	"golang.org/x/sync/errgroup"
	"stencil/internal/emitter"
	"stencil/internal/lang"
	golang "stencil/internal/lang/go"
	"stencil/internal/plan"
	"stencil/internal/spec"
)

// Orchestrator translates Kahn's output Tiers into physical Template executions.
type Orchestrator struct {
	registry *Registry
	emitter  *emitter.Emitter
}

func NewOrchestrator(r *Registry, e *emitter.Emitter) *Orchestrator {
	return &Orchestrator{registry: r, emitter: e}
}

// Run executes topographical tier batches sequentially, with nodes within each tier running concurrently.
func (o *Orchestrator) Run(specContext *spec.ResolvedSpec, dagPlan *plan.Plan) error {
	// Instantiate the language pack based on the spec's target language.
	// All generators within this run share the same LangPack instance.
	lp, err := newLangPack(specContext.Lang)
	if err != nil {
		return err
	}

	for tierIndex, tier := range dagPlan.Tiers {
		eg, _ := errgroup.WithContext(context.Background())
		fileChan := make(chan []emitter.File, len(tier))

		for _, node := range tier {
			targetNode := node
			eg.Go(func() error {
				plugin, err := o.registry.Get(targetNode.Generator)
				if err != nil {
					return err
				}

				ctx := GeneratorContext{
					Spec:    specContext,
					Lang:    lp,
					Payload: targetNode.Payload,
				}

				files, genErr := plugin.Generate(ctx)
				if genErr != nil {
					return fmt.Errorf("generator '%s' failed for node '%s': %w", plugin.ID(), targetNode.ID, genErr)
				}

				fileChan <- files
				return nil
			})
		}

		if err := eg.Wait(); err != nil {
			return fmt.Errorf("tier %d error: %w", tierIndex+1, err)
		}

		close(fileChan)
		for files := range fileChan {
			for _, f := range files {
				if f.Scaffold {
					o.emitter.StageScaffold(f)
				} else {
					o.emitter.Stage(f)
				}
			}
		}
	}

	return o.emitter.Flush()
}

// newLangPack returns the LangPack for the target language.
// To add a new language, add a case here and implement lang.LangPack.
func newLangPack(l spec.Lang) (lang.LangPack, error) {
	switch l {
	case spec.LangGo, "": // default to Go when not specified
		return golang.NewGoLangPack(), nil
	default:
		return nil, fmt.Errorf("unsupported target language: %q — only %q is supported currently", l, spec.LangGo)
	}
}
