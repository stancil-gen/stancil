package lib

import "embed"

// FS structurally embeds the runtime components inside Stencil compilations seamlessly!
//
//go:embed container errors handler graph pagination httputil
var FS embed.FS
