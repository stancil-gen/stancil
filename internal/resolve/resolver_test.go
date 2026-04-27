package resolve_test

import (
	"testing"

	"stencil/internal/spec"
	parser "stencil/internal/parse"
	resolver "stencil/internal/resolve"
)

// loadSuite parses and resolves the full resolver_suite.yaml fixture.
// All tests share this one resolved spec.
func loadSuite(t *testing.T) *spec.ResolvedSpec {
	t.Helper()
	ast, err := parser.ParseFile("../../testdata/resolver_suite.yaml")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return resolver.Resolve(ast)
}

// ─── Level 1: Objects ─────────────────────────────────────────────────────────

func TestLevel1_TypeObject_Count(t *testing.T) {
	s := loadSuite(t)
	types := s.ObjectsOfKind(spec.TypeObject)
	if len(types) != 1 {
		t.Errorf("want 1 TypeObject (Money), got %d", len(types))
	}
	if types[0].Name != "Money" {
		t.Errorf("want Money, got %s", types[0].Name)
	}
}

func TestLevel1_TypeObject_Fields(t *testing.T) {
	s := loadSuite(t)
	money := s.ObjectByName("Money")
	if money == nil {
		t.Fatal("Money TypeObject not found")
	}
	if len(money.Fields) != 2 {
		t.Errorf("want 2 fields on Money, got %d", len(money.Fields))
	}
	if money.Fields[0].Name != "Amount" {
		t.Errorf("want Amount, got %s", money.Fields[0].Name)
	}
	if money.Fields[0].Type.Kind != spec.TypeDecimal {
		t.Errorf("want TypeDecimal for amount, got %v", money.Fields[0].Type.Kind)
	}
}

func TestLevel1_TableModel_Count(t *testing.T) {
	s := loadSuite(t)
	tables := s.ObjectsOfKind(spec.TableModel)
	if len(tables) != 2 {
		t.Errorf("want 2 TableModels (users, orders), got %d", len(tables))
	}
}

func TestLevel1_TableModel_InjectedFields(t *testing.T) {
	s := loadSuite(t)
	users := s.ObjectByName("User")
	if users == nil {
		t.Fatal("User TableModel not found")
	}

	fieldNames := make(map[string]bool)
	for _, f := range users.Fields {
		fieldNames[f.Name] = true
	}

	required := []string{"ID", "CreatedAt", "UpdatedAt", "DeletedAt"}
	for _, req := range required {
		if !fieldNames[req] {
			t.Errorf("missing injected field %s on User", req)
		}
	}
}

func TestLevel1_TableModel_SoftDelete(t *testing.T) {
	s := loadSuite(t)
	users := s.ObjectByName("User")
	if users == nil {
		t.Fatal("User TableModel not found")
	}
	if !users.SoftDelete {
		t.Error("want SoftDelete=true on users table")
	}

	for _, f := range users.Fields {
		if f.Name == "DeletedAt" {
			if !f.Nullable {
				t.Error("DeletedAt should be nullable")
			}
			return
		}
	}
	t.Error("DeletedAt field not found")
}

func TestLevel1_TableModel_PrivateField(t *testing.T) {
	s := loadSuite(t)
	users := s.ObjectByName("User")
	if users == nil {
		t.Fatal("User TableModel not found")
	}
	for _, f := range users.Fields {
		if f.Name == "PasswordHash" {
			if !f.Private {
				t.Error("PasswordHash should be Private=true")
			}
			return
		}
	}
	t.Error("PasswordHash field not found on User")
}

// Custom types embedded in a TableModel must be raw values (no *), not pointers.
// They are stored as JSONB and Go's zero-value initialisation is needed.
func TestLevel1_TableModel_CustomTypeRaw(t *testing.T) {
	s := loadSuite(t)
	order := s.ObjectByName("Order")
	if order == nil {
		t.Fatal("Order TableModel not found")
	}
	for _, f := range order.Fields {
		if f.Name == "Total" {
			if !f.Type.IsCustom {
				t.Errorf("Total should be a custom type (IsCustom=true)")
			}
			if f.Type.CustomName != "Money" {
				t.Errorf("Total CustomName = %q, want \"Money\"", f.Type.CustomName)
			}
			if f.Type.Nullable {
				t.Errorf("Total (custom type Money) should not be nullable")
			}
			return
		}
	}
	t.Error("Total field not found on Order")
}

