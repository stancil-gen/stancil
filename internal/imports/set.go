package imports

import "sort"

// ImportSet deduplicates Go package requirements required for generation templates globally
type ImportSet struct {
	module string
	paths  map[string]struct{}
}

func NewImportSet(module string) *ImportSet {
	return &ImportSet{
		module: module,
		paths:  make(map[string]struct{}),
	}
}

// Add safely traps a target generation package target ensuring deduplication 
func (s *ImportSet) Add(path string) {
	s.paths[path] = struct{}{}
}

// List resolves the imports alphanumerically
func (s *ImportSet) List() []string {
	var result []string
	for path := range s.paths {
		result = append(result, path)
	}
	sort.Strings(result)
	return result
}
