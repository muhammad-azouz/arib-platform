package hq

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/aribpos/license-api/internal/model"
)

// Mirrors service_test.go's Customer method tests for their Supplier
// counterparts — same request/response wiring, just against /hq/suppliers...
// and the Supplier* types.

func TestSuppliers_PassesParamsAndDecoratesBranch(t *testing.T) {
	var gotQuery url.Values
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"total":1,"page":1,"page_size":50,"items":[` +
			`{"id":"s1","num":1,"name":"مورد الجملة","branch_id":"b1","phone1":"0100","is_active":true,"balance":150.5,"credit_limit":500,"is_credit":false}]}`))
	}))
	defer gw.Close()

	fresh := time.Now().UTC().Add(-3 * time.Minute)
	st := testStore(gw.URL)
	st.branches = []model.Branch{{ID: "b1", TenantID: "tnt_1", Name: "وسط البلد", Status: model.BranchActive, LastSyncAt: &fresh}}
	s := New(st, &fakeTokens{}, nil)
	params := url.Values{"search": {"مورد"}, "debt": {"has_debt"}}
	env, err := s.Suppliers(context.Background(), "acc_owner", "tnt_1", params)
	if err != nil {
		t.Fatalf("suppliers: %v", err)
	}
	if gotQuery.Get("search") != "مورد" || gotQuery.Get("debt") != "has_debt" {
		t.Fatalf("gateway did not see passed-through params: %v", gotQuery)
	}
	if env.Source != "synced" || env.Data.Total != 1 || len(env.Data.Items) != 1 {
		t.Fatalf("suppliers envelope wrong: %+v", env)
	}
	row := env.Data.Items[0]
	if row.Balance != 150.5 || row.BranchName != "وسط البلد" || row.Health != "ok" {
		t.Fatalf("supplier row not decorated with branch name/health: %+v", row)
	}
}

func TestSupplierDetail_DecoratesBranchAndNotFound(t *testing.T) {
	fresh := time.Now().UTC().Add(-2 * time.Minute)
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/hq/suppliers/s1":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"s1","num":1,"name":"مورد الجملة","branch_id":"b1","phone1":"0100",` +
				`"credit_limit":500,"is_credit":false,"is_active":true,"balance":150.5,` +
				`"stats":{"number_of_orders":3,"total_spent":900,"average_order_value":300,"last_purchase_date":"2026-07-01T00:00:00Z"}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer gw.Close()

	st := testStore(gw.URL)
	st.branches = []model.Branch{{ID: "b1", TenantID: "tnt_1", Name: "وسط البلد", Status: model.BranchActive, LastSyncAt: &fresh}}
	s := New(st, &fakeTokens{}, nil)

	env, err := s.SupplierDetail(context.Background(), "acc_owner", "tnt_1", "s1")
	if err != nil {
		t.Fatalf("supplier detail: %v", err)
	}
	if env.Data.BranchName != "وسط البلد" || env.Data.Health != "ok" || env.Data.Stats.NumberOfOrders != 3 || env.Data.Stats.TotalSpent != 900 {
		t.Fatalf("supplier detail wrong: %+v", env.Data)
	}

	if _, err := s.SupplierDetail(context.Background(), "acc_owner", "tnt_1", "does-not-exist"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestCreateSupplier_ForwardsAndReturnsResult(t *testing.T) {
	var gotMethod string
	var gotBody NewSupplier
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		if r.URL.Path != "/hq/suppliers" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusCreated)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"s1","num":42,"written_at":"2026-07-16T12:00:00Z"}`))
	}))
	defer gw.Close()

	s := New(testStore(gw.URL), &fakeTokens{}, nil)
	input := NewSupplier{Name: "مورد الجملة", Phone1: "0100", BranchID: "b1"}
	result, err := s.CreateSupplier(context.Background(), "acc_owner", "tnt_1", input)
	if err != nil {
		t.Fatalf("create supplier: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Fatalf("expected POST, gateway saw %s", gotMethod)
	}
	if gotBody.Name != "مورد الجملة" || gotBody.BranchID != "b1" {
		t.Fatalf("gateway did not receive the forwarded supplier: %+v", gotBody)
	}
	if result.ID != "s1" || result.Num != 42 {
		t.Fatalf("create supplier result wrong: %+v", result)
	}

	if _, err := s.CreateSupplier(context.Background(), "acc_intruder", "tnt_1", input); !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected ErrForbidden, got %v", err)
	}
}