func TestLevel1_TypeRef_Linking(t *testing.T) {
	s := loadSuite(t)
	order := s.ObjectByName("Order")
	if order == nil {
		t.Fatal("Order TableModel not found")
	}
	for _, f := range order.Fields {
		if f.Name == "Total" {
			if f.TypeRef == nil {
				t.Error("orders.total.TypeRef is nil — linkTypeRefs did not run")
				return
			}
			if f.TypeRef.Name != "Money" {
				t.Errorf("orders.total.TypeRef.Name = %q, want Money", f.TypeRef.Name)
			}
			return
		}
	}
	t.Error("Total field not found on Order")
}

func TestLevel1_SharedContext_CreateUser(t *testing.T) {
	s := loadSuite(t)
	ctx := s.ObjectByName("CreateUserContext")
	if ctx == nil {
		t.Fatal("CreateUserContext SharedContext not found")
	}
	if ctx.Kind != spec.SharedContext {
		t.Errorf("wrong Kind on CreateUserContext: %v", ctx.Kind)
	}

	fieldNames := make(map[string]bool)
	for _, f := range ctx.Fields {
		fieldNames[f.Name] = true
	}

	if !fieldNames["Request"] {
		t.Error("SharedContext missing Request field")
	}
	if !fieldNames["Response"] {
		t.Error("SharedContext missing Response field")
	}
	// writeUser step touches table users (create) → produces an output field
	if !fieldNames["WriteUserOutput"] {
		t.Error("SharedContext missing WriteUserOutput field (table create step)")
	}
}

func TestLevel1_RequestDTO(t *testing.T) {
	s := loadSuite(t)
	req := s.ObjectByName("CreateUserRequest")
	if req == nil {
		t.Fatal("CreateUserRequest not found")
	}
	if req.Kind != spec.RequestDTO {
		t.Errorf("wrong Kind: %v", req.Kind)
	}
	if len(req.Fields) == 0 {
		t.Error("CreateUserRequest has no fields")
	}
}

func TestLevel1_ExternalInput(t *testing.T) {
	s := loadSuite(t)
	inp := s.ObjectByName("ChargeRequest")
	if inp == nil {
		t.Fatal("ChargeRequest ExternalInput not found")
	}
	if inp.Kind != spec.ExternalInput {
		t.Errorf("wrong Kind: %v", inp.Kind)
	}
	// Should have the inline fields declared in the fixture (amount, currency, token)
	if len(inp.Fields) != 3 {
		t.Errorf("ChargeRequest: want 3 fields (amount, currency, token), got %d", len(inp.Fields))
	}
}

func TestLevel1_ExternalOutput(t *testing.T) {
	s := loadSuite(t)
	out := s.ObjectByName("ChargeResponse")
	if out == nil {
		t.Fatal("ChargeResponse ExternalOutput not found")
	}
	if out.Kind != spec.ExternalOutput {
		t.Errorf("wrong Kind: %v", out.Kind)
	}
	if len(out.Fields) != 2 {
		t.Errorf("ChargeResponse: want 2 fields (charge_id, status), got %d", len(out.Fields))
	}
}

func TestLevel1_ImportResolver_Objects(t *testing.T) {
	s := loadSuite(t)
	money := s.ObjectByName("Money")
	if money == nil {
		t.Fatal("Money not found")
	}
	// Imports are now collected inline by LangPack at generator time, not stored on the IR.
	// Verify instead that the TypeDescriptor has the right Kind for decimal fields.
	for _, f := range money.Fields {
		if f.Name == "Amount" {
			if f.Type.Kind != spec.TypeDecimal {
				t.Errorf("Amount Kind = %v, want TypeDecimal", f.Type.Kind)
			}
			return
		}
	}
	t.Error("Amount field not found on Money")
}

