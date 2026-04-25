package imports

import (
	"stencil/internal/spec"
)

// APIServiceImports is a STUB — will be updated in Phase 14 when generators are migrated.
// The new resolver attaches ImportSets directly to every ResolvedObject/Interface/Implementation.
// Generators should read obj.Imports[spec.LangGo].Paths instead of calling this function.
func APIServiceImports(impl *spec.ResolvedImplementation, mod string) *ImportSet {
	s := NewImportSet(mod)
	if impl == nil {
		return s
	}
	for _, imp := range impl.Imports[spec.LangGo].Paths {
		s.Add(imp)
	}
	return s
}
