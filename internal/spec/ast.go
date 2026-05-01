package spec

// SpecAST is the raw output of the parser. It directly mirrors the YAML structure.
// No defaults are filled, and no inferences are made at this stage. All enums are strings.
type SpecAST struct {
	Version       int                `yaml:"version"`
	Project       string             `yaml:"project"`
	Lang          string             `yaml:"lang"`
	Framework     string             `yaml:"framework"`
	Databases     []DatabaseAST      `yaml:"db"`       // replaces top-level db: string
	ConfigLoader  string             `yaml:"config_loader"`
	Config        []ConfigVarAST     `yaml:"config"`
	Tables        []TableAST         `yaml:"tables"`
	Types         []CustomTypeAST    `yaml:"types"`
	Transactions  []TransactionAST   `yaml:"transactions"`
	Externals     []ExternalAST      `yaml:"externals"`
	Messaging     *MessagingAST      `yaml:"messaging"`
	Cache         *CacheAST          `yaml:"cache"`
	Storage       *StorageAST        `yaml:"storage"`
	Auth          *AuthAST           `yaml:"auth"`
	Observability *ObservabilityAST  `yaml:"observability"`
	Middleware    []string           `yaml:"middleware"`
	Resources     []ResourceGroupAST `yaml:"resources"` // API groups
	Extensions    []ExtensionAST     `yaml:"extensions"`
	Overrides     *OverridesAST      `yaml:"overrides"`
}

type ConfigVarAST struct {
	Name        string
	Type        string
	Required    bool
	Default     interface{}
	MinLength   *int
	Min         *int
	Max         *int
	Values      []string
	Description string
}

// ── Core Infrastructure ─────────────────────────────────────────

// DatabaseAST declares one database connection in the `db:` block.
type DatabaseAST struct {
	Name      string `yaml:"name"`      // identifier used by tables, e.g. "postgres", "primary"
	Driver    string `yaml:"driver"`    // "postgres" | "mysql" | "mongo" — defaults to Name
	Framework string `yaml:"framework"` // "gorm" | "sqlx" | "sqlc" — defaults to "gorm"
	URL       string `yaml:"url"`       // config var ref, e.g. "${DATABASE_URL}"
}

type TableAST struct {
	Name        string           `yaml:"name"`
	DB          string           `yaml:"db"`
	IDType      string           `yaml:"id_type"` // "uuid" (default) | "auto_increment"
	Fields      []FieldAST       `yaml:"fields"`
	Queries     []QueryAST       `yaml:"queries"`
	SoftDelete  bool             `yaml:"soft_delete"`
	BulkCreate  bool             `yaml:"bulk_create"`
	States      *StateMachineAST `yaml:"states"`
	DTOs        *TableDTOAST     `yaml:"dtos"`
	Errors      []TableErrorAST  `yaml:"errors"`
}

type TableErrorAST struct {
	Code string `yaml:"code"` // "NOT_FOUND"
	Name string `yaml:"name"` // "NotFound"
}

type FieldAST struct {
	Name     string      `yaml:"name"`
	Type     string      `yaml:"type"`
	Required bool        `yaml:"required"`
	Unique   bool        `yaml:"unique"`
	Nullable bool        `yaml:"nullable"`
	Private  bool        `yaml:"private"`
	Default  interface{} `yaml:"default"`
	Values   []string    `yaml:"values"`
	Rules    []RuleAST   `yaml:"rules"`
}

type RuleAST struct {
	Type  string
	Value interface{}
}

type QueryAST struct {
	FindBy       []string            `yaml:"find_by"`
	Exists       []string            `yaml:"exists"`
	Count        []string            `yaml:"count"`
	Sum          []string            `yaml:"sum"`
	Op           string              `yaml:"op"`
	Paginate     interface{}         `yaml:"paginate"`
	OrderBy      []map[string]string `yaml:"order_by"`
	DefaultLimit int                 `yaml:"default_limit"`
	SoftDelete   bool                `yaml:"soft_delete"`
	BulkCreate   bool                `yaml:"bulk_create"`
	Custom       string              `yaml:"custom"`
	Returns      string              `yaml:"returns"`
	SQL          string              `yaml:"sql"`
	Params       []ParamAST          `yaml:"params"`
}

type StateMachineAST struct {
	Field       string
	Transitions []StateTransitionAST
}

type StateTransitionAST struct {
	From string
	To   string
}

type TableDTOAST struct {
	Model string
}

type CustomTypeAST struct {
	Name   string     `yaml:"name"`
	Fields []FieldAST `yaml:"fields"`
}

type TransactionAST struct {
	Name  string                `yaml:"name"`
	Type  string                `yaml:"type"`
	Steps []TransactionStepAST  `yaml:"steps"`
}

type TransactionStepAST struct {
	Name        string      `yaml:"name"`
	SQL         string      `yaml:"sql"`
	Params      []ParamAST  `yaml:"params"`
	ErrorIfRows *int        `yaml:"error_if_rows"`
	Error       string      `yaml:"error"`
}

type ParamAST struct {
	Name string `yaml:"name"`
	Type string `yaml:"type"`
}

type ExternalAST struct {
	Name      string              `yaml:"name"`
	Type      string              `yaml:"type"`
	BaseURL   string              `yaml:"base_url"`
	Auth      string              `yaml:"auth"`
	AuthToken string              `yaml:"auth_token"` // e.g. "${STRIPE_SECRET_KEY}" — config var for bearer/api_key auth
	Timeout   string              `yaml:"timeout"`
	Retry     *RetryAST           `yaml:"retry"`
	Headers   []ExternalHeaderAST `yaml:"headers"`
	Calls     []ExternalCallAST   `yaml:"calls"`
}

