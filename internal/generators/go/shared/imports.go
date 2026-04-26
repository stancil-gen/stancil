package shared

import "strings"

// TypeImportMap maps Go type substrings to their import paths.
var TypeImportMap = map[string]string{
	"uuid.UUID":       "github.com/google/uuid",
	"decimal.Decimal": "github.com/shopspring/decimal",
	"time.Time":       "time",
	"json.RawMessage": "encoding/json",
	"gorm.DB":         "gorm.io/gorm",
}

// CollectImports scans a list of Go type strings and returns the deduplicated
// import paths required by those types.
func CollectImports(goTypes []string) []string {
	seen := map[string]bool{}
	var imports []string
	for _, goType := range goTypes {
		for substr, importPath := range TypeImportMap {
			if strings.Contains(goType, substr) && !seen[importPath] {
				seen[importPath] = true
				imports = append(imports, importPath)
			}
		}
	}
	return imports
}
