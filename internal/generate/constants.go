package generate

// StencilLibModule is the Go module path of the stancil-go runtime library.
// Generated code imports sub-packages directly from this module.
const StencilLibModule = "github.com/stancil-gen/stancil-go"

// LibImportPath returns the full import path for a stancil-go sub-package.
// Example: LibImportPath("container") returns "github.com/stancil-gen/stancil-go/container".
func LibImportPath(pkg string) string {
	return StencilLibModule + "/" + pkg
}
