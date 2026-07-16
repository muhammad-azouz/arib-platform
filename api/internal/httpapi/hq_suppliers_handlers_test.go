package httpapi

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// Mirrors hq_customers_handlers_test.go's zero-gateway-calls guarantee for
// every Supplier handler — same validation shape, just against
// /hq/suppliers... and handleHqSupplier*.

func TestHandleHqSuppliers_InvalidFilters400WithoutGateway(t *testing.T) {
	cases := []struct {
		name  string
		query string
	}{
		{"invalid active", "active=maybe"},
		{"invalid debt", "debt=bankrupt"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/v1/tenants/tnt_1/hq/suppliers?"+tc.query, nil)
			rec := httptest.NewRecorder()

			(&Server{}).handleHqSuppliers(rec, req)

			if rec.Code != 400 {
				t.Fatalf("status = %d, want 400 (query %q)", rec.Code, tc.query)
			}
		})
	}
}

func TestHandleHqSupplierCreate_InvalidBody400WithoutGateway(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"missing name", `{"phone1":"0100","branch_id":"b1"}`},
		{"missing phone1", `{"name":"مورد","branch_id":"b1"}`},
		{"missing branch_id", `{"name":"مورد","phone1":"0100"}`},
		{"name too long", `{"name":"` + strings.Repeat("a", 101) + `","phone1":"0100","branch_id":"b1"}`},
		{"phone1 too long", `{"name":"مورد","phone1":"0123456789012","branch_id":"b1"}`},
		{"negative credit_limit", `{"name":"مورد","phone1":"0100","branch_id":"b1","credit_limit":-1}`},
		{"malformed json", `not json`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/v1/tenants/tnt_1/hq/suppliers", strings.NewReader(tc.body))
			rec := httptest.NewRecorder()

			(&Server{}).handleHqSupplierCreate(rec, req)

			if rec.Code != 400 {
				t.Fatalf("status = %d, want 400 (body %q)", rec.Code, tc.body)
			}
		})
	}
}

func TestHandleHqSupplierUpdate_InvalidBody400WithoutGateway(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"blank name", `{"name":""}`},
		{"name too long", `{"name":"` + strings.Repeat("a", 101) + `"}`},
		{"phone1 too long", `{"phone1":"0123456789012"}`},
		{"negative credit_limit", `{"credit_limit":-1}`},
		{"malformed json", `not json`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("PUT", "/v1/tenants/tnt_1/hq/suppliers/s1", strings.NewReader(tc.body))
			rec := httptest.NewRecorder()

			(&Server{}).handleHqSupplierUpdate(rec, req)

			if rec.Code != 400 {
				t.Fatalf("status = %d, want 400 (body %q)", rec.Code, tc.body)
			}
		})
	}
}

func TestHandleHqSuppliersBulkUpdate_InvalidBody400WithoutGateway(t *testing.T) {
	tooMany := `["` + strings.Repeat(`x","`, 501) + `x"]`
	cases := []struct {
		name string
		body string
	}{
		{"empty ids", `{"ids":[],"group_id":"g1"}`},
		{"too many ids", `{"ids":` + tooMany + `,"group_id":"g1"}`},
		{"no fields", `{"ids":["s1"]}`},
		{"malformed json", `not json`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("PUT", "/v1/tenants/tnt_1/hq/suppliers/bulk", strings.NewReader(tc.body))
			rec := httptest.NewRecorder()

			(&Server{}).handleHqSuppliersBulkUpdate(rec, req)

			if rec.Code != 400 {
				t.Fatalf("status = %d, want 400 (body %q)", rec.Code, tc.body)
			}
		})
	}
}

func TestHandleHqSuppliersExport_InvalidFilters400WithoutGateway(t *testing.T) {
	for _, q := range []string{"active=maybe", "debt=bankrupt"} {
		t.Run(q, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/v1/tenants/tnt_1/hq/suppliers/export?"+q, nil)
			rec := httptest.NewRecorder()

			(&Server{}).handleHqSuppliersExport(rec, req)

			if rec.Code != 400 {
				t.Fatalf("status = %d, want 400 (query %q)", rec.Code, q)
			}
		})
	}
}

func TestHandleHqSuppliersImport_RejectsNonMultipart400WithoutGateway(t *testing.T) {
	req := httptest.NewRequest("POST", "/v1/tenants/tnt_1/hq/suppliers/import", strings.NewReader("name,phone1,branch_id\n"))
	req.Header.Set("Content-Type", "text/csv")
	rec := httptest.NewRecorder()

	(&Server{}).handleHqSuppliersImport(rec, req)

	if rec.Code != 400 {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}