// ─── Level 2: Interfaces ──────────────────────────────────────────────────────

func TestLevel2_RepositoryInterface_Count(t *testing.T) {
	s := loadSuite(t)
	repos := s.InterfacesOfKind(spec.RepositoryInterface)
	if len(repos) != 2 {
		t.Errorf("want 2 RepositoryInterfaces, got %d", len(repos))
	}
}

func TestLevel2_RepositoryInterface_UserFunctions(t *testing.T) {
	s := loadSuite(t)
	repo := s.InterfaceByName("UserRepository")
	if repo == nil {
		t.Fatal("UserRepository not found")
	}
	if len(repo.Functions) < 4 {
		t.Errorf("UserRepository has only %d functions, want at least 4", len(repo.Functions))
	}

	fnNames := make(map[string]bool)
	for _, fn := range repo.Functions {
		fnNames[fn.Name] = true
	}
	for _, req := range []string{"CreateUser", "GetUserByID", "UpdateUser", "DeleteUser"} {
		if !fnNames[req] {
			t.Errorf("UserRepository missing function %s", req)
		}
	}
	for _, req := range []string{"GetUserByEmail", "SoftDeleteUser", "ListUsers"} {
		if !fnNames[req] {
			t.Errorf("UserRepository missing expanded query function %s", req)
		}
	}
}

// Every repo function must carry a non-nil QueryKind so the generator knows
// which GORM pattern to emit without inspecting the function name.
func TestLevel2_RepositoryInterface_QueryKind(t *testing.T) {
	s := loadSuite(t)
	repo := s.InterfaceByName("UserRepository")
	if repo == nil {
		t.Fatal("UserRepository not found")
	}

	for _, fn := range repo.Functions {
		if fn.QueryKind == nil {
			t.Errorf("function %s has nil QueryKind — all repo functions must have QueryKind set", fn.Name)
		}
	}

	// Spot-check specific kinds
	cases := map[string]spec.QueryKind{
		"CreateUser":    spec.QueryCreate,
		"GetUserByID":   spec.QueryGet,
		"UpdateUser":    spec.QueryUpdate,
		"DeleteUser":    spec.QueryDelete,
		"GetUserByEmail": spec.QueryFindBy,
		"SoftDeleteUser":  spec.QuerySoftDelete,
		"ListUsers":       spec.QueryPaginate,
	}
	for _, fn := range repo.Functions {
		if want, ok := cases[fn.Name]; ok {
			if fn.QueryKind == nil || *fn.QueryKind != want {
				got := "nil"
				if fn.QueryKind != nil {
					got = string(rune(*fn.QueryKind + '0'))
				}
				t.Errorf("%s: QueryKind = %s, want %v", fn.Name, got, want)
			}
		}
	}
}

func TestLevel2_HookInterface_Structure(t *testing.T) {
	s := loadSuite(t)
	hooks := s.InterfaceByName("CreateUserHooks")
	if hooks == nil {
		t.Fatal("CreateUserHooks not found")
	}
	if hooks.Kind != spec.HookInterface {
		t.Errorf("wrong Kind: %v", hooks.Kind)
	}

	fnNames := make(map[string]bool)
	for _, fn := range hooks.Functions {
		fnNames[fn.Name] = true
	}
	if !fnNames["BeforeCreateUser"] {
		t.Error("CreateUserHooks missing BeforeCreateUser")
	}
	if !fnNames["BeforeResponse"] {
		t.Error("CreateUserHooks missing BeforeResponse")
	}
	if !fnNames["BeforeTableUsersCreate"] {
		t.Error("CreateUserHooks missing BeforeTableUsersCreate")
	}
	if !fnNames["AfterTableUsersCreate"] {
		t.Error("CreateUserHooks missing AfterTableUsersCreate")
	}
}

