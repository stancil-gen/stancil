package parse

import (
	"fmt"
	"os"
	"stencil/internal/spec"

	"gopkg.in/yaml.v3"
)

// Parser handles translating YAML DSL into the spec.SpecAST
type Parser struct{}

func NewParser() *Parser {
	return &Parser{}
}

// Parse strict-unmarshals the byte array.
func (p *Parser) Parse(data []byte) (*spec.SpecAST, error) {
	var ast spec.SpecAST
	if err := yaml.Unmarshal(data, &ast); err != nil {
		return nil, fmt.Errorf("failed to parse yaml: %w", err)
	}

	return &ast, nil
}

// ParseFile is a convenience helper that reads a file and parses it.
func ParseFile(path string) (*spec.SpecAST, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}
	return NewParser().Parse(data)
}
