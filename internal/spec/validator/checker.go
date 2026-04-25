package validator

import (
	"fmt"
	"regexp"
	"stencil/internal/spec"
)

// SymbolTable serves as the project's universal registry for name resolution
type SymbolTable struct {
	Tables       map[string]int
	Types        map[string]int
	Externals    map[string]int
	Transactions map[string]int
	Configs      map[string]int
	
	// Complex subsystems
	Caches       map[string]bool
	Messaging    map[string]bool
	
	// Access Control
	Roles        map[string]bool
}

func NewSymbolTable() *SymbolTable {
	return &SymbolTable{
		Tables:       make(map[string]int),
		Types:        make(map[string]int),
		Externals:    make(map[string]int),
		Transactions: make(map[string]int),
		Configs:      make(map[string]int),
		Caches:       make(map[string]bool),
		Messaging:    make(map[string]bool),
		Roles:        make(map[string]bool),
	}
}

// Checker encapsulates the state of the semantic audit
type Checker struct {
	ast         *spec.SpecAST
	symbols     *SymbolTable
	errors      []ValidationError
	
	// Traversal state
	visiting    map[string]bool
	resolved    map[string]bool
}

func NewChecker(ast *spec.SpecAST) *Checker {
	return &Checker{
		ast:      ast,
		symbols:  NewSymbolTable(),
		visiting: make(map[string]bool),
		resolved: make(map[string]bool),
	}
}

func (c *Checker) Errors() []ValidationError {
	return c.errors
}

func (c *Checker) addError(path, code, msg string) {
	c.errors = append(c.errors, ValidationError{
		Path: path,
		Code: code,
		Msg:  msg,
	})
}

// Pass 1: Global Symbol Registration
func (c *Checker) RegisterSymbols() {
	// 1. Register Configs (First! as they are used in URLs)
	for i, conf := range c.ast.Config {
		if _, ok := c.symbols.Configs[conf.Name]; ok {
			c.addError(fmt.Sprintf("config[%d]", i), "DUPLICATE_CONFIG", fmt.Sprintf("Duplicate config variable '%s'", conf.Name))
		}
		c.symbols.Configs[conf.Name] = i
	}

	// 2. Register Custom Types
	for i, t := range c.ast.Types {
		if _, ok := c.symbols.Types[t.Name]; ok {
			c.addError(fmt.Sprintf("types[%d]", i), "DUPLICATE_TYPE", fmt.Sprintf("Duplicate type definition '%s'", t.Name))
		}
		c.symbols.Types[t.Name] = i
	}

	// 3. Register Tables
	for i, table := range c.ast.Tables {
		if _, ok := c.symbols.Tables[table.Name]; ok {
			c.addError(fmt.Sprintf("tables[%d]", i), "DUPLICATE_TABLE", fmt.Sprintf("Duplicate table definition '%s'", table.Name))
		}
		c.symbols.Tables[table.Name] = i
	}

	// 4. Register Externals
	for i, ext := range c.ast.Externals {
		if _, ok := c.symbols.Externals[ext.Name]; ok {
			c.addError(fmt.Sprintf("externals[%d]", i), "DUPLICATE_EXTERNAL", fmt.Sprintf("Duplicate external definition '%s'", ext.Name))
		}
		c.symbols.Externals[ext.Name] = i
	}
	
	// 5. Transactions — deferred to Phase 2
	// for i, tx := range c.ast.Transactions { ... }

	// 6. Caches — deferred to Phase 2
	// if c.ast.Cache != nil { ... }

	// 7. Messaging — deferred to Phase 2
	// if c.ast.Messaging != nil { ... }

	// 8. Auth Roles — deferred to Phase 2
	// if c.ast.Auth != nil { ... }
}

// ── Pass 2: Recursive Data Flow ───────────────────────────────────

