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
	DBMongo    DBDriver = "mongo"

	FrameworkGin    Framework = "gin"
	FrameworkEcho   Framework = "echo"
	FrameworkSpring Framework = "spring"
)

// ─── TypeKind ────────────────────────────────────────────────────────────────

// TypeKind is the language-agnostic canonical kind of a field type.
// The ImportResolver maps Kind → import path. Generators map Kind → rendered type name.
type TypeKind int

const (
	TypeStr       TypeKind = iota
	TypeInt                // int64
	TypeUUID               // uuid.UUID
	TypeBool               // bool
	TypeDecimal            // decimal.Decimal
	TypeTimestamp          // time.Time
	TypeDate               // time.Time (date only)
	TypeJSON               // json.RawMessage
	TypeEnum               // string + values
	TypeCustom             // pointer to a generated struct
)

// TypeDescriptor is the language-agnostic canonical type representation.
// Resolvers populate it once; generators read GoType/JavaType to render.
type TypeDescriptor struct {
	Kind TypeKind

	// Rendered type names
	GoType   string // "string", "decimal.Decimal", "uuid.UUID", "*Money"
	JavaType string // "String", "BigDecimal", "UUID", "Money"

	// DB
	DBType     string // "VARCHAR(255)", "NUMERIC", "UUID", "JSONB"
	DBNullable bool

	// Nullability — GoPointer is true when nullable=true or Kind==TypeCustom
	GoPointer bool

	// Metadata
	IsCustom   bool
	IsEnum     bool
	EnumValues []string
}

// ─── ImportSet ────────────────────────────────────────────────────────────────

// ImportSet holds the sorted, deduplicated import paths for one target language.
// Populated by ImportResolver after each resolver level.
type ImportSet struct {
	Lang  Lang
	Paths []string
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
	StepInput                           // per-step typed input struct (GraphBased APIs)
	StepOutput                          // per-step typed output struct (GraphBased APIs)
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

	// Populated by ImportResolver in Level 1A
	Imports map[Lang]ImportSet
}

// ResolvedField is a single field inside a ResolvedObject.
type ResolvedField struct {
	Name     string // PascalCase: "Amount", "CreatedAt", "UserID"
	DBColumn string // snake_case: "amount", "created_at", "user_id"

	Type    TypeDescriptor
	TypeRef *ResolvedObject // non-nil when Type.IsCustom == true

	// Constraints
	Required bool
	Unique   bool
	Nullable bool
	Private  bool
	Default  interface{}
	Compute  bool

	// Enum values (only when Type.IsEnum == true)
	Values []string

	// Foreign key (only when this field is a FK)
	FK *ResolvedForeignKey

	// Pre-rendered struct tags — generator writes these verbatim
	GoStructTag    string // `json:"amount" validate:"required,min=0"`
	JavaAnnotation string // "@NotNull @DecimalMin(\"0\")"
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
	MapperInterface // per-APIAST: MapXxxInput per step + MapResponse
)

// ─── Level 2: ResolvedInterface ──────────────────────────────────────────────

// ResolvedInterface is the Level 2 output. Every Go interface the generators
// need to declare is represented as one ResolvedInterface.
type ResolvedInterface struct {
	Name string
	Path string
	Kind InterfaceKind

	// Ordered list of method signatures
	Functions []ResolvedFunction

	// Populated by ImportResolver in Level 2A
	Imports map[Lang]ImportSet
}

// ResolvedFunction is one method in an interface.
// Generators render the language-specific signature from Name, Params, and Returns.
// QueryKind is non-nil only for repository functions — tells the generator what GORM pattern to emit.
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
	// ServiceImpl dispatch kinds (reuse same struct, distinguish by presence of refs)
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

