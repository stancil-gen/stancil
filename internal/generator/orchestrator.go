package generator

import (
	"bytes"
	"context"
	"fmt"

	"io/fs"

	"golang.org/x/sync/errgroup"
	"stencil/internal/emitter"
	"stencil/internal/plan"
	"stencil/internal/spec"
	lib "github.com/stencil-run/stencil-go"
)

// Orchestrator translates Kahn's output Tiers into physical Template executions
type Orchestrator struct {
	registry *Registry
	emitter  *emitter.Emitter
}

func NewOrchestrator(r *Registry, e *emitter.Emitter) *Orchestrator {
	return &Orchestrator{
		registry: r,
		emitter:  e,
	}
}

// Run executes Topographical Tier batches sequentially, but executes the target Generation Nodes strictly concurrently!
func (o *Orchestrator) Run(specContext *spec.ResolvedSpec, dagPlan *plan.Plan) error {
	
	// Preload native runtime library — rewrite inter-package imports to use the project's module path.
	libModulePath := "github.com/stencil-run/stencil-go"
	targetLibPath := specContext.Module + "/generated/lib"

	err := fs.WalkDir(lib.FS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		if path == "embed.go" || path == "." || path == "go.mod" || path == "go.sum" {
			return nil
		}

		content, err := fs.ReadFile(lib.FS, path)
		if err != nil {
			return err
		}

		// Rewrite imports: github.com/stencil-run/stencil-go/errors → {module}/generated/lib/errors
		content = bytes.ReplaceAll(content, []byte(libModulePath), []byte(targetLibPath))

		o.emitter.Stage(emitter.File{Path: "lib/" + path, Content: content})
		return nil
	})
	
	if err != nil {
		return fmt.Errorf("CRITICAL: Failed dynamically packaging internal Library logic natively! %v", err)
	}

	for tierIndex, tier := range dagPlan.Tiers {
		// Encapsulate parallel runs within an explicit bounding ErrGroup
		eg, _ := errgroup.WithContext(context.Background())

		// A bounded channel to safely pass back memory arrays across parallel boundaries
		fileChan := make(chan []emitter.File, len(tier))

		for _, node := range tier {
			// Thread-safe capture context 
			targetNode := node

			eg.Go(func() error {
				plugin, err := o.registry.Get(targetNode.Generator)
				if err != nil {
					return err
				}

				ctx := GeneratorContext{
					Spec:    specContext,
					Payload: targetNode.Payload,
				}

				files, genErr := plugin.Generate(ctx)
				if genErr != nil {
					return fmt.Errorf("generator blueprint plugin '%s' critically panicked simulating node execution '%s': %w", plugin.ID(), targetNode.ID, genErr)
				}

				fileChan <- files
				return nil
			})
		}

		// Halt sequentially until parallel GoRoutines finish and evaluate mathematical errors
		if err := eg.Wait(); err != nil {
			return fmt.Errorf("tier %d parallel batch anomaly detected: %w", tierIndex+1, err)
		}

		close(fileChan)

		// Safely funnel successful concurrent outputs sequentially into the Emitter buffer!
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

	// Trigger atomic File IO dump safely only when ALL tiers finish mathematically
	return o.emitter.Flush()
}
