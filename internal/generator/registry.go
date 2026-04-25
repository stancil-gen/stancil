package generator

import (
	"fmt"
	"sync"
)

// Registry acts as the thread-safe global catalog binding generic String IDs to concrete Execution Engines
type Registry struct {
	mu      sync.RWMutex
	plugins map[string]Generator
}

func NewRegistry() *Registry {
	return &Registry{
		plugins: make(map[string]Generator),
	}
}

func (r *Registry) Register(g Generator) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.plugins[g.ID()] = g
}

func (r *Registry) Get(id string) (Generator, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	
	if plugin, ok := r.plugins[id]; ok {
		return plugin, nil
	}
	
	return nil, fmt.Errorf("unregistered DAG blueprint target evaluated: %q - Are you missing an import plugin?", id)
}