func (c *Checker) ResolveAll() {
	// First resolve all custom types to catch circularities early
	for typeName := range c.symbols.Types {
		c.resolveType(typeName, "types")
	}

	// Then resolve all infrastructure (Tables depend on Types)
	for tableName := range c.symbols.Tables {
		c.resolveTable(tableName)
	}

	for extName := range c.symbols.Externals {
		c.resolveExternal(extName)
	}

	// Transactions — deferred to Phase 2
	// for txName := range c.symbols.Transactions { c.resolveTransaction(txName) }

	// Messaging — deferred to Phase 2
	// if c.ast.Messaging != nil { c.resolveMessaging() }

	// Cache — deferred to Phase 2
	// if c.ast.Cache != nil { c.resolveCache() }

	// Finally resolve APIs (APIs depend on Tables/Externals/Types)
	c.resolveAPIs()
}

func (c *Checker) resolveType(name string, scope string) {
	if c.resolved["type:"+name] {
		return
	}
	if c.visiting["type:"+name] {
		c.addError(scope, "CIRCULAR_DEPENDENCY", fmt.Sprintf("Circular reference detected in type '%s'", name))
		return
	}

	idx, ok := c.symbols.Types[name]
	if !ok {
		c.addError(scope, "UNKNOWN_TYPE", fmt.Sprintf("Type '%s' is not defined", name))
		return
	}

	c.visiting["type:"+name] = true
	t := c.ast.Types[idx]
	path := fmt.Sprintf("types[%d]", idx)

	for fIdx, field := range t.Fields {
		fPath := fmt.Sprintf("%s.fields[%d]", path, fIdx)
		if !c.isPrimitive(field.Type) {
			c.resolveType(field.Type, fPath)
		}
	}

	c.visiting["type:"+name] = false
	c.resolved["type:"+name] = true
}

func (c *Checker) resolveTable(name string) {
	idx := c.symbols.Tables[name]
	path := fmt.Sprintf("tables[%d]", idx)
	table := c.ast.Tables[idx]

	for fIdx, field := range table.Fields {
		fPath := fmt.Sprintf("%s.fields[%d]", path, fIdx)
		if !c.isPrimitive(field.Type) {
			// Tables can depend on custom types
			if _, ok := c.symbols.Types[field.Type]; ok {
				c.resolveType(field.Type, fPath+".type")
			} else {
				c.addError(fPath+".type", "UNKNOWN_TYPE", fmt.Sprintf("Field '%s' uses unknown type '%s'", field.Name, field.Type))
			}
		}

		if field.Type == "enum" && len(field.Values) == 0 {
			c.addError(fPath, "MISSING_ENUM_VALUES", fmt.Sprintf("Enum field '%s' must provide values", field.Name))
		}
	}

	// State Machine Checks (Advanced)
	if table.States != nil {
		c.validateStateMachine(table, path+".states")
	}
}

func (c *Checker) resolveExternal(name string) {
	idx := c.symbols.Externals[name]
	path := fmt.Sprintf("externals[%d]", idx)
	ext := c.ast.Externals[idx]

	// Check BaseURL for variables
	c.checkVariables(ext.BaseURL, path+".base_url")

	for cIdx, call := range ext.Calls {
		cPath := fmt.Sprintf("%s.calls[%d]", path, cIdx)
		c.checkVariables(call.Path, cPath+".path")
	}
}

func (c *Checker) resolveTransaction(name string) {
	idx := c.symbols.Transactions[name]
	path := fmt.Sprintf("transactions[%d]", idx)
	tx := c.ast.Transactions[idx]

	for sIdx, step := range tx.Steps {
		if step.SQL == "" {
			c.addError(fmt.Sprintf("%s.steps[%d]", path, sIdx), "EMPTY_SQL", "Transaction step must provide SQL statement")
		}
		for pIdx, p := range step.Params {
			if !c.isPrimitive(p.Type) {
				if _, ok := c.symbols.Types[p.Type]; !ok {
					c.addError(fmt.Sprintf("%s.steps[%d].params[%d]", path, sIdx, pIdx), "UNKNOWN_TYPE", fmt.Sprintf("Transaction param uses unknown type '%s'", p.Type))
				}
			}
		}
	}
}

