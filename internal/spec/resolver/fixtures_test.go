package resolver_test

import (
	"testing"

	"stencil/internal/spec"
	"stencil/internal/spec/parser"
	"stencil/internal/spec/resolver"
)

// ─── Fixture loader helpers ──────────────────────────────────────────────────

func loadFixture(t *testing.T, path string) *spec.ResolvedSpec {
	t.Helper()
	ast, err := parser.ParseFile(path)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return resolver.Resolve(ast)
}

func loadQueryVariants(t *testing.T) *spec.ResolvedSpec {
	return loadFixture(t, "../../../testdata/query_variants.yaml")
}
func loadStateMachine(t *testing.T) *spec.ResolvedSpec {
	return loadFixture(t, "../../../testdata/state_machine.yaml")
}
func loadMultiExternal(t *testing.T) *spec.ResolvedSpec {
	return loadFixture(t, "../../../testdata/multi_external.yaml")
}
func loadFieldRules(t *testing.T) *spec.ResolvedSpec {
	return loadFixture(t, "../../../testdata/field_rules.yaml")
}
func loadMultiAPI(t *testing.T) *spec.ResolvedSpec {
	return loadFixture(t, "../../../testdata/multi_api.yaml")
}

// ─────────────────────────────────────────────────────────────────────────────
// QUERY VARIANTS
// Exercises: find_by, exists, count, bulk_create, custom SQL, soft_delete,
//            cursor/offset pagination, default_limit
// ─────────────────────────────────────────────────────────────────────────────

func TestQueryVariants_RepoFunctionCount(t *testing.T) {
	s := loadQueryVariants(t)
	repo := s.InterfaceByName("ProductRepository")
	if repo == nil {
		t.Fatal("ProductRepository not found")
	}

	fnNames := make(map[string]bool)
	for _, fn := range repo.Functions {
		fnNames[fn.Name] = true
	}

	// Standard CRUD (always generated)
	for _, name := range []string{"CreateProduct", "GetProductByID", "UpdateProduct", "DeleteProduct"} {
		if !fnNames[name] {
			t.Errorf("missing standard CRUD function %s", name)
		}
	}

	// Query shorthands
	expected := []string{
		"GetProductBySku",                 // find_by: [sku] returns: single
		"GetProductsByCategoryAndStatus",  // find_by: [category, status]
		"ProductExistsBySku",              // exists: [sku]
		"CountProductsByStatus",           // count: [status]
		"BatchCreateProducts",             // bulk_create: true
		"SoftDeleteProduct",               // soft_delete: true
		"ListProducts",                    // paginate: cursor
		"GetTopSellingProducts",           // custom: GetTopSellingProducts
	}
	for _, name := range expected {
		if !fnNames[name] {
			t.Errorf("missing query function %s", name)
		}
	}
}

func TestQueryVariants_ExistsQueryKind(t *testing.T) {
	s := loadQueryVariants(t)
	repo := s.InterfaceByName("ProductRepository")
	if repo == nil {
		t.Fatal("ProductRepository not found")
	}
	for _, fn := range repo.Functions {
		if fn.Name == "ProductExistsBySku" {
			if fn.QueryKind == nil || *fn.QueryKind != spec.QueryExists {
				t.Errorf("ProductExistsBySku QueryKind should be QueryExists")
			}
			// exists returns (bool)
			if len(fn.Returns) != 1 {
				t.Fatalf("ProductExistsBySku: want 1 return, got %d", len(fn.Returns))
			}
			if fn.Returns[0].Type.Kind != spec.TypeBool {
				t.Errorf("first return Kind = %v, want TypeBool", fn.Returns[0].Type.Kind)
			}
			return
		}
	}
	t.Error("ProductExistsBySku not found")
}

func TestQueryVariants_CountQueryKind(t *testing.T) {
	s := loadQueryVariants(t)
	repo := s.InterfaceByName("ProductRepository")
	if repo == nil {
		t.Fatal("ProductRepository not found")
	}
	for _, fn := range repo.Functions {
		if fn.Name == "CountProductsByStatus" {
			if fn.QueryKind == nil || *fn.QueryKind != spec.QueryCount {
				t.Errorf("CountProductsByStatus QueryKind should be QueryCount")
			}
			// count returns (int64)
			if len(fn.Returns) != 1 {
				t.Fatalf("CountProductsByStatus: want 1 return, got %d", len(fn.Returns))
			}
			if fn.Returns[0].Type.Kind != spec.TypeInt {
				t.Errorf("first return Kind = %v, want TypeInt", fn.Returns[0].Type.Kind)
			}
			return
		}
	}
	t.Error("CountProductsByStatus not found")
}