type RetryAST struct {
	Attempts int    `yaml:"attempts"`
	Backoff  string `yaml:"backoff"`
	OnStatus []int  `yaml:"on_status"`
	DLQ      string `yaml:"dlq"`
}

type ExternalCallAST struct {
	Name        string              `yaml:"name"`
	Method      string              `yaml:"method"`
	Path        string              `yaml:"path"`
	QueryParams []FieldAST          `yaml:"query_params"`
	Headers     []ExternalHeaderAST `yaml:"headers"`
	Body        *ExternalBodyAST    `yaml:"body"`
	Response    *ExternalBodyAST    `yaml:"response"`
	Errors      []ExternalErrorAST  `yaml:"errors"`
}

type ExternalHeaderAST struct {
	Name     string `yaml:"name"`
	Type     string `yaml:"type"`
	Value    string `yaml:"value"`
	Required bool   `yaml:"required"`
}

// ExternalBodyAST defines a request or response body inline in the external call.
// Fields are declared directly here instead of referencing global types.
type ExternalBodyAST struct {
	Name   string     `yaml:"name"`
	Fields []FieldAST `yaml:"fields"`
}

type ExternalErrorAST struct {
	Status int    `yaml:"status"`
	Error  string `yaml:"error"`
}

// Subsystems we loosely map for now to allow JSON unmarshaling later
type MessagingAST struct {
	Broker    string        `yaml:"broker"`
	Brokers   []string      `yaml:"brokers"`
	Producers []ProducerAST `yaml:"producers"`
	Consumers []ConsumerAST `yaml:"consumers"`
}

type ProducerAST struct {
	Topic string `yaml:"topic"`
	Event string `yaml:"event"`
	Key   string `yaml:"key"`
}

type ConsumerAST struct {
	Topic   string    `yaml:"topic"`
	Event   string    `yaml:"event"`
	Group   string    `yaml:"group"`
	Handler string    `yaml:"handler"`
	Retry   *RetryAST `yaml:"retry"`
}

type CacheAST struct {
	Provider   string              `yaml:"provider"`
	URL        string              `yaml:"url"`
	Prefix     string              `yaml:"prefix"`
	Interfaces []CacheInterfaceAST `yaml:"interfaces"`
}

type CacheInterfaceAST struct {
	Name        string   `yaml:"name"`
	KeyTemplate string   `yaml:"key_template"`
	ValueType   string   `yaml:"value_type"`
	DefaultTTL  string   `yaml:"default_ttl"`
	Methods     []string `yaml:"methods"`
}

type StorageAST struct {
	Provider string `yaml:"provider"`
	Bucket   string `yaml:"bucket"`
}

type AuthAST struct {
	Provider    string          `yaml:"provider"`
	Secret      string          `yaml:"secret"`
	Expiry      string          `yaml:"expiry"`
	Roles       []string        `yaml:"roles"`
	Permissions []PermissionAST `yaml:"permissions"`
}

type PermissionAST struct {
	Role string `yaml:"role"`
	Can  []string `yaml:"can"`
	On   string `yaml:"on"`
}

type ObservabilityAST struct {
	Tracing string `yaml:"tracing"`
	Metrics string `yaml:"metrics"`
}

type ExtensionAST map[string]interface{}
type OverridesAST map[string]interface{}

// ── API Layer ───────────────────────────────────────────────────

type ResourceGroupAST struct {
	Group    string    `yaml:"group"`
	BasePath string    `yaml:"base_path"`
	Auth     string    `yaml:"auth"`
	APIs     []APIAST  `yaml:"apis"`
}

type APIAST struct {
	Name     string       `yaml:"name"`
	Method   string       `yaml:"method"`
	Path     string       `yaml:"path"`
	Status   int          `yaml:"status"` // HTTP success status code (e.g. 201, 200, 204)
	Auth     string       `yaml:"auth"`
	Roles    []string     `yaml:"roles"`
	Owner    bool         `yaml:"owner"`
	Request  string       `yaml:"request"`
	Response string       `yaml:"response"`
	DTOs     *DTOBlockAST `yaml:"dtos"`
	Steps    []StepAST    `yaml:"steps"`
}

type DTOBlockAST struct {
	Request  *DTOAST `yaml:"request"`
	Response *DTOAST `yaml:"response"`
}

func (b *DTOBlockAST) Find(name string) *DTOAST {
	if b == nil {
		return nil
	}
	if b.Request != nil && b.Request.Name == name {
		return b.Request
	}
	if b.Response != nil && b.Response.Name == name {
		return b.Response
	}
	return nil
}

type StepAST struct {
	ID    string   `yaml:"id"`
	Touch TouchAST `yaml:"touch"`
	Fatal *bool    `yaml:"fatal"`
}

type DTOAST struct {
	Name   string        `yaml:"name"`
	Fields []DTOFieldAST `yaml:"fields"`
}

type DTOFieldAST struct {
	Name     string    `yaml:"name"`
	Type     string    `yaml:"type"`
	Required bool      `yaml:"required"`
	Compute  bool      `yaml:"compute"`
	Private  bool      `yaml:"private"`
	Rules    []RuleAST `yaml:"rules"`
}

type TouchAST struct {
	Table       string `yaml:"table"`
	External    string `yaml:"external"`
	Message     string `yaml:"message"`
	Cache       string `yaml:"cache"`
	Transaction string `yaml:"transaction"`
	Storage     string `yaml:"storage"`

	Op     string `yaml:"op"`
	Method string `yaml:"method"`
}