func TestCreateSupplier_InvalidInputForwardsGatewayMessage(t *testing.T) {
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"error":"branch not found"}`))
	}))
	defer gw.Close()

	s := New(testStore(gw.URL), &fakeTokens{}, nil)
	_, err := s.CreateSupplier(context.Background(), "acc_owner", "tnt_1", NewSupplier{Name: "مورد الجملة", Phone1: "0100", BranchID: "ghost"})
	var badInput *InvalidCustomerInputError
	if !errors.As(err, &badInput) || badInput.Error() != "branch not found" {
		t.Fatalf("expected InvalidCustomerInputError(\"branch not found\"), got %v", err)
	}
}

func TestCreateSupplier_MissingAccountOperand(t *testing.T) {
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer gw.Close()

	s := New(testStore(gw.URL), &fakeTokens{}, nil)
	_, err := s.CreateSupplier(context.Background(), "acc_owner", "tnt_1", NewSupplier{Name: "مورد الجملة", Phone1: "0100", BranchID: "b1"})
	if !errors.Is(err, ErrMissingAccountOperand) {
		t.Fatalf("expected ErrMissingAccountOperand, got %v", err)
	}
}

func TestUpdateSupplier_ForwardsPartialBodyAndNotFound(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody SupplierEdit
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		if r.URL.Path != "/hq/suppliers/s1" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"written_at":"2026-07-16T12:00:00Z"}`))
	}))
	defer gw.Close()

	s := New(testStore(gw.URL), &fakeTokens{}, nil)
	name := "اسم جديد"
	if _, err := s.UpdateSupplier(context.Background(), "acc_owner", "tnt_1", "s1", SupplierEdit{Name: &name}); err != nil {
		t.Fatalf("update supplier: %v", err)
	}
	if gotMethod != http.MethodPut || gotPath != "/hq/suppliers/s1" || gotBody.Name == nil || *gotBody.Name != name {
		t.Fatalf("gateway did not receive the forwarded partial update: method=%s path=%s body=%+v", gotMethod, gotPath, gotBody)
	}

	if _, err := s.UpdateSupplier(context.Background(), "acc_owner", "tnt_1", "ghost", SupplierEdit{}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestBulkUpdateSuppliers_ForwardsBodyAndInvalidInput(t *testing.T) {
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/hq/suppliers/bulk" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var body struct {
			IDs     []string `json:"ids"`
			GroupID string   `json:"group_id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if len(body.IDs) != 2 || body.GroupID != "g1" {
			w.WriteHeader(http.StatusBadRequest)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"error":"one or more ids do not belong to this tenant"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"updated":2,"written_at":"2026-07-16T12:00:00Z"}`))
	}))
	defer gw.Close()

	s := New(testStore(gw.URL), &fakeTokens{}, nil)
	groupID := "g1"
	result, err := s.BulkUpdateSuppliers(context.Background(), "acc_owner", "tnt_1", []string{"s1", "s2"}, &groupID, nil)
	if err != nil {
		t.Fatalf("bulk update suppliers: %v", err)
	}
	if result.Updated != 2 {
		t.Fatalf("bulk update suppliers result wrong: %+v", result)
	}

	_, err = s.BulkUpdateSuppliers(context.Background(), "acc_owner", "tnt_1", []string{"ghost"}, &groupID, nil)
	var badInput *InvalidCustomerInputError
	if !errors.As(err, &badInput) {
		t.Fatalf("expected InvalidCustomerInputError, got %v", err)
	}
}

func TestImportSuppliers_ForwardsBodyAndDecodesResult(t *testing.T) {
	var gotContentType, gotBody string
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotContentType = r.Header.Get("Content-Type")
		buf := make([]byte, 1024)
		n, _ := r.Body.Read(buf)
		gotBody = string(buf[:n])
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"created":1,"errors":[{"row":3,"message":"branch not found"}]}`))
	}))
	defer gw.Close()

	s := New(testStore(gw.URL), &fakeTokens{}, nil)
	result, err := s.ImportSuppliers(context.Background(), "acc_owner", "tnt_1",
		`multipart/form-data; boundary=X`, strings.NewReader("--X--"))
	if err != nil {
		t.Fatalf("import suppliers: %v", err)
	}
	if gotContentType != "multipart/form-data; boundary=X" || !strings.Contains(gotBody, "--X--") {
		t.Fatalf("gateway did not see the forwarded multipart body: ct=%q body=%q", gotContentType, gotBody)
	}
	if result.Created != 1 || len(result.Errors) != 1 || result.Errors[0].Message != "branch not found" {
		t.Fatalf("import result wrong: %+v", result)
	}
}
