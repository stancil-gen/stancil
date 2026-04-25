package graph

import (
	"context"
	"fmt"
	"sync"
)

type StepFn func(ctx context.Context) error

type Graph struct {
	nodes    []*stepNode
	lastDeps []string
}

type stepNode struct {
	id       string
	deps     []string
	parallel []string
	when     func(ctx context.Context) bool
}

func NewGraph() *Graph {
	return &Graph{}
}

func (g *Graph) Parallel(ids ...string) *Graph {
	g.nodes = append(g.nodes, &stepNode{parallel: ids})
	g.lastDeps = nil
	return g
}

func (g *Graph) After(ids ...string) *Graph {
	g.lastDeps = ids
	return g
}

func (g *Graph) Then(id string) *Graph {
	node := &stepNode{id: id, deps: g.lastDeps}
	g.nodes = append(g.nodes, node)
	g.lastDeps = nil
	return g
}

func (g *Graph) When(fn func(ctx context.Context) bool) *Graph {
	if len(g.nodes) > 0 {
		g.nodes[len(g.nodes)-1].when = fn
	}
	return g
}

func (g *Graph) Respond() *Graph {
	return g
}

// Execute evaluates topological bounds sequentially across generated node limits safely
func (g *Graph) Execute(ctx context.Context, fns map[string]StepFn) error {
	for _, node := range g.nodes {
		if node.when != nil && !node.when(ctx) {
			continue 
		}

		if len(node.parallel) > 0 {
			var wg sync.WaitGroup
			errChan := make(chan error, len(node.parallel))
			
			for _, id := range node.parallel {
				fn, ok := fns[id]
				if !ok {
					return fmt.Errorf("step %s not found recursively inside mappers", id)
				}
				wg.Add(1)
				go func(stepID string, stepFn StepFn) {
					defer wg.Done()
					if err := stepFn(ctx); err != nil {
						errChan <- err
					}
				}(id, fn)
			}
			wg.Wait()
			close(errChan)
			for err := range errChan {
				if err != nil {
					return err
				}
			}
		} else if node.id != "" {
			fn, ok := fns[node.id]
			if !ok {
				return fmt.Errorf("step %s not found recursively inside mappers", node.id)
			}
			if err := fn(ctx); err != nil {
				return err
			}
		}
	}
	return nil
}
