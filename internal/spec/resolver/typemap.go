package resolver

import "stencil/internal/spec"

// MapType converts a Stencil DSL type string into a language-agnostic TypeDescriptor.
// This is the single source of truth for all type mappings in the resolver.
// Called by all Level 1 object builders.
func MapType(stencilType string, lang spec.Lang, db spec.DBDriver, nullable bool) spec.TypeDescriptor {
	td := mapBase(stencilType, db)

	// Custom types are always raw value types (no pointer) for now.
	// Pointer optimization for nullable/large nested types is deferred — see pending_items.md.
	if td.IsCustom {
		td.DBNullable = nullable
		return td
	}

	// For primitives: apply nullable by promoting to pointer
	if nullable && !td.GoPointer {
		td.GoPointer = true
		td.DBNullable = true
		if len(td.GoType) > 0 && td.GoType[0] != '*' {
			td.GoType = "*" + td.GoType
		}
	}

	return td
}

func mapBase(stencilType string, db spec.DBDriver) spec.TypeDescriptor {
	switch stencilType {
	case "str":
		return spec.TypeDescriptor{
			Kind:   spec.TypeStr,
			GoType: "string", JavaType: "String",
			DBType: dbVarchar(db),
		}
	case "int":
		return spec.TypeDescriptor{
			Kind:   spec.TypeInt,
			GoType: "int64", JavaType: "Long",
			DBType: "BIGINT",
		}
	case "uuid":
		return spec.TypeDescriptor{
			Kind:   spec.TypeUUID,
			GoType: "uuid.UUID", JavaType: "UUID",
			DBType: "UUID",
		}
	case "bool":
		return spec.TypeDescriptor{
			Kind:   spec.TypeBool,
			GoType: "bool", JavaType: "Boolean",
			DBType: "BOOLEAN",
		}
	case "decimal":
		return spec.TypeDescriptor{
			Kind:   spec.TypeDecimal,
			GoType: "decimal.Decimal", JavaType: "BigDecimal",
			DBType: "NUMERIC",
		}
	case "timestamp":
		return spec.TypeDescriptor{
			Kind:   spec.TypeTimestamp,
			GoType: "time.Time", JavaType: "Instant",
			DBType: "TIMESTAMP",
		}
	case "date":
		return spec.TypeDescriptor{
			Kind:   spec.TypeDate,
			GoType: "time.Time", JavaType: "LocalDate",
			DBType: "DATE",
		}
	case "json":
		return spec.TypeDescriptor{
			Kind:   spec.TypeJSON,
			GoType: "json.RawMessage", JavaType: "String",
			DBType: "JSONB",
		}
	case "enum":
		return spec.TypeDescriptor{
			Kind:   spec.TypeEnum,
			GoType: "string", JavaType: "String",
			DBType: "TEXT",
		}
	default:
		// Custom type — raw value by default; MapType adds * when nullable: true
		return spec.TypeDescriptor{
			Kind:      spec.TypeCustom,
			GoType:    toPascalCase(stencilType),
			JavaType:  toPascalCase(stencilType),
			DBType:    "JSONB",
			GoPointer: false,
			IsCustom:  true,
		}
	}
}

func dbVarchar(db spec.DBDriver) string {
	switch db {
	case spec.DBMySQL:
		return "VARCHAR(255)"
	default: // postgres and others
		return "TEXT"
	}
}

// FormatVerb returns the fmt verb for a TypeKind, used in cache key construction.
func FormatVerb(kind spec.TypeKind) string {
	switch kind {
	case spec.TypeInt:
		return "%d"
	case spec.TypeUUID:
		return "%s"
	case spec.TypeStr:
		return "%s"
	case spec.TypeBool:
		return "%v"
	default:
		return "%v"
	}
}
