package resolver

import "stencil/internal/spec"

// Resolver is the main orchestrator. It runs the 7-step pipeline:
//   Level 1 → Level 1A (imports) → Level 2 → Level 2A → Level 3 → Level 3A
type Resolver struct {
	ast *spec.SpecAST
	res *spec.ResolvedSpec
	ir  *ImportResolver
}

// NewResolver creates a new Resolver for the given SpecAST.
func NewResolver(ast *spec.SpecAST) *Resolver {
	return &Resolver{ast: ast}
}

// Resolve runs the full 7-step pipeline and returns the populated ResolvedSpec.
// Contract: if the SpecAST passed Validation, Resolve must succeed.
// If something panics here, it is a bug in the Resolver.
func (r *Resolver) Resolve() *spec.ResolvedSpec {
	r.res = &spec.ResolvedSpec{
		Project: r.ast.Project,
		Module:  r.ast.Project, // Phase 1: module == project name
	}

	// Step 2: Resolve metadata (lang/db/framework → typed constants)
	r.resolveMetadata()

	// Import resolver (shared across all three levels)
	r.ir = NewImportResolver(r.res.Lang, r.res.Module)

	// ── Level 1: Objects ──────────────────────────────────────────────────────

	// Step 1a: Custom types (leaves — nothing references them first in Phase 1)
	for _, t := range r.ast.Types {
		obj := buildTypeObject(t, r.res.Lang, r.res.DB)
		r.res.Objects = append(r.res.Objects, obj)
	}

	// Step 1b: Table models (may reference types)
	for _, t := range r.ast.Tables {
		obj := buildTableModel(t, r.res.Lang, r.res.DB)
		r.res.Objects = append(r.res.Objects, obj)
	}

	// Step 1c: API types (RequestDTO, ResponseDTO, SharedContext)
	for _, group := range r.ast.Resources {
		for _, api := range group.APIs {
			if req := buildRequestDTO(api, group, r.res.Lang, r.res.DB); req != nil {
				r.res.Objects = append(r.res.Objects, *req)
			}
			if resp := buildResponseDTO(api, group, r.res.Lang, r.res.DB); resp != nil {
				r.res.Objects = append(r.res.Objects, *resp)
			}
			ctx := buildSharedContext(api, group, r.res.Lang, r.res.DB)
			r.res.Objects = append(r.res.Objects, ctx)
		}
	}

	// Step 1d: External types (ExternalInput, ExternalOutput)
	for _, ext := range r.ast.Externals {
		for _, call := range ext.Calls {
			if inp := buildExternalInput(ext, call, r.res.Lang, r.res.DB); inp != nil {
				r.res.Objects = append(r.res.Objects, *inp)
			}
			if out := buildExternalOutput(ext, call, r.res.Lang, r.res.DB); out != nil {
				r.res.Objects = append(r.res.Objects, *out)
			}
		}
	}

	// Step 1e: Transaction params — deferred to Phase 2
	// for _, tx := range r.ast.Transactions {
	// 	obj := buildTransactionParams(tx, r.res.Lang, r.res.DB)
	// 	r.res.Objects = append(r.res.Objects, obj)
	// }

	// Step 1f: LinkTypeRefs (second pass — fills TypeRef pointers)
	linkTypeRefs(r.res.Objects)

	// Step 1A: ImportResolver over all objects
	for i := range r.res.Objects {
		r.res.Objects[i].Imports[r.res.Lang] = r.ir.ForObject(&r.res.Objects[i])
	}

	// Config
	r.resolveConfig()

	// Stash raw externals for generators that need AST-only metadata
	r.res.RawExternals = r.ast.Externals

	// Subsystems — deferred to Phase 2
	// r.resolveMessaging()
	// r.resolveAuth()

	// ── Level 2: Interfaces ────────────────────────────────────────────────────

	// Collect pointer slice for lookups in lower levels
	allObjects := r.objectPtrs()

	// Step 2a: Repository interfaces (Tables)
	for _, t := range r.ast.Tables {
		tableObj := r.res.ObjectByName(modelName(t.Name))
		iface := buildRepositoryInterface(t, tableObj, r.res.Lang, r.res.DB)
		r.res.Interfaces = append(r.res.Interfaces, iface)
	}

	// Step 2b: Cache interfaces — deferred to Phase 2
	// if r.ast.Cache != nil {
	// 	for _, ci := range r.ast.Cache.Interfaces {
	// 		valueObj := r.res.ObjectByName(toPascalCase(ci.ValueType))
	// 		iface := buildCacheInterface(ci, valueObj)
	// 		r.res.Interfaces = append(r.res.Interfaces, iface)
	// 	}
	// }

	// Step 2c: External interfaces
	for _, ext := range r.ast.Externals {
		iface := buildExternalInterface(ext, allObjects)
		r.res.Interfaces = append(r.res.Interfaces, iface)
	}

	// Step 2d: Hook interfaces — one per API (Resources)
	for _, group := range r.ast.Resources {
		for _, api := range group.APIs {
			ctxObj := r.res.ObjectByName(toPascalCase(api.Name) + "Context")
			iface := buildHookInterface(api, group, ctxObj)
			r.res.Interfaces = append(r.res.Interfaces, iface)
		}
	}

	// Step 2d-mapper: Mapper interfaces — one per API that declares steps
	for _, group := range r.ast.Resources {
		for _, api := range group.APIs {
			if len(api.Steps) == 0 {
				continue
			}
			ctxObj := r.res.ObjectByName(toPascalCase(api.Name) + "Context")
			iface := buildMapperInterface(api, group, ctxObj, r.ast.Externals)
			r.res.Interfaces = append(r.res.Interfaces, iface)
		}
	}

	// Step 2e: Service interfaces — one per ResourceGroup
	for _, group := range r.ast.Resources {
		iface := buildServiceInterface(group, allObjects)
		r.res.Interfaces = append(r.res.Interfaces, iface)
	}

	// Step 2A: ImportResolver over all interfaces
	for i := range r.res.Interfaces {
		r.res.Interfaces[i].Imports[r.res.Lang] = r.ir.ForInterface(&r.res.Interfaces[i])
	}

	// ── Level 3: Implementations ───────────────────────────────────────────────

	allInterfaces := r.interfacePtrs()

	// Step 3a: Repository implementations (Tables)
	for _, t := range r.ast.Tables {
		tableObj := r.res.ObjectByName(modelName(t.Name))
		repoIface := r.res.InterfaceByName(modelName(t.Name) + "Repository")
		impl := buildRepositoryImpl(t, tableObj, repoIface)
		r.res.Implementations = append(r.res.Implementations, impl)
	}

	// Step 3b: Cache implementations + mocks — deferred to Phase 2
	// if r.ast.Cache != nil {
	// 	for _, ci := range r.ast.Cache.Interfaces {
	// 		cacheIface := r.res.InterfaceByName(ci.Name)
	// 		valueObj := r.res.ObjectByName(toPascalCase(ci.ValueType))
	// 		impl := buildCacheImpl(ci, cacheIface, valueObj)
	// 		r.res.Implementations = append(r.res.Implementations, impl)
	// 		mock := buildCacheMock(ci, cacheIface)
	// 		r.res.Implementations = append(r.res.Implementations, mock)
	// 	}
	// }

	// Step 3c: External implementations + mocks
	for _, ext := range r.ast.Externals {
		extIface := r.res.InterfaceByName(toPascalCase(ext.Name))
		impl := buildExternalImpl(ext, extIface, allObjects)
		r.res.Implementations = append(r.res.Implementations, impl)
		mock := buildExternalMock(ext, extIface)
		r.res.Implementations = append(r.res.Implementations, mock)
	}

	// Step 3d: Transaction implementations — deferred to Phase 2
	// for _, tx := range r.ast.Transactions {
	// 	impl := buildTransactionImpl(tx, allObjects)
	// 	r.res.Implementations = append(r.res.Implementations, impl)
	// }

	// Step 3e: Service implementations (depends on everything above)
	for _, group := range r.ast.Resources {
		serviceIface := r.res.InterfaceByName(toPascalCase(group.Group) + "Service")
		impl := buildServiceImpl(group, serviceIface, allObjects, allInterfaces)
		r.res.Implementations = append(r.res.Implementations, impl)
	}

	// Step 3f: Default mapper implementations — one per API that declares steps
	for _, group := range r.ast.Resources {
		for _, api := range group.APIs {
			if len(api.Steps) == 0 {
				continue
			}
			mapperIface := r.res.InterfaceByName(toPascalCase(api.Name) + "Mappers")
			ctxObj := r.res.ObjectByName(toPascalCase(api.Name) + "Context")
			impl := buildDefaultMapperImpl(api, group, mapperIface, ctxObj, allObjects, r.ast.Externals)
			r.res.Implementations = append(r.res.Implementations, impl)
		}
	}

	// Step 3A: ImportResolver over all implementations
	for i := range r.res.Implementations {
		r.res.Implementations[i].Imports[r.res.Lang] = r.ir.ForImplementation(&r.res.Implementations[i])
	}

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
	switch r.ast.DB {
	case "postgres":
		r.res.DB = spec.DBPostgres
	case "mysql":
		r.res.DB = spec.DBMySQL
	case "mongo":
		r.res.DB = spec.DBMongo
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

func (r *Resolver) resolveConfig() {
	for _, c := range r.ast.Config {
		goType := "string"
		switch c.Type {
		case "int":
			goType = "int"
		case "bool":
			goType = "bool"
		}
		r.res.Config = append(r.res.Config, spec.ResolvedConfigVar{
			Name:     c.Name,
			GoType:   goType,
			Required: c.Required,
			Default:  c.Default,
		})
	}
}

func (r *Resolver) resolveMessaging() {
	if r.ast.Messaging == nil {
		return
	}
	rm := &spec.ResolvedMessaging{
		Broker:  r.ast.Messaging.Broker,
		Brokers: r.ast.Messaging.Brokers,
	}
	for _, p := range r.ast.Messaging.Producers {
		rm.Producers = append(rm.Producers, spec.ResolvedProducer{
			Topic: p.Topic,
			Event: p.Event,
			Key:   p.Key,
		})
	}
	for _, c := range r.ast.Messaging.Consumers {
		rm.Consumers = append(rm.Consumers, spec.ResolvedConsumer{
			Topic:   c.Topic,
			Event:   c.Event,
			Group:   c.Group,
			Handler: c.Handler,
		})
	}
	r.res.Messaging = rm
}

func (r *Resolver) resolveAuth() {
	if r.ast.Auth == nil {
		return
	}
	r.res.Auth = &spec.ResolvedAuth{
		Provider: r.ast.Auth.Provider,
		Expiry:   r.ast.Auth.Expiry,
		Roles:    r.ast.Auth.Roles,
	}
}

// ─── Slice helpers ────────────────────────────────────────────────────────────

func (r *Resolver) objectPtrs() []*spec.ResolvedObject {
	ptrs := make([]*spec.ResolvedObject, len(r.res.Objects))
	for i := range r.res.Objects {
		ptrs[i] = &r.res.Objects[i]
	}
	return ptrs
}

func (r *Resolver) interfacePtrs() []*spec.ResolvedInterface {
	ptrs := make([]*spec.ResolvedInterface, len(r.res.Interfaces))
	for i := range r.res.Interfaces {
		ptrs[i] = &r.res.Interfaces[i]
	}
	return ptrs
}

// Resolve is the package-level entry point used by the compiler pipeline.
func Resolve(ast *spec.SpecAST) *spec.ResolvedSpec {
	return NewResolver(ast).Resolve()
}
