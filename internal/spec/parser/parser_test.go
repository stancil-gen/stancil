package parser

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParser_ParseMinimal(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "minimal.yaml"))
	if err != nil {
		t.Fatalf("failed to read minimal.yaml: %v", err)
	}

	parser := NewParser()
	ast, err := parser.Parse(data)
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	if ast.Project != "orders-service" {
		t.Errorf("Expected project 'orders-service', got '%s'", ast.Project)
	}

	if len(ast.Tables) != 1 {
		t.Errorf("Expected 1 table, got %d", len(ast.Tables))
	} else if ast.Tables[0].Name != "users" {
		t.Errorf("Expected table 'users', got '%s'", ast.Tables[0].Name)
	}

	if len(ast.Resources) != 1 {
		t.Errorf("Expected 1 resource group, got %d", len(ast.Resources))
	} else if len(ast.Resources[0].APIs) != 1 {
		t.Errorf("Expected 1 API, got %d", len(ast.Resources[0].APIs))
	}

	api := ast.Resources[0].APIs[0]
	if len(api.Steps) != 1 {
		t.Errorf("Expected 1 step in API, got %d", len(api.Steps))
	} else if api.Steps[0].ID != "writeUser" {
		t.Errorf("Expected step ID 'writeUser', got '%s'", api.Steps[0].ID)
	} else if api.Steps[0].Touch.Table != "users" {
		t.Errorf("Expected touch table 'users', got '%s'", api.Steps[0].Touch.Table)
	}
}