func (c *Checker) resolveMessaging() {
	for i, p := range c.ast.Messaging.Producers {
		path := fmt.Sprintf("messaging.producers[%d]", i)
		// 1. Verify Event is a valid Type definition
		if _, ok := c.symbols.Types[p.Event]; !ok {
			c.addError(path+".event", "UNKNOWN_TYPE", fmt.Sprintf("Producer event '%s' must be a defined Type", p.Event))
		}
		
		// 2. Key is a field name in the event object, no global type check needed
	}

	for i, cns := range c.ast.Messaging.Consumers {
		path := fmt.Sprintf("messaging.consumers[%d]", i)
		// 1. Verify Event is a valid Type definition
		if _, ok := c.symbols.Types[cns.Event]; !ok {
			c.addError(path+".event", "UNKNOWN_TYPE", fmt.Sprintf("Consumer event '%s' must be a defined Type", cns.Event))
		}
	}
}

func (c *Checker) resolveCache() {
	for i, iface := range c.ast.Cache.Interfaces {
		path := fmt.Sprintf("cache.interfaces[%d]", i)
		if !c.isPrimitive(iface.ValueType) {
			if _, ok := c.symbols.Types[iface.ValueType]; !ok {
				c.addError(path+".value_type", "UNKNOWN_TYPE", fmt.Sprintf("Cache interface uses unknown value type '%s'", iface.ValueType))
			}
		}
	}
}

func (c *Checker) checkVariables(input string, path string) {
	// Simple regex to find ${VARIABLE}
	re := regexp.MustCompile(`\$\{([A-Z0-9_]+)\}`)
	matches := re.FindAllStringSubmatch(input, -1)

	for _, m := range matches {
		varName := m[1]
		if _, ok := c.symbols.Configs[varName]; !ok {
			c.addError(path, "UNDEFINED_CONFIG", fmt.Sprintf("Variable '${%s}' is not defined in the global config: block", varName))
		}
	}
}


func (c *Checker) resolveAPIs() {
	for gIdx, group := range c.ast.Resources {
		for aIdx, api := range group.APIs {
			path := fmt.Sprintf("resources[%d].apis[%d]", gIdx, aIdx)

			if len(api.Steps) == 0 {
				c.addError(path, "API_NO_STEPS", fmt.Sprintf("API '%s' must declare at least one step", api.Name))
			}
			
			// Auth Handshake
			if api.Auth != "" && c.ast.Auth == nil {
				c.addError(path+".auth", "MISSING_AUTH_BLOCK", "API requires auth but no global auth: block is defined")
			}

			// RBAC Check
			for rIdx, role := range api.Roles {
				if !c.symbols.Roles[role] {
					c.addError(fmt.Sprintf("%s.roles[%d]", path, rIdx), "UNKNOWN_ROLE", fmt.Sprintf("Role '%s' is not defined in auth: config", role))
				}
			}

			// Steps Cross-Reference
			seenStepIDs := make(map[string]bool)
			for sIdx, step := range api.Steps {
				if step.ID != "" {
					if seenStepIDs[step.ID] {
						c.addError(fmt.Sprintf("%s.steps[%d]", path, sIdx), "DUPLICATE_STEP_ID", fmt.Sprintf("Duplicate step ID '%s' in API '%s'", step.ID, api.Name))
					}
					seenStepIDs[step.ID] = true
				}

				sPath := fmt.Sprintf("%s.steps[%d].touch", path, sIdx)
				t := step.Touch
				
				if t.Table != "" {
					if _, ok := c.symbols.Tables[t.Table]; !ok {
						c.addError(sPath+".table", "UNKNOWN_TABLE", fmt.Sprintf("Touch references unknown table '%s'", t.Table))
					}
				}
				if t.External != "" {
					if _, ok := c.symbols.Externals[t.External]; !ok {
						c.addError(sPath+".external", "UNKNOWN_EXTERNAL", fmt.Sprintf("Touch references unknown external '%s'", t.External))
					}
				}
				// Cache / Messaging / Transaction touch validation — deferred to Phase 2
				// if t.Cache != "" { ... }
				// if t.Message != "" { ... }
				// if t.Transaction != "" { ... }
			}

			// Privacy Guard
			c.checkPrivacyLeaks(api, path)
		}
	}
}

