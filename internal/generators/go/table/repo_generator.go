package table

import (
	"fmt"
	"strings"

	"stencil/internal/emitter"
	"stencil/internal/generator"
	"stencil/internal/spec"
	"stencil/internal/template"
)
// ─── Template data structures ────────────────────────────────────────────────

// RepoFilterClause is one WHERE clause element for find_by / exists / count.
type RepoFilterClause struct {
	Column string // DB column name: "email"
	Param  string // Go param name: "email"
}

// RepoFuncData describes one method on the repository.
type RepoFuncData struct {
	FuncName       string // "CreateUser", "GetUserByID"
	QueryKind      string // "create", "get", "update", etc.
	Params         string // pre-rendered param list: "ctx context.Context, data *User"
	Returns        string // pre-rendered return list: "(*User, error)"
	ModelName      string // "User"
	FilterClauses  []RepoFilterClause
	PaginationKind string // "cursor" | "offset" | ""
	OrderByColumn  string // "created_at"
	OrderDir       string // "ASC" | "DESC"
	DefaultLimit   int
	CustomSQL      string
	ReturnsMany    bool
	ReturnsSingle  bool // true when find_by with returns=single
}

// RepoData is the top-level payload passed to repo.go.tmpl.
type RepoData struct {
	Package       string         // snake_case table name: "users"
	ModelName     string         // PascalCase: "User"
	InterfaceName string         // "UserRepository"
	ImplName      string         // "UserRepositoryImpl"
	Module        string         // Go module path for imports
	DBTypeName    string         // Named DB wrapper type: "PostgresDB" (empty → fallback *gorm.DB)
	HasPagination bool           // true if any function uses paginate
	NotFoundError string         // var name of the NOT_FOUND error, e.g. "ErrCustomerNotFound"
	Functions     []RepoFuncData // one per method
}

// ─── Generator ───────────────────────────────────────────────────────────────

type repoGenerator struct {
	engine *template.Engine
}

func NewRepoGenerator(engine *template.Engine) generator.Generator {
	return &repoGenerator{engine: engine}
}

func (g *repoGenerator) ID() string { return "go.table.repo" }

func (g *repoGenerator) Generate(ctx generator.GeneratorContext) ([]emitter.File, error) {
	obj, ok := ctx.Payload.(*spec.ResolvedObject)
	if !ok {
		return nil, fmt.Errorf("repo generator: payload is not *spec.ResolvedObject")
	}
	if obj.Kind != spec.TableModel {
		return nil, fmt.Errorf("repo generator: object %s is not a TableModel", obj.Name)
	}

	// Look up the interface and implementation by naming convention.
	ifaceName := obj.Name + "Repository"
	implName := obj.Name + "RepositoryImpl"

	iface := ctx.Spec.InterfaceByName(ifaceName)
	if iface == nil {
		return nil, fmt.Errorf("repo generator: interface %s not found in spec", ifaceName)
	}

	impl := ctx.Spec.ImplByName(implName)
	if impl == nil {
		return nil, fmt.Errorf("repo generator: implementation %s not found in spec", implName)
	}

	data := buildRepoData(obj, iface, impl, ctx.Spec.Module, ctx.Lang)

	content, err := g.engine.Render("go/table/repo.go.tmpl", data)
	if err != nil {
		return nil, fmt.Errorf("repo generator: render failed for %s: %w", obj.Name, err)
	}

	return []emitter.File{
		{
			Path:    fmt.Sprintf("tables/%s/repository.go", obj.TableName),
			Content: content,
		},
	}, nil
}

