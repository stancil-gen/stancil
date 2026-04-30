package spec

import "time"

// ─── Lang / DB / Framework constants ─────────────────────────────────────────

type Lang string
type DBDriver string
type Framework string

const (
	LangGo   Lang = "go"
	LangJava Lang = "java"

	DBPostgres DBDriver = "postgres"
	DBMySQL    DBDriver = "mysql"
	DBSQLite   DBDriver = "sqlite"
	DBMongo    DBDriver = "mongo"

	FrameworkGin    Framework = "gin"
	FrameworkEcho   Framework = "echo"
	FrameworkSpring Framework = "spring"
)

// ─── TypeKind ────────────────────────────────────────────────────────────────

// TypeKind is the language-agnostic canonical kind of a field type.
// LangPack maps Kind → rendered type name. Generators read Kind directly.
type TypeKind int

const (
	TypeStr       TypeKind = iota
	TypeInt                // int64 in Go, long in Java
	TypeUUID               // uuid.UUID in Go, UUID in Java
	TypeBool               // bool
	TypeDecimal            // decimal.Decimal in Go, BigDecimal in Java
	TypeTimestamp          // time.Time in Go, LocalDateTime in Java
	TypeDate               // time.Time (date only) in Go
	TypeJSON               // json.RawMessage in Go, JsonNode in Java
	TypeEnum               // string + values — enum type
	TypeCustom             // pointer to a generated struct (Money, Address, etc.)
	TypeAny                // interface{} in Go — fallback for truly dynamic types
)

// TypeDescriptor is the language-agnostic canonical type representation.
// No language strings — LangPack renders Kind+Nullable+CustomName at generator time.
type TypeDescriptor struct {
	Kind TypeKind

	// Nullability — language-agnostic. LangPack decides how to render (pointer, Option<T>, | null).
	Nullable bool

	// IsList — true when this field holds a slice/array of the base type.
	// LangPack renders as []*T (Go), T[] (TypeScript), List<T> (Java).
	IsList bool

	// DB type — database-specific, not language-specific.
	DBType string // "UUID", "VARCHAR(255)", "NUMERIC", "JSONB", "TIMESTAMP", "BOOLEAN"

	// Metadata for enum types.
	IsEnum     bool
	EnumValues []string

	// CustomName is the type name when IsCustom is true.
	// Example: "Money", "Address". Generators qualify with the correct package.
	IsCustom   bool
	CustomName string
}

// ─── ObjectKind ──────────────────────────────────────────────────────────────

type ObjectKind int

const (
	TypeObject        ObjectKind = iota // from types: block
	TableModel                          // from tables: block
	RequestDTO                          // from api.dtos.request
	ResponseDTO                         // from api.dtos.response
	SharedContext                       // per-API execution context
	ExternalInput                       // from externals[*].calls[*].body
	ExternalOutput                      // from externals[*].calls[*].response
	TransactionParams                   // merged params across tx steps
	StepInput                           // per-step typed input struct (future)
	StepOutput                          // per-step typed output struct (future)
)

// ─── Level 1: ResolvedObject ─────────────────────────────────────────────────

// ResolvedObject is the Level 1 output. Every struct the generators need to
// declare is represented as one ResolvedObject.
type ResolvedObject struct {
	// Identity
	Name string // PascalCase
	Path string // generated file path, e.g. "generated/repo/users/model.go"
	Kind ObjectKind

	// Fields in declaration order
	Fields []ResolvedField

	// Validation rules per field (key: PascalCase field name)
	Rules map[string][]ResolvedRule

	// DB metadata — only populated when Kind == TableModel
	TableName  string // snake_case: "users"
	PrimaryKey string // field Name of the PK: "ID"
	SoftDelete bool
	BulkCreate bool
	Indexes    []ResolvedIndex

	// Table errors — only populated when Kind == TableModel
	Errors []ResolvedError
}

// ResolvedError is a domain error declared on a table.
type ResolvedError struct {
	Code    string // "NOT_FOUND" — from YAML
	Name    string // "NotFound" — from YAML
	VarName string // "ErrCustomerNotFound" — derived
	Message string // "customer: not found" — derived
}