// ─── ResolvedTouch — unified ─────────────────────────────────────────────────

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
	KeyFunc      string // pre-computed: `fmt.Sprintf("user:%d", id)`
	DefaultTTL   time.Duration

	// ── HTTPCall (ExternalImpl) ───────────────────────────────────────────────
	HTTPMethod      string
	PathTemplate    string
	PathParams      []ResolvedParam
	RequestBodyRef  *ResolvedObject
	ResponseBodyRef *ResolvedObject
	StatusErrors    []ResolvedStatusError
	RetryAttempts   int
	RetryBackoff    string
	RetryOnStatus   []int
	AuthKind        string
	Timeout         time.Duration

	// ── TxStep (TransactionImpl) ─────────────────────────────────────────────
	StepName   string
	SQL        string
	StepParams []ResolvedParam

	// ── Publish (messaging) ──────────────────────────────────────────────────
	MessageName  string
	EventTypeRef *ResolvedObject

	// ── ServiceImpl enrichment ────────────────────────────────────────────────
	// Populated on touches inside ServiceImpl methods.
	// No flags — control flow is in the user's graph engine via When() conditions.
	ResultField string // context field name for this touch's output, e.g. "ChargePaymentOutput"
	FatalError  bool   // if true, a non-nil error halts the method

	// Resolved infra refs for ServiceImpl dispatch
	QueryRef           *ResolvedFunction       // the specific repo function to call
	ExternalRef        *ResolvedInterface      // the external client interface
	ExternalMethod     *ResolvedFunction       // the specific method on the external
	CacheMethod        *ResolvedFunction       // Get, Set, Delete, or Invalidate
	TransactionImplRef *ResolvedImplementation // pointer forward-declared below
}

type ResolvedStatusError struct {
	Status    int
	ErrorName string // "CardDeclined" → generates sentinel error
}

// ─── ExecutionModel ───────────────────────────────────────────────────────────

// ExecutionModel tells the generator which service body pattern to render.
type ExecutionModel int

const (
	Sequential ExecutionModel = iota // flat steps in order; renders flag/infra/hook pattern
	GraphBased                       // developer writes BuildGraph(); renders graph.Execute() body
)

// ─── ResolvedFieldMapping ────────────────────────────────────────────────────

// ResolvedFieldMapping is one field-to-source mapping produced by resolveFieldMappings.
// The Generator reads this to render the body of DefaultMapperImpl methods.
type ResolvedFieldMapping struct {
	MethodName   string // "MapValidateUserInput"
	TargetField  string // "UserID" — field on the StepInput or ResponseDTO
	SourcePath   string // "shared.Request.UserID" — where the value comes from
	Inferred     bool   // true = default implementation can handle this
	MustOverride bool   // true = no source found; default emits an error body
	Reason       string // why it can't be inferred (used in generated comment)
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
	DefaultMapperImpl // generated field-matching default for MapperInterface
	CacheMockImpl     // in-memory stub for CacheInterface
)

// ResolvedMethod is one method body inside a ResolvedImplementation.
type ResolvedMethod struct {
	FunctionName string

	// The infra this method touches, in declaration order.
	// Generator reads Kind on each touch to decide what ORM/HTTP code to render.
	Touches []ResolvedTouch

	// ServiceImpl only: the shared context type for this method.
	SharedContext *ResolvedObject

	// TransactionImpl only: pointer to the params object for the Execute() signature.
	InputParamsRef *ResolvedObject

	// ServiceImpl only: which execution body pattern to render.
	ExecutionModel ExecutionModel

	// ServiceImpl only: points to the MapperInterface for this API.
	// Nil when ExecutionModel == Sequential and no mapper is declared.
	MapperRef *ResolvedInterface
}

// ResolvedDependency describes one constructor parameter / struct field.
type ResolvedDependency struct {
	FieldName string // Go struct field name: "db", "userCache", "hooks"
	TypeName  string // Go type string: "*sql.DB", "*UserCache", "*PlaceOrderHooks"
	Import    string // import path if needed: "database/sql"
}

// ResolvedImplementation is the Level 3 output. Every concrete struct the
// generators need to write is represented as one ResolvedImplementation.
type ResolvedImplementation struct {
	Name string
	Path string
	Kind ImplementationKind

	// Which interface this satisfies
	Implements *ResolvedInterface

	// Constructor dependencies (struct fields + ctor params)
	Dependencies []ResolvedDependency

	// One entry per method
	Methods []ResolvedMethod

	// DefaultMapperImpl only: field-to-source mappings produced by resolveFieldMappings.
	// Generator reads these to render each MapXxxInput and MapResponse body.
	FieldMappings []ResolvedFieldMapping

	// Populated by ImportResolver in Level 3A
	Imports map[Lang]ImportSet
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
	GoType   string
	Required bool
	Default  interface{}
}

// ─── ResolvedSpec — the final output ─────────────────────────────────────────

// ResolvedSpec is what every Generator receives. Generators query the three
// slices by Kind to find exactly what they need.
type ResolvedSpec struct {
	// Metadata
	Project   string
	Module    string
	Lang      Lang
	Framework Framework
	DB        DBDriver

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