func (c *Checker) checkPrivacyLeaks(api spec.APIAST, path string) {
	// If API touches a table, check if response DTO leaks private fields
	for _, step := range api.Steps {
		if step.Touch.Table == "" {
			continue
		}
		
		tableIdx := c.symbols.Tables[step.Touch.Table]
		table := c.ast.Tables[tableIdx]
		
		privateFields := make(map[string]bool)
		for _, f := range table.Fields {
			if f.Private {
				privateFields[f.Name] = true
			}
		}

		// Check response DTO (if defined inline or resolved)
		if api.DTOs != nil && api.DTOs.Response != nil {
			for fIdx, df := range api.DTOs.Response.Fields {
				if privateFields[df.Name] {
					c.addError(fmt.Sprintf("%s.dtos.response.fields[%d]", path, fIdx), "PRIVATE_FIELD_LEAK", fmt.Sprintf("Response DTO leaks private field '%s' from table '%s'", df.Name, table.Name))
				}
			}
		}
	}
}


func (c *Checker) validateStateMachine(table spec.TableAST, path string) {
	// 1. Verify state field exists and is enum
	var stateField *spec.FieldAST
	for _, f := range table.Fields {
		if f.Name == table.States.Field {
			stateField = &f
			break
		}
	}

	if stateField == nil {
		c.addError(path+".field", "STATE_FIELD_NOT_FOUND", fmt.Sprintf("State field '%s' not found in table", table.States.Field))
		return
	}
	if stateField.Type != "enum" {
		c.addError(path+".field", "INVALID_STATE_TYPE", fmt.Sprintf("State field '%s' must be an 'enum' to support a state machine", stateField.Name))
	}

	// 2. BFS Reachability (Simplified for now, just checking value validity)
	validValues := make(map[string]bool)
	for _, v := range stateField.Values {
		validValues[v] = true
	}

	for i, trans := range table.States.Transitions {
		if !validValues[trans.From] {
			c.addError(fmt.Sprintf("%s.transitions[%d].from", path, i), "INVALID_STATE", fmt.Sprintf("'%s' is not a valid enum value for field '%s'", trans.From, stateField.Name))
		}
		if !validValues[trans.To] {
			c.addError(fmt.Sprintf("%s.transitions[%d].to", path, i), "INVALID_STATE", fmt.Sprintf("'%s' is not a valid enum value for field '%s'", trans.To, stateField.Name))
		}
	}

	// 3. True BFS Reachability
	if len(stateField.Values) > 0 {
		initialState := stateField.Values[0] // Assume first enum value is the implicit start
		reachable := make(map[string]bool)
		reachable[initialState] = true
		
		adj := make(map[string][]string)
		for _, trans := range table.States.Transitions {
			adj[trans.From] = append(adj[trans.From], trans.To)
		}

		queue := []string{initialState}
		for len(queue) > 0 {
			curr := queue[0]
			queue = queue[1:]
			for _, next := range adj[curr] {
				if !reachable[next] {
					reachable[next] = true
					queue = append(queue, next)
				}
			}
		}

		for _, state := range stateField.Values {
			if !reachable[state] {
				c.addError(path, "UNREACHABLE_STATE", fmt.Sprintf("State '%s' is unreachable from initial state '%s'", state, initialState))
			}
		}
	}
}

// CheckPrimitive verifies if a type is a native Stencil primitive
func (c *Checker) isPrimitive(t string) bool {
	switch t {
	case "str", "int", "uuid", "bool", "decimal", "timestamp", "json", "enum", "date":
		return true
	}
	return false
}