// buildRepoData constructs the template data from resolved spec objects.
func buildRepoData(obj *spec.ResolvedObject, iface *spec.ResolvedInterface, impl *spec.ResolvedImplementation, module string, lp langPack) RepoData {
	data := RepoData{
		Package:       obj.TableName,
		ModelName:     obj.Name,
		InterfaceName: iface.Name,
		ImplName:      impl.Name,
		Module:        module,
	}

	// Set the named DB type if this repo has a resolved database.
	if impl.Database != nil {
		data.DBTypeName = impl.Database.TypeName
	}

	// Find the NOT_FOUND error.
	for _, e := range obj.Errors {
		if e.Code == "NOT_FOUND" {
			data.NotFoundError = e.VarName
			break
		}
	}

	// Build a method map from the implementation for touch metadata.
	touchByFunc := make(map[string]*spec.ResolvedTouch)
	for i := range impl.Methods {
		m := &impl.Methods[i]
		if len(m.Touches) > 0 {
			touchByFunc[m.FunctionName] = &m.Touches[0]
		}
	}

	for _, fn := range iface.Functions {
		fd := RepoFuncData{
			FuncName:  fn.Name,
			ModelName: obj.Name,
			Params:    renderParams(fn.Params, lp),
			Returns:   renderReturns(fn.Returns, lp),
		}

		// Determine QueryKind from the interface function or implementation touch.
		if fn.QueryKind != nil {
			fd.QueryKind = queryKindString(*fn.QueryKind)
		}

		// Enrich from the implementation touch if available.
		if touch, ok := touchByFunc[fn.Name]; ok {
			if fd.QueryKind == "" {
				fd.QueryKind = queryKindString(touch.QueryKind)
			}

			// Filter clauses for find_by, exists, count.
			for _, ff := range touch.FilterFields {
				fd.FilterClauses = append(fd.FilterClauses, RepoFilterClause{
					Column: ff.DBColumn,
					Param:  ff.DBColumn,
				})
			}

			fd.PaginationKind = touch.PaginationKind
			if touch.OrderByField != nil {
				fd.OrderByColumn = touch.OrderByField.DBColumn
			}
			fd.OrderDir = touch.OrderDir
			fd.DefaultLimit = touch.DefaultLimit
			fd.CustomSQL = touch.CustomSQL
			fd.ReturnsMany = touch.ReturnsMany

			// For find_by, set ReturnsSingle when returns=single.
			if fd.QueryKind == "find_by" {
				fd.ReturnsSingle = !touch.ReturnsMany
			}
		}

		// Override returns for paginate functions to use pagination library types.
		if fd.QueryKind == "paginate" {
			data.HasPagination = true
			if fd.PaginationKind == "offset" {
				fd.Returns = fmt.Sprintf("(*pagination.OffsetPage[%s], error)", data.ModelName)
			} else {
				fd.Returns = fmt.Sprintf("(*pagination.CursorPage[%s], error)", data.ModelName)
			}
		}

		data.Functions = append(data.Functions, fd)
	}

	return data
}

// renderParams converts a slice of ResolvedParam into a Go function parameter string.
// Always prepends "ctx context.Context".
func renderParams(params []spec.ResolvedParam, lp langPack) string {
	parts := []string{"ctx context.Context"}
	for _, p := range params {
		parts = append(parts, fmt.Sprintf("%s %s", p.Name, lp.TypeRef(p.Type).Name))
	}
	return strings.Join(parts, ", ")
}

// renderReturns converts a slice of ResolvedReturn into a Go return type string.
// Always appends "error".
func renderReturns(returns []spec.ResolvedReturn, lp langPack) string {
	if len(returns) == 0 {
		return "error"
	}

	var parts []string
	for _, r := range returns {
		parts = append(parts, lp.TypeRef(r.Type).Name)
	}
	parts = append(parts, "error")

	if len(parts) == 1 {
		return parts[0]
	}
	return "(" + strings.Join(parts, ", ") + ")"
}

// queryKindString maps a QueryKind constant to the template string.
func queryKindString(qk spec.QueryKind) string {
	switch qk {
	case spec.QueryCreate:
		return "create"
	case spec.QueryGet:
		return "get"
	case spec.QueryUpdate:
		return "update"
	case spec.QueryDelete:
		return "delete"
	case spec.QueryFindBy:
		return "find_by"
	case spec.QueryExists:
		return "exists"
	case spec.QueryCount:
		return "count"
	case spec.QueryPaginate:
		return "paginate"
	case spec.QuerySoftDelete:
		return "soft_delete"
	case spec.QueryBulkCreate:
		return "bulk_create"
	case spec.QueryCustom:
		return "custom"
	default:
		return "unknown"
	}
}
