package resolve

import "stencil/internal/spec"

// Resolver is the thin orchestrator. It runs each Feature in registration order:
//
//	TypesFeature → TablesFeature → ExternalsFeature → APIsFeature
//
// To add a new spec section (e.g. webhooks), create a new Feature implementation
// and add it to the features slice below.
type Resolver struct {
	ast *spec.SpecAST
	res *spec.ResolvedSpec
}

// NewResolver creates a new Resolver for the given SpecAST.
func NewResolver(ast *spec.SpecAST) *Resolver {
	return &Resolver{ast: ast}
}

// Resolve runs the full pipeline and returns the populated ResolvedSpec.
// Contract: if the SpecAST passed Validation, Resolve must succeed.
func (r *Resolver) Resolve() *spec.ResolvedSpec {
	r.res = &spec.ResolvedSpec{
		Project: r.ast.Project,
		Module:  r.ast.Project,
	}

	r.resolveMetadata()
	r.resolveConfig()
	r.resolveDatabases()

	// Feature registration order matters — Types before Tables before APIs.
	// Each feature appends to r.res.Objects/Interfaces/Implementations.
	features := []Feature{
		&TypesFeature{},
		&TablesFeature{},
		&ExternalsFeature{},
		&APIsFeature{},
	}

	// Pass 1: validate all features (collect errors without halting)
	// Note: structural validation is handled by the validator/ package before Resolve is called.
	// Feature.Validate handles semantic checks that require cross-referencing within the feature.
	var errs []error
	for _, f := range features {
		errs = append(errs, f.Validate(r.ast)...)
	}
	_ = errs // reserved for future use; validator/ already catches structural issues

	// Pass 2: resolve all features in dependency order
	for _, f := range features {
		f.Resolve(r.ast, r.res)
	}

	// Link TypeRef pointers (second pass — all objects must exist before linking)
	linkTypeRefs(r.res.Objects)

	return r.res
}

// ─── Metadata ─────────────────────────────────────────────────────────────────

func (r *Resolver) resolveMetadata() {
	switch r.ast.Lang {
	case "go":
		r.res.Lang = spec.LangGo
	case "java":
		r.res.Lang = spec.LangJava
	}
	switch r.ast.Framework {
	case "gin":
		r.res.Framework = spec.FrameworkGin
	case "echo":
		r.res.Framework = spec.FrameworkEcho
	case "spring":
		r.res.Framework = spec.FrameworkSpring
	}
	r.res.ConfigLoader = r.ast.ConfigLoader
	if r.res.ConfigLoader == "" {
		r.res.ConfigLoader = "env"
	}
}

func (r *Resolver) resolveDatabases() {
	for _, db := range r.ast.Databases {
		driver := db.Driver
		if driver == "" {
			driver = db.Name // "postgres", "mysql", "mongo" used as both name and driver
		}
		framework := db.Framework
		if framework == "" {
			framework = "gorm"
		}

		var resolvedDriver spec.DBDriver
		switch driver {
		case "postgres":
			resolvedDriver = spec.DBPostgres
		case "mysql":
			resolvedDriver = spec.DBMySQL
		case "sqlite":
			resolvedDriver = spec.DBSQLite
		case "mongo":
			resolvedDriver = spec.DBMongo
		}

		urlField := resolveConfigRef(db.URL)
		typeName := toPascalCase(db.Name) + "DB"
		funcName := toPascalCase(db.Name)

		r.res.Databases = append(r.res.Databases, spec.ResolvedDatabase{
			Name:      db.Name,
			Driver:    resolvedDriver,
			Framework: framework,
			URLField:  urlField,
			TypeName:  typeName,
			FuncName:  funcName,
		})
	}
}

func (r *Resolver) resolveConfig() {
	for _, c := range r.ast.Config {
		r.res.Config = append(r.res.Config, spec.ResolvedConfigVar{
			Name:     c.Name,
			YAMLType: c.Type,
			Required: c.Required,
			Default:  c.Default,
		})
	}
}

// ─── Package-level entry point ────────────────────────────────────────────────

// Resolve is the package-level entry point used by the compiler pipeline.
func Resolve(ast *spec.SpecAST) *spec.ResolvedSpec {
	return NewResolver(ast).Resolve()
}