// ResolvedField is a single field inside a ResolvedObject.
type ResolvedField struct {
	Name     string // PascalCase: "Amount", "CreatedAt", "UserID"
	DBColumn string // snake_case: "amount", "created_at", "user_id"

	Type    TypeDescriptor
	TypeRef *ResolvedObject // non-nil when Type.IsCustom == true (linked by linkTypeRefs)

	// Constraints — language-agnostic. LangPack renders these as struct tags / decorators.
	Required bool
	Unique   bool
	Nullable bool
	Private  bool
	Default  interface{}
	Compute  bool

	// Validation rules — used by LangPack to generate field annotations.
	Rules []ResolvedRule

	// Enum values (only when Type.IsEnum == true)
	Values []string

	// Foreign key (only when this field is a FK)
	FK *ResolvedForeignKey
}

type ResolvedForeignKey struct {
	TableRef  *ResolvedObject // the TableModel being referenced
	FieldName string          // "ID"
}

type ResolvedRule struct {
	Type  string      // "min_length", "max_length", "email", "min", "max", "regex"
	Param interface{} // 3, "^[a-z]+$", etc.
}

type ResolvedIndex struct {
	Fields []string
	Unique bool
	Name   string // "idx_users_email"
}

// ─── InterfaceKind ────────────────────────────────────────────────────────────

type InterfaceKind int

const (
	RepositoryInterface InterfaceKind = iota
	HookInterface
	ServiceInterface
	CacheInterface
	ExternalInterface
	MapperInterface // per-API: MapXxxInput per step + MapResponse
)

// ─── Level 2: ResolvedInterface ──────────────────────────────────────────────

// ResolvedInterface is the Level 2 output. Every interface the generators
// need to declare is represented as one ResolvedInterface.
type ResolvedInterface struct {
	Name string
	Path string
	Kind InterfaceKind

	// Ordered list of method signatures
	Functions []ResolvedFunction
}

// ResolvedFunction is one method in an interface.
// QueryKind is non-nil only for repository functions — tells the generator which DB pattern to emit.
type ResolvedFunction struct {
	Name      string
	Params    []ResolvedParam
	Returns   []ResolvedReturn
	QueryKind *QueryKind // nil for hooks, mappers, service, external; set for repo functions
}

// ResolvedParam is one parameter in a function signature.
type ResolvedParam struct {
	Name    string
	Type    TypeDescriptor
	TypeRef *ResolvedObject // non-nil when Type.IsCustom == true
}

// ResolvedReturn is one return value in a function signature.
type ResolvedReturn struct {
	Type    TypeDescriptor
	TypeRef *ResolvedObject
}

// ─── TouchKind ───────────────────────────────────────────────────────────────

type TouchKind int

const (
	TouchKindQuery    TouchKind = iota // RepositoryImpl — DB query
	TouchKindCacheOp                   // CacheImpl — Redis operation
	TouchKindHTTPCall                  // ExternalImpl — HTTP call
	TouchKindTxStep                    // TransactionImpl — one SQL step
	TouchKindPublish                   // ServiceImpl — message publish
	// ServiceImpl dispatch kinds
	TouchKindTable       // ServiceImpl touches a table via repo
	TouchKindCache       // ServiceImpl touches a cache interface
	TouchKindExternal    // ServiceImpl touches an external interface
	TouchKindTransaction // ServiceImpl touches a transaction
	TouchKindMessage     // ServiceImpl publishes a message
)

type QueryKind int

const (
	QueryCreate     QueryKind = iota
	QueryGet                  // get by PK
	QueryUpdate               // update by PK
	QueryDelete               // delete by PK
	QueryFindBy               // find_by: [fields]
	QueryExists               // exists: [fields]
	QueryCount                // count: [fields]
	QueryPaginate             // paginate: cursor|offset
	QuerySoftDelete           // soft_delete: true
	QueryBulkCreate           // bulk_create: true
	QueryCustom               // custom: name
)

type CacheOpKind int

const (
	CacheOpGet CacheOpKind = iota
	CacheOpSet
	CacheOpDelete
	CacheOpInvalidate
)

// ─── ResolvedTouch — unified infra interaction ────────────────────────────────

