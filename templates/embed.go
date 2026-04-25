package templates

import "embed"

// FS exposes the absolute template directory natively packed inside the Go CLI binary via compilation!
//
//go:embed go/*
var FS embed.FS
