package validate

import (
	"stencil/internal/spec"
	parser "stencil/internal/parse"

	"os"
	"path/filepath"
	"testing"
)

func TestValidator_ValidAST(t *testing.T) {
	// Parse the correct minimal.yaml
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "minimal.yaml"))
	if err != nil {
		t.Fatalf("failed to read minimal.yaml: %v", err)
	}

	parser := parser.NewParser()
	ast, _ := parser.Parse(data)

	validator := NewValidator()
	errors := validator.Validate(ast)

	if len(errors) > 0 {
		t.Fatalf("Expected 0 validation errors for minimal.yaml, got %d: %v", len(errors), errors)
	}
}

func TestValidator_ErrorsCaught(t *testing.T) {
	// Create a intentionally broken AST
	brokenAST := &spec.SpecAST{
		// Missing project, lang, framework, db!
		Tables: []spec.TableAST{
			{
				Name: "users",
				Fields: []spec.FieldAST{
					{Name: "email", Type: "unknown_super_string"},      // INVALID: Unknown type
					{Name: "status", Type: "enum", Values: []string{}}, // INVALID: Missing enum values
				},
			},
		},
		Resources: []spec.ResourceGroupAST{
			{
				Group: "BrokenGroup",
				APIs: []spec.APIAST{
					{
						Name: "InvalidAPI",
						Steps: []spec.StepAST{
							{ID: "step1", Touch: spec.TouchAST{Table: "non_existent_table"}}, // Invalid table reference
							{ID: "step2", Touch: spec.TouchAST{Table: "users"}},
							{ID: "step2", Touch: spec.TouchAST{Table: "users"}}, // Duplicate step ID
						},
					},
					{
						Name:  "EmptyAPI",
						Steps: []spec.StepAST{}, // No touches!
					},
				},
			},
		},
	}

	validator := NewValidator()
	errors := validator.Validate(brokenAST)

	// We expect multiple aggregated errors here:
	// MISSING_PROJECT, MISSING_LANG, MISSING_FRAMEWORK, MISSING_DB
	// UNKNOWN_TABLE
	// DUPLICATE_FLAG
	// API_NO_STEPS
	// UNKNOWN_TYPE
	// MISSING_ENUM_VALUES

	if len(errors) != 9 {
		t.Fatalf("Expected exactly 9 validation errors, got %d. Errors: %v", len(errors), errors)
	}

	// Helper to check if a code was thrown
	hasCode := func(code string) bool {
		for _, e := range errors {
			if e.Code == code {
				return true
			}
		}
		return false
	}

	if !hasCode("MISSING_PROJECT") {
		t.Errorf("Did not catch missing project")
	}
	if !hasCode("MISSING_LANG") {
		t.Errorf("Did not catch missing lang")
	}
	if !hasCode("UNKNOWN_TABLE") {
		t.Errorf("Did not catch unknown table reference")
	}
	if !hasCode("DUPLICATE_STEP_ID") {
		t.Errorf("Did not catch duplicate step ID collision")
	}
	if !hasCode("API_NO_STEPS") {
		t.Errorf("Did not catch empty API touches")
	}
	if !hasCode("UNKNOWN_TYPE") {
		t.Errorf("Did not catch unknown field type")
	}
	if !hasCode("MISSING_ENUM_VALUES") {
		t.Errorf("Did not catch missing enum values")
	}
}