func TestQueryVariants_BulkCreateQueryKind(t *testing.T) {
	s := loadQueryVariants(t)
	repo := s.InterfaceByName("ProductRepository")
	if repo == nil {
		t.Fatal("ProductRepository not found")
	}
	for _, fn := range repo.Functions {
		if fn.Name == "BatchCreateProducts" {
			if fn.QueryKind == nil || *fn.QueryKind != spec.QueryBulkCreate {
				t.Errorf("BatchCreateProducts QueryKind should be QueryBulkCreate")
			}
			return
		}
	}
	t.Error("BatchCreateProducts not found")
}

func TestQueryVariants_CustomQueryKind(t *testing.T) {
	s := loadQueryVariants(t)
	repo := s.InterfaceByName("ProductRepository")
	if repo == nil {
		t.Fatal("ProductRepository not found")
	}
	for _, fn := range repo.Functions {
		if fn.Name == "GetTopSellingProducts" {
			if fn.QueryKind == nil || *fn.QueryKind != spec.QueryCustom {
				t.Errorf("GetTopSellingProducts QueryKind should be QueryCustom")
			}
			// custom query with params: (limit int) returns ([]*Product)
			if len(fn.Params) < 1 {
				t.Fatalf("want at least 1 param (limit), got %d", len(fn.Params))
			}
			return
		}
	}
	t.Error("GetTopSellingProducts not found")
}

func TestQueryVariants_CustomQuery_ImplTouch(t *testing.T) {
	s := loadQueryVariants(t)
	impl := s.ImplByName("ProductRepositoryImpl")
	if impl == nil {
		t.Fatal("ProductRepositoryImpl not found")
	}
	for _, m := range impl.Methods {
		if m.FunctionName == "GetTopSellingProducts" {
			if len(m.Touches) != 1 {
				t.Fatalf("want 1 touch, got %d", len(m.Touches))
			}
			touch := m.Touches[0]
			if touch.Kind != spec.TouchKindQuery {
				t.Errorf("touch Kind = %v, want TouchKindQuery", touch.Kind)
			}
			if touch.QueryKind != spec.QueryCustom {
				t.Errorf("touch QueryKind = %v, want QueryCustom", touch.QueryKind)
			}
			if touch.CustomSQL == "" {
				t.Error("custom query touch has empty CustomSQL")
			}
			if len(touch.CustomParams) != 1 {
				t.Errorf("want 1 custom param (limit), got %d", len(touch.CustomParams))
			}
			return
		}
	}
	t.Error("GetTopSellingProducts method not found in impl")
}

func TestQueryVariants_MultiFieldFindBy(t *testing.T) {
	s := loadQueryVariants(t)
	repo := s.InterfaceByName("ProductRepository")
	if repo == nil {
		t.Fatal("ProductRepository not found")
	}
	for _, fn := range repo.Functions {
		if fn.Name == "GetProductsByCategoryAndStatus" {
			// Should have 2 filter params
			if len(fn.Params) != 2 {
				t.Errorf("want 2 params (category, status), got %d", len(fn.Params))
			}
			return
		}
	}
	t.Error("GetProductsByCategoryAndStatus not found")
}

func TestQueryVariants_ReviewsOffsetPagination(t *testing.T) {
	s := loadQueryVariants(t)
	repo := s.InterfaceByName("ReviewRepository")
	if repo == nil {
		t.Fatal("ReviewRepository not found")
	}
	for _, fn := range repo.Functions {
		if fn.Name == "ListReviews" {
			if fn.QueryKind == nil || *fn.QueryKind != spec.QueryPaginate {
				t.Errorf("ListReviews QueryKind should be QueryPaginate")
			}
			// Offset pagination: (page int, limit int) → ([]*Review, int)
			if len(fn.Params) != 2 {
				t.Errorf("offset paginate: want 2 params (page, limit), got %d", len(fn.Params))
			}
			// returns: slice + total count (int)
			if len(fn.Returns) != 2 {
				t.Errorf("offset paginate: want 2 returns ([]Review, int), got %d", len(fn.Returns))
			}
			return
		}
	}
	t.Error("ListReviews not found")
}

func TestQueryVariants_ProductsCursorPagination(t *testing.T) {
	s := loadQueryVariants(t)
	repo := s.InterfaceByName("ProductRepository")
	if repo == nil {
		t.Fatal("ProductRepository not found")
	}
	for _, fn := range repo.Functions {
		if fn.Name == "ListProducts" {
			// Cursor pagination: (cursor string, limit int) → ([]*Product, string)
			if len(fn.Params) != 2 {
				t.Errorf("cursor paginate: want 2 params (cursor, limit), got %d", len(fn.Params))
			}
			if len(fn.Returns) != 2 {
				t.Errorf("cursor paginate: want 2 returns, got %d", len(fn.Returns))
			}
			if fn.Returns[1].Type.Kind != spec.TypeStr {
				t.Errorf("second return should be string (next cursor), got Kind=%v", fn.Returns[1].Type.Kind)
			}
			return
		}
	}
	t.Error("ListProducts not found")
}