// ResolvedTouch represents one infra interaction. The Kind field determines
// which set of fields is populated. All other fields are zero values.
type ResolvedTouch struct {
	Kind TouchKind

	// ── Query (RepositoryImpl) ────────────────────────────────────────────────
	TableRef     *ResolvedObject
	QueryKind    QueryKind
	FilterFields []ResolvedField // for FindBy/Exists/Count
	Op           string          // "eq" | "gte" | "lte" | "like" | "in"

	// Pagination
	PaginationKind string // "cursor" | "offset"
	OrderByField   *ResolvedField
	OrderDir       string
	DefaultLimit   int

	// Custom query
	CustomSQL    string
	CustomParams []ResolvedParam
	ReturnsMany  bool

	// Error condition
	ErrorIfRows *int
	ErrorName   string

	// RETURNING clause scan target
	ScanInto *ResolvedParam

	// ── CacheOp (CacheImpl) ──────────────────────────────────────────────────
	CacheRef     *ResolvedInterface
	CacheOp      CacheOpKind
	ValueTypeRef *ResolvedObject
	KeyParams    []ResolvedParam
	KeyTemplate  string
	KeyFunc      string
	DefaultTTL   time.Duration

	// ── HTTPCall (ExternalImpl) ───────────────────────────────────────────────
	HTTPMethod      string
	PathTemplate    string
	PathParams      []string        // extracted {placeholder} names from PathTemplate
	QueryParamFields []ResolvedField // query params from external call spec
	DynamicHeaders  []ResolvedField // per-request headers (Type != "")
	StaticHeaders   map[string]string // fixed headers (Value != "")
	RequestBodyRef  *ResolvedObject
	ResponseBodyRef *ResolvedObject
	StatusErrors    []ResolvedStatusError
	RetryAttempts   int
	RetryBackoff    string
	RetryOnStatus   []int
	AuthKind        string
	AuthConfigField string // config field name for auth token, e.g. "StripeSecretKey"
	BaseURLField    string // config field name for base URL, e.g. "StripeUrl"
	Timeout         time.Duration

	// ── TxStep (TransactionImpl) ─────────────────────────────────────────────
	StepName   string
	SQL        string
	StepParams []ResolvedParam

	// ── Publish (messaging) ──────────────────────────────────────────────────
	MessageName  string
	EventTypeRef *ResolvedObject

	// ── ServiceImpl enrichment ────────────────────────────────────────────────
	StepID      string // raw step ID from YAML, e.g. "validateCustomer"
	ResultField string // context field name for this touch's output
	FatalError  bool   // if true, a non-nil error halts the method

	// Resolved infra refs for ServiceImpl dispatch
	QueryRef           *ResolvedFunction
	ExternalRef        *ResolvedInterface
	ExternalMethod     *ResolvedFunction
	CacheMethod        *ResolvedFunction
	TransactionImplRef *ResolvedImplementation
}

type ResolvedStatusError struct {
	Status    int
	ErrorName string
}

// ─── ExecutionModel ───────────────────────────────────────────────────────────

type ExecutionModel int

const (
	Sequential ExecutionModel = iota
	GraphBased
)

// ─── ResolvedFieldMapping ────────────────────────────────────────────────────

type ResolvedFieldMapping struct {
	MethodName   string
	TargetField  string
	SourcePath   string
	Inferred     bool
	MustOverride bool
	Reason       string
}

// ─── Level 3: ResolvedImplementation ─────────────────────────────────────────

type ImplementationKind int

const (
	RepositoryImpl ImplementationKind = iota
	ServiceImpl
	TransactionImpl
	CacheImpl
	ExternalImpl
	ExternalMockImpl
	DefaultMapperImpl
	CacheMockImpl
)

// ResolvedMethod is one method body inside a ResolvedImplementation.
type ResolvedMethod struct {
	FunctionName string

	Touches []ResolvedTouch

	// ServiceImpl only
	SharedContext  *ResolvedObject
	ExecutionModel ExecutionModel
	MapperRef      *ResolvedInterface
	HTTPMethod     string
	HTTPPath       string

	// TransactionImpl only
	InputParamsRef *ResolvedObject
}

// ResolvedDependency describes one constructor parameter / struct field.
type ResolvedDependency struct {
	FieldName string // struct field name: "db", "userCache", "hooks"
	TypeName  string // type string (still language-specific here — generators use directly)
	Import    string // import path if needed
}