func TestLevel2_ServiceInterface_UserAPIs(t *testing.T) {
	s := loadSuite(t)
	svc := s.InterfaceByName("UserAPIsService")
	if svc == nil {
		t.Fatal("UserAPIsService not found")
	}
	if svc.Kind != spec.ServiceInterface {
		t.Errorf("wrong Kind: %v", svc.Kind)
	}
	if len(svc.Functions) != 2 {
		t.Errorf("want 2 functions (CreateUser, GetUser), got %d", len(svc.Functions))
	}
}

func TestLevel2_ExternalInterface(t *testing.T) {
	s := loadSuite(t)
	ext := s.InterfaceByName("StripeClient")
	if ext == nil {
		t.Fatal("StripeClient ExternalInterface not found")
	}
	if len(ext.Functions) != 1 {
		t.Errorf("want 1 function (ChargeCard), got %d", len(ext.Functions))
	}
	if ext.Functions[0].Name != "ChargeCard" {
		t.Errorf("want ChargeCard, got %s", ext.Functions[0].Name)
	}
}

// ExternalInterface params and returns must have TypeRef set to the resolved object
// so generators can navigate directly to the struct definition.
func TestLevel2_ExternalInterface_TypeRef(t *testing.T) {
	s := loadSuite(t)
	ext := s.InterfaceByName("StripeClient")
	if ext == nil {
		t.Fatal("StripeClient not found")
	}
	chargeCard := ext.Functions[0]

	// Param 0 = req (ChargeRequest)
	if len(chargeCard.Params) < 1 {
		t.Fatalf("ChargeCard: want at least 1 param (req), got %d", len(chargeCard.Params))
	}
	reqParam := chargeCard.Params[0]
	if reqParam.TypeRef == nil {
		t.Error("ChargeCard req param TypeRef is nil — should point to ChargeRequest object")
	} else if reqParam.TypeRef.Name != "ChargeRequest" {
		t.Errorf("req TypeRef.Name = %q, want ChargeRequest", reqParam.TypeRef.Name)
	}

	// Return 0 = ChargeResponse
	if len(chargeCard.Returns) < 1 {
		t.Fatalf("ChargeCard: want at least 1 return (ChargeResponse), got %d", len(chargeCard.Returns))
	}
	if chargeCard.Returns[0].TypeRef == nil {
		t.Error("ChargeCard first return TypeRef is nil — should point to ChargeResponse object")
	} else if chargeCard.Returns[0].TypeRef.Name != "ChargeResponse" {
		t.Errorf("return TypeRef.Name = %q, want ChargeResponse", chargeCard.Returns[0].TypeRef.Name)
	}
}

// The mapper MapXxxInput must return the actual ExternalInput or TableModel type,
// not a redundant StepInput copy.
func TestLevel2_MapperInterface_InputType(t *testing.T) {
	s := loadSuite(t)
	mapper := s.InterfaceByName("PlaceOrderMappers")
	if mapper == nil {
		t.Fatal("PlaceOrderMappers not found")
	}

	fnNames := make(map[string]spec.ResolvedFunction)
	for _, fn := range mapper.Functions {
		fnNames[fn.Name] = fn
	}

	// chargePayment step touches external StripeClient.ChargeCard → body is ChargeRequest
	chargeInputFn, ok := fnNames["MapChargePaymentInput"]
	if !ok {
		t.Fatal("MapChargePaymentInput not found in PlaceOrderMappers")
	}
	if len(chargeInputFn.Returns) == 0 {
		t.Fatal("MapChargePaymentInput has no returns")
	}
	if chargeInputFn.Returns[0].Type.CustomName != "ChargeRequest" {
		t.Errorf("MapChargePaymentInput return CustomName = %q, want ChargeRequest", chargeInputFn.Returns[0].Type.CustomName)
	}

	// MapResponse must always be present
	if _, ok := fnNames["MapResponse"]; !ok {
		t.Error("MapResponse not found in PlaceOrderMappers")
	}
}