func TestQueryVariants_RepoImpl_PaginationTouch(t *testing.T) {
	s := loadQueryVariants(t)
	impl := s.ImplByName("ProductRepositoryImpl")
	if impl == nil {
		t.Fatal("ProductRepositoryImpl not found")
	}
	for _, m := range impl.Methods {
		if m.FunctionName == "ListProducts" {
			if len(m.Touches) != 1 {
				t.Fatalf("want 1 touch, got %d", len(m.Touches))
			}
			touch := m.Touches[0]
			if touch.PaginationKind != "cursor" {
				t.Errorf("PaginationKind = %q, want cursor", touch.PaginationKind)
			}
			if touch.OrderByField == nil {
				t.Error("OrderByField is nil")
			} else if touch.OrderByField.Name != "CreatedAt" {
				t.Errorf("OrderByField = %q, want CreatedAt", touch.OrderByField.Name)
			}
			if touch.OrderDir != "desc" {
				t.Errorf("OrderDir = %q, want desc", touch.OrderDir)
			}
			return
		}
	}
	t.Error("ListProducts method not found in impl")
}

func TestQueryVariants_TwoTables(t *testing.T) {
	s := loadQueryVariants(t)
	tables := s.ObjectsOfKind(spec.TableModel)
	if len(tables) != 2 {
		t.Errorf("want 2 tables (products, reviews), got %d", len(tables))
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// STATE MACHINE
// Exercises: states block, enum field validation, transition modeling,
//            multiple tables with independent state machines
// ─────────────────────────────────────────────────────────────────────────────

func TestStateMachine_TicketTableModel(t *testing.T) {
	s := loadStateMachine(t)
	ticket := s.ObjectByName("Ticket")
	if ticket == nil {
		t.Fatal("Ticket TableModel not found")
	}

	// Verify enum fields are resolved
	var statusField *spec.ResolvedField
	var priorityField *spec.ResolvedField
	for i := range ticket.Fields {
		switch ticket.Fields[i].Name {
		case "Status":
			statusField = &ticket.Fields[i]
		case "Priority":
			priorityField = &ticket.Fields[i]
		}
	}

	if statusField == nil {
		t.Fatal("Status field not found on Ticket")
	}
	if !statusField.Type.IsEnum {
		t.Error("Status should be IsEnum=true")
	}
	if len(statusField.Values) != 5 {
		t.Errorf("Status enum values: want 5, got %d", len(statusField.Values))
	}

	if priorityField == nil {
		t.Fatal("Priority field not found on Ticket")
	}
	if !priorityField.Type.IsEnum {
		t.Error("Priority should be IsEnum=true")
	}
	if len(priorityField.Values) != 4 {
		t.Errorf("Priority enum values: want 4, got %d", len(priorityField.Values))
	}
}

func TestStateMachine_NullableField(t *testing.T) {
	s := loadStateMachine(t)
	ticket := s.ObjectByName("Ticket")
	if ticket == nil {
		t.Fatal("Ticket TableModel not found")
	}
	for _, f := range ticket.Fields {
		if f.Name == "AssigneeId" {
			if !f.Nullable {
				t.Error("AssigneeId should be nullable")
			}
			return
		}
	}
	t.Error("AssigneeId field not found")
}

func TestStateMachine_InvoiceTableModel(t *testing.T) {
	s := loadStateMachine(t)
	invoice := s.ObjectByName("Invoice")
	if invoice == nil {
		t.Fatal("Invoice TableModel not found")
	}

	var statusField *spec.ResolvedField
	for i := range invoice.Fields {
		if invoice.Fields[i].Name == "Status" {
			statusField = &invoice.Fields[i]
			break
		}
	}
	if statusField == nil {
		t.Fatal("Status field not found on Invoice")
	}
	if len(statusField.Values) != 5 {
		t.Errorf("Invoice status values: want 5, got %d", len(statusField.Values))
	}
}

func TestStateMachine_TwoTables(t *testing.T) {
	s := loadStateMachine(t)
	tables := s.ObjectsOfKind(spec.TableModel)
	if len(tables) != 2 {
		t.Errorf("want 2 tables (tickets, invoices), got %d", len(tables))
	}
}

func TestStateMachine_UpdateAPI_Hooks(t *testing.T) {
	s := loadStateMachine(t)
	hooks := s.InterfaceByName("UpdateTicketStatusHooks")
	if hooks == nil {
		t.Fatal("UpdateTicketStatusHooks not found")
	}

	fnNames := make(map[string]bool)
	for _, fn := range hooks.Functions {
		fnNames[fn.Name] = true
	}

	expected := []string{
		"BeforeUpdateTicketStatus",
		"BeforeTableTicketsUpdate",
		"AfterTableTicketsUpdate",
		"BeforeResponse",
	}
	for _, name := range expected {
		if !fnNames[name] {
			t.Errorf("missing hook function %s", name)
		}
	}
}

func TestStateMachine_ServiceImpl(t *testing.T) {
	s := loadStateMachine(t)
	svc := s.ImplByName("TicketAPIsServiceImpl")
	if svc == nil {
		t.Fatal("TicketAPIsServiceImpl not found")
	}
	if len(svc.Methods) != 2 {
		t.Errorf("want 2 methods (CreateTicket, UpdateTicketStatus), got %d", len(svc.Methods))
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// MULTI EXTERNAL
// Exercises: multiple external services, bearer vs api_key vs no auth,
//            multiple calls per external, calls with/without body/response,
//            fatal vs non-fatal steps, services touching multiple externals
// ─────────────────────────────────────────────────────────────────────────────

func TestMultiExternal_ExternalInterfaces(t *testing.T) {
	s := loadMultiExternal(t)
	exts := s.InterfacesOfKind(spec.ExternalInterface)
	if len(exts) != 3 {
		t.Errorf("want 3 ExternalInterfaces (Stripe, Sendgrid, Geocode), got %d", len(exts))
	}
}

func TestMultiExternal_StripeHasTwoCalls(t *testing.T) {
	s := loadMultiExternal(t)
	stripe := s.InterfaceByName("StripeClient")
	if stripe == nil {
		t.Fatal("StripeClient not found")
	}
	if len(stripe.Functions) != 2 {
		t.Errorf("StripeClient: want 2 functions (ChargeCard, RefundCharge), got %d", len(stripe.Functions))
	}

	fnNames := make(map[string]bool)
	for _, fn := range stripe.Functions {
		fnNames[fn.Name] = true
	}
	if !fnNames["ChargeCard"] {
		t.Error("missing ChargeCard on StripeClient")
	}
	if !fnNames["RefundCharge"] {
		t.Error("missing RefundCharge on StripeClient")
	}
}

func TestMultiExternal_SendgridNoResponse(t *testing.T) {
	s := loadMultiExternal(t)
	sg := s.InterfaceByName("SendgridClient")
	if sg == nil {
		t.Fatal("SendgridClient not found")
	}
	if len(sg.Functions) != 1 {
		t.Fatalf("want 1 function, got %d", len(sg.Functions))
	}
	fn := sg.Functions[0]
	if fn.Name != "SendEmail" {
		t.Errorf("want SendEmail, got %s", fn.Name)
	}
	// No response type → no returns (void)
	if len(fn.Returns) != 0 {
		t.Errorf("SendEmail: want 0 returns (void, no response), got %d", len(fn.Returns))
	}
}

func TestMultiExternal_GeocodeNoBody(t *testing.T) {
	s := loadMultiExternal(t)
	geo := s.InterfaceByName("GeocodeClient")
	if geo == nil {
		t.Fatal("GeocodeClient not found")
	}
	fn := geo.Functions[0]
	if fn.Name != "Lookup" {
		t.Errorf("want Lookup, got %s", fn.Name)
	}
	// GET with no body → no params
	if len(fn.Params) != 0 {
		t.Errorf("Lookup: want 0 params (no body, no ctx), got %d", len(fn.Params))
	}
	// Has response → returns (GeocodeResult)
	if len(fn.Returns) != 1 {
		t.Errorf("Lookup: want 1 return (response), got %d", len(fn.Returns))
	}
}

func TestMultiExternal_ExternalImpls(t *testing.T) {
	s := loadMultiExternal(t)
	impls := s.ImplsOfKind(spec.ExternalImpl)
	if len(impls) != 3 {
		t.Errorf("want 3 ExternalImpls, got %d", len(impls))
	}
	mocks := s.ImplsOfKind(spec.ExternalMockImpl)
	if len(mocks) != 3 {
		t.Errorf("want 3 ExternalMockImpls, got %d", len(mocks))
	}
}

func TestMultiExternal_StripeImpl_AuthKind(t *testing.T) {
	s := loadMultiExternal(t)
	impl := s.ImplByName("StripeClientImpl")
	if impl == nil {
		t.Fatal("StripeClientImpl not found")
	}
	if len(impl.Methods) != 2 {
		t.Errorf("want 2 methods, got %d", len(impl.Methods))
	}
	// Check auth kind on the first method's touch
	touch := impl.Methods[0].Touches[0]
	if touch.AuthKind != "bearer_token" {
		t.Errorf("AuthKind = %q, want bearer_token", touch.AuthKind)
	}
}

func TestMultiExternal_SendgridImpl_AuthKind(t *testing.T) {
	s := loadMultiExternal(t)
	impl := s.ImplByName("SendgridClientImpl")
	if impl == nil {
		t.Fatal("SendgridClientImpl not found")
	}
	touch := impl.Methods[0].Touches[0]
	if touch.AuthKind != "api_key" {
		t.Errorf("AuthKind = %q, want api_key", touch.AuthKind)
	}
}

func TestMultiExternal_GeocodeImpl_NoAuth(t *testing.T) {
	s := loadMultiExternal(t)
	impl := s.ImplByName("GeocodeClientImpl")
	if impl == nil {
		t.Fatal("GeocodeClientImpl not found")
	}
	touch := impl.Methods[0].Touches[0]
	if touch.AuthKind != "" {
		t.Errorf("AuthKind = %q, want empty (no auth)", touch.AuthKind)
	}
}

func TestMultiExternal_ServiceImpl_MultipleTouches(t *testing.T) {
	s := loadMultiExternal(t)
	svc := s.ImplByName("PaymentAPIsServiceImpl")
	if svc == nil {
		t.Fatal("PaymentAPIsServiceImpl not found")
	}

	// Find ProcessPayment method
	var processPayment *spec.ResolvedMethod
	for i := range svc.Methods {
		if svc.Methods[i].FunctionName == "ProcessPayment" {
			processPayment = &svc.Methods[i]
			break
		}
	}
	if processPayment == nil {
		t.Fatal("ProcessPayment method not found")
	}

	// Should have 3 touches: table(get) + external(Stripe) + external(Sendgrid)
	if len(processPayment.Touches) != 3 {
		t.Errorf("ProcessPayment: want 3 touches, got %d", len(processPayment.Touches))
	}
	if processPayment.Touches[0].Kind != spec.TouchKindTable {
		t.Errorf("touch 0: want TouchKindTable, got %v", processPayment.Touches[0].Kind)
	}
	if processPayment.Touches[1].Kind != spec.TouchKindExternal {
		t.Errorf("touch 1: want TouchKindExternal, got %v", processPayment.Touches[1].Kind)
	}
	if processPayment.Touches[2].Kind != spec.TouchKindExternal {
		t.Errorf("touch 2: want TouchKindExternal, got %v", processPayment.Touches[2].Kind)
	}

	// Third touch (SendEmail) should be non-fatal
	if processPayment.Touches[2].FatalError {
		t.Error("SendEmail touch should be non-fatal (fatal: false)")
	}
	// First two should be fatal
	if !processPayment.Touches[0].FatalError {
		t.Error("lookupOrder touch should be fatal")
	}
	if !processPayment.Touches[1].FatalError {
		t.Error("chargeCard touch should be fatal")
	}
}

func TestMultiExternal_ExternalIOObjects(t *testing.T) {
	s := loadMultiExternal(t)

	// Stripe has body+response for both calls → 4 objects
	// Sendgrid has body only → 1 object
	// Geocode has response only → 1 object
	inputs := s.ObjectsOfKind(spec.ExternalInput)
	outputs := s.ObjectsOfKind(spec.ExternalOutput)

	if len(inputs) != 3 {
		t.Errorf("want 3 ExternalInput objects, got %d", len(inputs))
	}
	if len(outputs) != 3 {
		t.Errorf("want 3 ExternalOutput objects, got %d", len(outputs))
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// FIELD RULES
// Exercises: validation rules (email, min_length, max_length, min, max),
//            nullable fields, private fields, custom types embedded in tables,
//            multiple custom types, compute fields on DTOs
// ─────────────────────────────────────────────────────────────────────────────

func TestFieldRules_CustomTypes(t *testing.T) {
	s := loadFieldRules(t)
	types := s.ObjectsOfKind(spec.TypeObject)
	if len(types) != 2 {
		t.Errorf("want 2 custom types (Address, ContactInfo), got %d", len(types))
	}
}

func TestFieldRules_AddressFields(t *testing.T) {
	s := loadFieldRules(t)
	addr := s.ObjectByName("Address")
	if addr == nil {
		t.Fatal("Address TypeObject not found")
	}
	if len(addr.Fields) != 5 {
		t.Errorf("Address: want 5 fields, got %d", len(addr.Fields))
	}

	// Zip should have validation rules (min_length, max_length)
	for _, f := range addr.Fields {
		if f.Name == "Zip" {
			if len(f.Rules) == 0 {
				t.Error("Zip Rules is empty — should contain min_length and max_length rules")
			}
			return
		}
	}
	t.Error("Zip field not found on Address")
}

func TestFieldRules_CustomerNullableFields(t *testing.T) {
	s := loadFieldRules(t)
	customer := s.ObjectByName("Customer")
	if customer == nil {
		t.Fatal("Customer TableModel not found")
	}

	cases := map[string]bool{
		"Age":     true,  // nullable: true
		"Address": true,  // nullable: true in YAML
		"Notes":   true,  // nullable: true
		"Email":   false, // not nullable
	}
	for _, f := range customer.Fields {
		if expected, ok := cases[f.Name]; ok {
			if f.Nullable != expected {
				t.Errorf("field %s: Nullable = %v, want %v", f.Name, f.Nullable, expected)
			}
		}
	}
}

func TestFieldRules_PrivateField(t *testing.T) {
	s := loadFieldRules(t)
	customer := s.ObjectByName("Customer")
	if customer == nil {
		t.Fatal("Customer TableModel not found")
	}
	for _, f := range customer.Fields {
		if f.Name == "PasswordHash" {
			if !f.Private {
				t.Error("PasswordHash should be Private=true")
			}
			return
		}
	}
	t.Error("PasswordHash field not found")
}

func TestFieldRules_CustomTypeInTable(t *testing.T) {
	s := loadFieldRules(t)
	customer := s.ObjectByName("Customer")
	if customer == nil {
		t.Fatal("Customer TableModel not found")
	}

	for _, f := range customer.Fields {
		if f.Name == "Address" {
			if !f.Type.IsCustom {
				t.Error("Address field should be IsCustom=true")
			}
			if f.TypeRef == nil {
				t.Error("Address TypeRef is nil — linkTypeRefs did not link")
			} else if f.TypeRef.Name != "Address" {
				t.Errorf("Address TypeRef.Name = %q, want Address", f.TypeRef.Name)
			}
			return
		}
	}
	t.Error("Address field not found on Customer")
}

func TestFieldRules_MultipleCustomTypesInTable(t *testing.T) {
	s := loadFieldRules(t)
	customer := s.ObjectByName("Customer")
	if customer == nil {
		t.Fatal("Customer not found")
	}

	customFields := 0
	for _, f := range customer.Fields {
		if f.Type.IsCustom {
			customFields++
		}
	}
	if customFields != 2 {
		t.Errorf("want 2 custom type fields (Address, Contact), got %d", customFields)
	}
}

func TestFieldRules_ComputeField(t *testing.T) {
	s := loadFieldRules(t)
	resp := s.ObjectByName("CreateCustomerResponse")
	if resp == nil {
		t.Fatal("CreateCustomerResponse not found")
	}
	for _, f := range resp.Fields {
		if f.Name == "FullName" {
			if !f.Compute {
				t.Error("FullName should have Compute=true")
			}
			return
		}
	}
	t.Error("FullName field not found on CustomerResponse")
}

func TestFieldRules_EnumFieldOnTable(t *testing.T) {
	s := loadFieldRules(t)
	customer := s.ObjectByName("Customer")
	if customer == nil {
		t.Fatal("Customer not found")
	}
	for _, f := range customer.Fields {
		if f.Name == "Tier" {
			if !f.Type.IsEnum {
				t.Error("Tier should be IsEnum=true")
			}
			if len(f.Values) != 4 {
				t.Errorf("Tier values: want 4, got %d", len(f.Values))
			}
			return
		}
	}
	t.Error("Tier field not found")
}

func TestFieldRules_MapperHasMustOverrideForCompute(t *testing.T) {
	s := loadFieldRules(t)
	mapper := s.ImplByName("DefaultCreateCustomerMappers")
	if mapper == nil {
		t.Fatal("DefaultCreateCustomerMappers not found")
	}
	// FullName is compute=true → must have MustOverride=true in field mappings
	for _, fm := range mapper.FieldMappings {
		if fm.TargetField == "FullName" && fm.MethodName == "MapResponse" {
			if !fm.MustOverride {
				t.Error("FullName mapping should be MustOverride=true (compute field)")
			}
			return
		}
	}
	t.Error("FullName field mapping not found in DefaultCreateCustomerMappers")
}

func TestFieldRules_ExistsQueryOnCustomer(t *testing.T) {
	s := loadFieldRules(t)
	repo := s.InterfaceByName("CustomerRepository")
	if repo == nil {
		t.Fatal("CustomerRepository not found")
	}
	fnNames := make(map[string]bool)
	for _, fn := range repo.Functions {
		fnNames[fn.Name] = true
	}
	if !fnNames["CustomerExistsByEmail"] {
		t.Error("missing CustomerExistsByEmail")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// MULTI API
// Exercises: 3 resource groups, 3 tables, 1 external, custom type,
//            cross-table APIs, multi-step APIs, list ops, state machine,
//            APIs touching both tables and externals in same flow
// ─────────────────────────────────────────────────────────────────────────────

func TestMultiAPI_ThreeResourceGroups(t *testing.T) {
	s := loadMultiAPI(t)
	svcIfaces := s.InterfacesOfKind(spec.ServiceInterface)
	if len(svcIfaces) != 3 {
		t.Errorf("want 3 ServiceInterfaces, got %d", len(svcIfaces))
	}

	names := make(map[string]bool)
	for _, iface := range svcIfaces {
		names[iface.Name] = true
	}
	for _, expected := range []string{"UserAPIsService", "ProductAPIsService", "OrderAPIsService"} {
		if !names[expected] {
			t.Errorf("missing ServiceInterface %s", expected)
		}
	}
}

func TestMultiAPI_ThreeTables(t *testing.T) {
	s := loadMultiAPI(t)
	tables := s.ObjectsOfKind(spec.TableModel)
	if len(tables) != 3 {
		t.Errorf("want 3 tables (users, products, orders), got %d", len(tables))
	}
}

func TestMultiAPI_OrderServiceMethodCount(t *testing.T) {
	s := loadMultiAPI(t)
	svc := s.InterfaceByName("OrderAPIsService")
	if svc == nil {
		t.Fatal("OrderAPIsService not found")
	}
	if len(svc.Functions) != 3 {
		t.Errorf("want 3 functions (PlaceOrder, GetOrder, ListMyOrders), got %d", len(svc.Functions))
	}
}

func TestMultiAPI_PlaceOrder_ThreeSteps(t *testing.T) {
	s := loadMultiAPI(t)
	svc := s.ImplByName("OrderAPIsServiceImpl")
	if svc == nil {
		t.Fatal("OrderAPIsServiceImpl not found")
	}

	var placeOrder *spec.ResolvedMethod
	for i := range svc.Methods {
		if svc.Methods[i].FunctionName == "PlaceOrder" {
			placeOrder = &svc.Methods[i]
			break
		}
	}
	if placeOrder == nil {
		t.Fatal("PlaceOrder method not found")
	}

	// 3 steps: checkStock(external) + reserveStock(external) + createOrder(table)
	if len(placeOrder.Touches) != 3 {
		t.Errorf("want 3 touches, got %d", len(placeOrder.Touches))
	}
	if placeOrder.Touches[0].Kind != spec.TouchKindExternal {
		t.Errorf("touch 0: want External, got %v", placeOrder.Touches[0].Kind)
	}
	if placeOrder.Touches[1].Kind != spec.TouchKindExternal {
		t.Errorf("touch 1: want External, got %v", placeOrder.Touches[1].Kind)
	}
	if placeOrder.Touches[2].Kind != spec.TouchKindTable {
		t.Errorf("touch 2: want Table, got %v", placeOrder.Touches[2].Kind)
	}
}

func TestMultiAPI_PlaceOrder_SharedContext(t *testing.T) {
	s := loadMultiAPI(t)
	ctx := s.ObjectByName("PlaceOrderContext")
	if ctx == nil {
		t.Fatal("PlaceOrderContext not found")
	}

	fieldNames := make(map[string]bool)
	for _, f := range ctx.Fields {
		fieldNames[f.Name] = true
	}

	if !fieldNames["Request"] {
		t.Error("missing Request field")
	}
	if !fieldNames["Response"] {
		t.Error("missing Response field")
	}
	// Table create step → output field
	if !fieldNames["CreateOrderOutput"] {
		t.Error("missing CreateOrderOutput field from table create step")
	}
}

func TestMultiAPI_CustomTypeInProduct(t *testing.T) {
	s := loadMultiAPI(t)
	product := s.ObjectByName("Product")
	if product == nil {
		t.Fatal("Product TableModel not found")
	}
	for _, f := range product.Fields {
		if f.Name == "Price" {
			if !f.Type.IsCustom {
				t.Error("Price should be IsCustom=true")
			}
			if f.TypeRef == nil {
				t.Error("Price TypeRef is nil")
			} else if f.TypeRef.Name != "Money" {
				t.Errorf("Price TypeRef = %q, want Money", f.TypeRef.Name)
			}
			return
		}
	}
	t.Error("Price field not found on Product")
}

func TestMultiAPI_OrderSoftDelete(t *testing.T) {
	s := loadMultiAPI(t)
	order := s.ObjectByName("Order")
	if order == nil {
		t.Fatal("Order TableModel not found")
	}
	if !order.SoftDelete {
		t.Error("Order should have SoftDelete=true")
	}
	// Should have DeletedAt field
	for _, f := range order.Fields {
		if f.Name == "DeletedAt" {
			if !f.Nullable {
				t.Error("DeletedAt should be nullable")
			}
			return
		}
	}
	t.Error("DeletedAt field not found on Order")
}

func TestMultiAPI_InventoryExternal_TwoCalls(t *testing.T) {
	s := loadMultiAPI(t)
	ext := s.InterfaceByName("InventoryService")
	if ext == nil {
		t.Fatal("InventoryService external interface not found")
	}
	if len(ext.Functions) != 2 {
		t.Errorf("want 2 functions (CheckStock, ReserveStock), got %d", len(ext.Functions))
	}

	// CheckStock: GET, no body → 0 params
	checkStock := ext.Functions[0]
	if checkStock.Name != "CheckStock" {
		t.Errorf("first function: want CheckStock, got %s", checkStock.Name)
	}
	if len(checkStock.Params) != 0 {
		t.Errorf("CheckStock: want 0 params (no body), got %d", len(checkStock.Params))
	}

	// ReserveStock: POST with body → 1 param (body)
	reserveStock := ext.Functions[1]
	if reserveStock.Name != "ReserveStock" {
		t.Errorf("second function: want ReserveStock, got %s", reserveStock.Name)
	}
	if len(reserveStock.Params) != 1 {
		t.Errorf("ReserveStock: want 1 param (body), got %d", len(reserveStock.Params))
	}
}

func TestMultiAPI_HookInterfaces_Count(t *testing.T) {
	s := loadMultiAPI(t)
	hooks := s.InterfacesOfKind(spec.HookInterface)
	// 2 UserAPIs + 2 ProductAPIs + 3 OrderAPIs = 7 hook interfaces
	if len(hooks) != 7 {
		t.Errorf("want 7 HookInterfaces (one per API), got %d", len(hooks))
	}
}

func TestMultiAPI_MapperInterfaces_Count(t *testing.T) {
	s := loadMultiAPI(t)
	mappers := s.InterfacesOfKind(spec.MapperInterface)
	// 7 APIs with steps → 7 mapper interfaces
	if len(mappers) != 7 {
		t.Errorf("want 7 MapperInterfaces, got %d", len(mappers))
	}
}

func TestMultiAPI_DefaultMapperImpls_Count(t *testing.T) {
	s := loadMultiAPI(t)
	mapperImpls := s.ImplsOfKind(spec.DefaultMapperImpl)
	if len(mapperImpls) != 7 {
		t.Errorf("want 7 DefaultMapperImpls, got %d", len(mapperImpls))
	}
}

func TestMultiAPI_OrderRepo_MultiFieldFindBy(t *testing.T) {
	s := loadMultiAPI(t)
	repo := s.InterfaceByName("OrderRepository")
	if repo == nil {
		t.Fatal("OrderRepository not found")
	}

	fnNames := make(map[string]bool)
	for _, fn := range repo.Functions {
		fnNames[fn.Name] = true
	}

	expected := []string{
		"GetOrdersByBuyerId",
		"GetOrdersByBuyerIdAndStatus",
		"GetOrdersByProductId",
		"SoftDeleteOrder",
		"ListOrders",
	}
	for _, name := range expected {
		if !fnNames[name] {
			t.Errorf("missing query function %s on OrderRepository", name)
		}
	}
}

func TestMultiAPI_ThreeServiceImpls(t *testing.T) {
	s := loadMultiAPI(t)
	svcImpls := s.ImplsOfKind(spec.ServiceImpl)
	if len(svcImpls) != 3 {
		t.Errorf("want 3 ServiceImpls, got %d", len(svcImpls))
	}
}

func TestMultiAPI_Config(t *testing.T) {
	s := loadMultiAPI(t)
	if len(s.Config) != 2 {
		t.Errorf("want 2 config vars, got %d", len(s.Config))
	}
}

func TestMultiAPI_Indexes(t *testing.T) {
	s := loadMultiAPI(t)
	order := s.ObjectByName("Order")
	if order == nil {
		t.Fatal("Order not found")
	}
	// find_by: [buyer_id], [buyer_id, status], [product_id] → 3 indexes
	if len(order.Indexes) != 3 {
		t.Errorf("want 3 indexes on Order, got %d", len(order.Indexes))
	}
}