// ResolvedImplementation is the Level 3 output.
type ResolvedImplementation struct {
	Name string
	Path string
	Kind ImplementationKind

	Implements *ResolvedInterface

	Dependencies []ResolvedDependency

	// RepositoryImpl only: which database this repo connects to
	Database *ResolvedDatabase

	// ServiceImpl only
	BasePath string

	Methods []ResolvedMethod

	// DefaultMapperImpl only
	FieldMappings []ResolvedFieldMapping
}

// ─── Subsystems ──────────────────────────────────────────────────────────────

type ResolvedMessaging struct {
	Broker    string
	Brokers   []string
	Producers []ResolvedProducer
	Consumers []ResolvedConsumer
}

type ResolvedProducer struct {
	Topic string
	Event string
	Key   string
}

type ResolvedConsumer struct {
	Topic   string
	Event   string
	Group   string
	Handler string
}

type ResolvedAuth struct {
	Provider string
	Expiry   string
	Roles    []string
}

type ResolvedConfigVar struct {
	Name     string
	YAMLType string // "str", "int", "bool" — LangPack renders to language type
	Required bool
	Default  interface{}
}

// ─── ResolvedDatabase ─────────────────────────────────────────────────────────

// ResolvedDatabase is one resolved entry from the spec's `db:` block.
type ResolvedDatabase struct {
	Name      string   // identifier: "postgres", "primary", "analytics"
	Driver    DBDriver // DBPostgres | DBMySQL | DBMongo
	Framework string   // "gorm" | "sqlx" | "sqlc"
	URLField  string   // PascalCase config field name, e.g. "DatabaseUrl"
	TypeName  string   // Go named wrapper type: e.g. "PostgresDB" = Pascal(Name)+"DB"
	FuncName  string   // MustOpen function suffix: e.g. "Postgres" → MustOpenPostgres
}

// ─── ResolvedSpec — the final output ─────────────────────────────────────────

// ResolvedSpec is what every Generator receives. Generators query the three
// slices by Kind to find exactly what they need.
type ResolvedSpec struct {
	// Metadata
	Project      string
	Module       string
	Lang         Lang
	Framework    Framework
	Databases    []ResolvedDatabase // declared DB connections (from db: block)
	ConfigLoader string // "env" | "viper-yaml" | "viper-json"

	// Level 1 output
	Objects []ResolvedObject

	// Level 2 output
	Interfaces []ResolvedInterface

	// Level 3 output
	Implementations []ResolvedImplementation

	// Subsystems
	Messaging *ResolvedMessaging
	Auth      *ResolvedAuth
	Config    []ResolvedConfigVar
}

// ─── Query helpers ────────────────────────────────────────────────────────────

func (s *ResolvedSpec) ObjectsOfKind(k ObjectKind) []*ResolvedObject {
	var out []*ResolvedObject
	for i := range s.Objects {
		if s.Objects[i].Kind == k {
			out = append(out, &s.Objects[i])
		}
	}
	return out
}

func (s *ResolvedSpec) ObjectByName(name string) *ResolvedObject {
	for i := range s.Objects {
		if s.Objects[i].Name == name {
			return &s.Objects[i]
		}
	}
	return nil
}

func (s *ResolvedSpec) InterfacesOfKind(k InterfaceKind) []*ResolvedInterface {
	var out []*ResolvedInterface
	for i := range s.Interfaces {
		if s.Interfaces[i].Kind == k {
			out = append(out, &s.Interfaces[i])
		}
	}
	return out
}

func (s *ResolvedSpec) InterfaceByName(name string) *ResolvedInterface {
	for i := range s.Interfaces {
		if s.Interfaces[i].Name == name {
			return &s.Interfaces[i]
		}
	}
	return nil
}

func (s *ResolvedSpec) ImplsOfKind(k ImplementationKind) []*ResolvedImplementation {
	var out []*ResolvedImplementation
	for i := range s.Implementations {
		if s.Implementations[i].Kind == k {
			out = append(out, &s.Implementations[i])
		}
	}
	return out
}

func (s *ResolvedSpec) ImplByName(name string) *ResolvedImplementation {
	for i := range s.Implementations {
		if s.Implementations[i].Name == name {
			return &s.Implementations[i]
		}
	}
	return nil
}
