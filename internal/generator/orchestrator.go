package generator

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"

	"golang.org/x/sync/errgroup"
	"stencil/internal/emitter"
	"stencil/internal/lang"
	golang "stencil/internal/lang/go"
	"stencil/internal/plan"
	"stencil/internal/spec"
	lib "github.com/stencil-run/stencil-go"
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
	// Preload native runtime library — rewrite inter-package imports to the project's module path.
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
		content = bytes.ReplaceAll(content, []byte(libModulePath), []byte(targetLibPath))
		o.emitter.Stage(emitter.File{Path: "lib/" + path, Content: content})
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed packaging runtime library: %v", err)
	}

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
