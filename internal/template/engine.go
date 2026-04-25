package template

import (
	"bytes"
	"fmt"
	"path/filepath"
	"text/template"

	"stencil/templates"
)

// Engine safely binds exact physical text evaluations against dynamic memory datasets isolating panics.
type Engine struct {
	fm template.FuncMap
}

func NewEngine() *Engine {
	return &Engine{
		fm: template.FuncMap{},
	}
}

// Render enforces absolute structural compilation. It pulls target text from the embed package
// entirely bypassing local OS disk fetches!
func (e *Engine) Render(path string, data interface{}) ([]byte, error) {
	content, err := templates.FS.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("native template %s missing from binary embed payload: %v", path, err)
	}

	tmpl, err := template.New(filepath.Base(path)).Funcs(e.fm).Parse(string(content))
	if err != nil {
		return nil, fmt.Errorf("malformed logical boundaries inside text map '%s': %v", path, err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("generator layout panicked translating AST data explicitly against '%s': %v", path, err)
	}

	return buf.Bytes(), nil
}