// ─── Level 3: Implementations ─────────────────────────────────────────────────

func TestLevel3_RepositoryImpl_Structure(t *testing.T) {
	s := loadSuite(t)
	repos := s.ImplsOfKind(spec.RepositoryImpl)
	if len(repos) != 2 {
		t.Errorf("want 2 RepositoryImpls, got %d", len(repos))
	}

	userRepo := s.ImplByName("UserRepositoryImpl")
	if userRepo == nil {
		t.Fatal("UserRepositoryImpl not found")
	}
	if userRepo.Implements == nil {
		t.Error("UserRepositoryImpl.Implements is nil")
	}
	if len(userRepo.Dependencies) == 0 {
		t.Error("UserRepositoryImpl has no dependencies")
	}
	if userRepo.Dependencies[0].FieldName != "db" {
		t.Errorf("first dependency should be db, got %s", userRepo.Dependencies[0].FieldName)
	}
}

func TestLevel3_RepositoryImpl_Methods(t *testing.T) {
	s := loadSuite(t)
	userRepo := s.ImplByName("UserRepositoryImpl")
	if userRepo == nil {
		t.Fatal("UserRepositoryImpl not found")
	}
	if len(userRepo.Methods) < 4 {
		t.Errorf("want at least 4 methods, got %d", len(userRepo.Methods))
	}
	for _, m := range userRepo.Methods {
		if len(m.Touches) != 1 {
			t.Errorf("method %s: want 1 touch, got %d", m.FunctionName, len(m.Touches))
		}
		if len(m.Touches) > 0 && m.Touches[0].Kind != spec.TouchKindQuery {
			t.Errorf("method %s: touch kind = %v, want TouchKindQuery", m.FunctionName, m.Touches[0].Kind)
		}
	}
}

func TestLevel3_ExternalImpl_HTTPCall(t *testing.T) {
	s := loadSuite(t)
	extImpls := s.ImplsOfKind(spec.ExternalImpl)
	if len(extImpls) != 1 {
		t.Errorf("want 1 ExternalImpl, got %d", len(extImpls))
	}

	stripe := extImpls[0]
	if stripe.Name != "StripeClientImpl" {
		t.Errorf("want StripeClientImpl, got %s", stripe.Name)
	}
	if len(stripe.Methods) != 1 {
		t.Errorf("want 1 method, got %d", len(stripe.Methods))
	}

	chargeCard := stripe.Methods[0]
	if len(chargeCard.Touches) != 1 {
		t.Fatalf("want 1 touch on ChargeCard, got %d", len(chargeCard.Touches))
	}
	touch := chargeCard.Touches[0]
	if touch.Kind != spec.TouchKindHTTPCall {
		t.Errorf("touch kind = %v, want TouchKindHTTPCall", touch.Kind)
	}
	if touch.HTTPMethod != "POST" {
		t.Errorf("HTTPMethod = %q, want POST", touch.HTTPMethod)
	}
	if touch.RetryAttempts != 3 {
		t.Errorf("RetryAttempts = %d, want 3", touch.RetryAttempts)
	}
	if touch.AuthKind != "bearer_token" {
		t.Errorf("AuthKind = %q, want bearer_token", touch.AuthKind)
	}
	if len(touch.StatusErrors) != 2 {
		t.Errorf("want 2 status errors, got %d", len(touch.StatusErrors))
	}
}

func TestLevel3_ExternalMock(t *testing.T) {
	s := loadSuite(t)
	mocks := s.ImplsOfKind(spec.ExternalMockImpl)
	if len(mocks) != 1 {
		t.Errorf("want 1 ExternalMockImpl, got %d", len(mocks))
	}
	if mocks[0].Name != "StripeClientMock" {
		t.Errorf("want StripeClientMock, got %s", mocks[0].Name)
	}
	if len(mocks[0].Methods) != 0 {
		t.Errorf("ExternalMockImpl should have no Methods, got %d", len(mocks[0].Methods))
	}
}

