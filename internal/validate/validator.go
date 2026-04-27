package validate

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
	if len(c.ast.Resources) == 0 {
		c.addError("resources", "MISSING_RESOURCES", "At least one API resource group must be defined")
	}

	c.checkDatabases()
	c.checkTableDB()
}

// checkDatabases validates the db: block — each entry must have a name and a resolvable URL.
func (c *Checker) checkDatabases() {
	if len(c.ast.Tables) > 0 && len(c.ast.Databases) == 0 {
		c.addError("db", "MISSING_DB_BLOCK",
			"spec defines tables but no db: block is declared — add a db: block with at least one database entry")
		return
	}

	validDrivers := map[string]bool{"postgres": true, "mysql": true, "mongo": true}
	validFrameworks := map[string]bool{"gorm": true, "sqlx": true, "sqlc": true}
	seenNames := map[string]bool{}

	for i, db := range c.ast.Databases {
		path := fmt.Sprintf("db[%d]", i)

		if db.Name == "" {
			c.addError(path, "DB_MISSING_NAME", "database entry must have a name")
			continue
		}
		if seenNames[db.Name] {
			c.addError(path, "DB_DUPLICATE_NAME", fmt.Sprintf("duplicate database name %q", db.Name))
		}
		seenNames[db.Name] = true

		if db.URL == "" {
			c.addError(path+".url", "DB_MISSING_URL",
				fmt.Sprintf("database %q must declare a url (e.g. url: ${DATABASE_URL})", db.Name))
		} else {
			// Validate the ${VAR} reference exists in config
			varName := extractConfigVar(db.URL)
			if varName != "" {
				if _, ok := c.symbols.Configs[varName]; !ok {
					c.addError(path+".url", "DB_UNKNOWN_URL_VAR",
						fmt.Sprintf("database %q references config var %q which is not declared in config:", db.Name, varName))
				}
			}
		}

		driver := db.Driver
		if driver == "" {
			driver = db.Name
		}
		if !validDrivers[driver] {
			c.addError(path+".driver", "DB_UNKNOWN_DRIVER",
				fmt.Sprintf("database %q has unknown driver %q — must be one of: postgres, mysql, mongo", db.Name, driver))
		}

		if db.Framework != "" && !validFrameworks[db.Framework] {
			c.addError(path+".framework", "DB_UNKNOWN_FRAMEWORK",
				fmt.Sprintf("database %q has unknown framework %q — must be one of: gorm, sqlx, sqlc", db.Name, db.Framework))
		}
	}
}

// checkTableDB validates that each table's db: field references a declared database.
func (c *Checker) checkTableDB() {
	if len(c.ast.Databases) == 0 {
		return // covered by checkDatabases
	}
	dbNames := map[string]bool{}
	for _, db := range c.ast.Databases {
		dbNames[db.Name] = true
	}

	for _, t := range c.ast.Tables {
		if t.DB == "" && len(c.ast.Databases) > 1 {
			c.addError(fmt.Sprintf("tables.%s", t.Name), "TABLE_AMBIGUOUS_DB",
				fmt.Sprintf("table %q does not declare a db: field but multiple databases are declared — add 'db: <name>' to the table", t.Name))
		} else if t.DB != "" && !dbNames[t.DB] {
			c.addError(fmt.Sprintf("tables.%s.db", t.Name), "TABLE_UNKNOWN_DB",
				fmt.Sprintf("table %q references unknown database %q — declare it in the db: block", t.Name, t.DB))
		}
	}
}

// extractConfigVar extracts the variable name from a ${VAR} reference.
func extractConfigVar(ref string) string {
	if len(ref) > 3 && ref[0] == '$' && ref[1] == '{' && ref[len(ref)-1] == '}' {
		return ref[2 : len(ref)-1]
	}
	return ""
}

