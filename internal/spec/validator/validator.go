package validator

import "stencil/internal/spec"

import "fmt"

// ValidationError represents a single failure in the spec
type ValidationError struct {
	Path string
	Code string
	Msg  string
}

func (e ValidationError) Error() string {
	return fmt.Sprintf("[%s] %s: %s", e.Code, e.Path, e.Msg)
}

// ValidationRule is the signature for checking parts of the AST
type ValidationRule func(ast *spec.SpecAST) []ValidationError

// Validator evaluates the entire AST and returns a list of semantic errors
type Validator struct{}

func NewValidator() *Validator {
	return &Validator{}
}

// Validate executes the Advanced Semantic Checker.
func (v *Validator) Validate(ast *spec.SpecAST) []ValidationError {
	c := NewChecker(ast)
	
	// Pass 1: Discovery (O(1) Map building)
	c.RegisterSymbols()
	
	// Pass 2: Analysis (Recursive Traversal)
	c.ResolveAll()
	
	// Pass 3: Metadata (Top-level attributes)
	c.CheckMetadata()
	
	return c.Errors()
}

func (c *Checker) CheckMetadata() {
	if c.ast.Project == "" {
		c.addError("project", "MISSING_PROJECT", "Project name is required")
	}
	if c.ast.Lang == "" {
		c.addError("lang", "MISSING_LANG", "Language is required")
	}
	if c.ast.Framework == "" {
		c.addError("framework", "MISSING_FRAMEWORK", "Framework is required")
	}
	if c.ast.DB == "" {
		c.addError("db", "MISSING_DB", "DB is required")
	}
	if len(c.ast.Resources) == 0 {
		c.addError("resources", "MISSING_RESOURCES", "At least one API resource group must be defined")
	}
}