func TestLevel3_ServiceImpl_Structure(t *testing.T) {
	s := loadSuite(t)
	svcs := s.ImplsOfKind(spec.ServiceImpl)
	if len(svcs) != 2 {
		t.Errorf("want 2 ServiceImpls (UserAPIs, OrderAPIs), got %d", len(svcs))
	}
}

func TestLevel3_ServiceImpl_CreateUser_Touches(t *testing.T) {
	s := loadSuite(t)
	userSvc := s.ImplByName("UserAPIsServiceImpl")
	if userSvc == nil {
		t.Fatal("UserAPIsServiceImpl not found")
	}

	var createUser *spec.ResolvedMethod
	for i := range userSvc.Methods {
		if userSvc.Methods[i].FunctionName == "CreateUser" {
			createUser = &userSvc.Methods[i]
			break
		}
	}
	if createUser == nil {
		t.Fatal("CreateUser method not found in UserAPIsServiceImpl")
	}

	if createUser.SharedContext == nil {
		t.Error("CreateUser.SharedContext is nil")
	} else if createUser.SharedContext.Name != "CreateUserContext" {
		t.Errorf("SharedContext.Name = %q, want CreateUserContext", createUser.SharedContext.Name)
	}

	// writeUser (table create) — only touch in Phase 1 scope
	if len(createUser.Touches) != 1 {
		t.Errorf("CreateUser: want 1 touch (table), got %d", len(createUser.Touches))
	}
	if len(createUser.Touches) > 0 && createUser.Touches[0].Kind != spec.TouchKindTable {
		t.Errorf("first touch: kind = %v, want TouchKindTable", createUser.Touches[0].Kind)
	}
}

func TestLevel3_ServiceImpl_PlaceOrder_Touches(t *testing.T) {
	s := loadSuite(t)
	orderSvc := s.ImplByName("OrderAPIsServiceImpl")
	if orderSvc == nil {
		t.Fatal("OrderAPIsServiceImpl not found")
	}

	var placeOrder *spec.ResolvedMethod
	for i := range orderSvc.Methods {
		if orderSvc.Methods[i].FunctionName == "PlaceOrder" {
			placeOrder = &orderSvc.Methods[i]
			break
		}
	}
	if placeOrder == nil {
		t.Fatal("PlaceOrder method not found")
	}

	// chargePayment (external) — only touch in Phase 1 scope (transaction deferred)
	if len(placeOrder.Touches) != 1 {
		t.Errorf("PlaceOrder: want 1 touch (external), got %d", len(placeOrder.Touches))
	}
	if len(placeOrder.Touches) > 0 && placeOrder.Touches[0].Kind != spec.TouchKindExternal {
		t.Errorf("first touch: kind = %v, want TouchKindExternal", placeOrder.Touches[0].Kind)
	}
	if len(placeOrder.Touches) > 0 && placeOrder.Touches[0].ResultField != "ChargePaymentOutput" {
		t.Errorf("ResultField = %q, want ChargePaymentOutput", placeOrder.Touches[0].ResultField)
	}
}

// ─── ResolvedSpec Metadata ────────────────────────────────────────────────────

func TestMetadata_Lang(t *testing.T) {
	s := loadSuite(t)
	if s.Lang != spec.LangGo {
		t.Errorf("Lang = %q, want %q", s.Lang, spec.LangGo)
	}
	if len(s.Databases) == 0 || s.Databases[0].Driver != spec.DBPostgres {
		t.Errorf("DB driver = %q, want %q", s.Databases[0].Driver, spec.DBPostgres)
	}
	if s.Framework != spec.FrameworkGin {
		t.Errorf("Framework = %q, want %q", s.Framework, spec.FrameworkGin)
	}
}

func TestMetadata_Config(t *testing.T) {
	s := loadSuite(t)
	if len(s.Config) == 0 {
		t.Error("Config is empty")
	}
}
