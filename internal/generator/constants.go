package generator

// StencilLibImportPrefix is the import path prefix for the stencil-go runtime library.
// Generated code imports from "{Module}/generated/lib/{package}".
//
// When stencil-go is published as a standalone module, change this to
// "github.com/stencil-run/stencil-go" and remove the lib embedding from the orchestrator.
const StencilLibImportPrefix = "lib"

// LibImportPath returns the full import path for a stencil-go sub-package
// relative to the generated module. Example: LibImportPath("orders-service", "container")
// returns "orders-service/generated/lib/container".
func LibImportPath(module, pkg string) string {
	return module + "/generated/" + StencilLibImportPrefix + "/" + pkg
}
