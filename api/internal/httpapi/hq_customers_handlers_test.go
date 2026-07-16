package httpapi

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// handleHqCustomers validates active/debt before touching claims or the hq
// service, so a bad value 400s without needing any auth/gateway wiring — the
// zero-gateway-calls guarantee T54 asks for. A nil *Server would panic if the
// handler ever reached claimsFrom/s.hq.Customers, so reaching 400 here is
// itself proof the gateway was never called.
func TestHandleHqCustomers_InvalidFilters400WithoutGateway(t *testing.T) {
	cases := []struct {
		name  string
		query string
	}{
		{"invalid active", "active=maybe"},
		{"invalid debt", "debt=bankrupt"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/v1/tenants/tnt_1/hq/customers?"+tc.query, nil)
			rec := httptest.NewRecorder()

			(&Server{}).handleHqCustomers(rec, req)

			if rec.Code != 400 {
				t.Fatalf("status = %d, want 400 (query %q)", rec.Code, tc.query)
			}
		})
	}
}

// handleHqCustomerCreate validates before it ever dereferences claims or
// calls the hq service, so every case here must 400 without a configured
// Server — matching handleHqCatalogProductCreate's own defense-in-depth
// validation.
func TestHandleHqCustomerCreate_InvalidBody400WithoutGateway(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"missing name", `{"phone1":"0100","branch_id":"b1"}`},
		{"missing phone1", `{"name":"محمد","branch_id":"b1"}`},
		{"missing branch_id", `{"name":"محمد","phone1":"0100"}`},
		{"name too long", `{"name":"` + strings.Repeat("a", 101) + `","phone1":"0100","branch_id":"b1"}`},
		{"phone1 too long", `{"name":"محمد","phone1":"0123456789012","branch_id":"b1"}`},
		{"negative credit_limit", `{"name":"محمد","phone1":"0100","branch_id":"b1","credit_limit":-1}`},
		{"malformed json", `not json`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/v1/tenants/tnt_1/hq/customers", strings.NewReader(tc.body))
			rec := httptest.NewRecorder()

			(&Server{}).handleHqCustomerCreate(rec, req)

			if rec.Code != 400 {
				t.Fatalf("status = %d, want 400 (body %q)", rec.Code, tc.body)
			}
		})
	}
}

// handleHqCustomerUpdate: every provided field is validated up front, same
// zero-gateway-calls guarantee.
func TestHandleHqCustomerUpdate_InvalidBody400WithoutGateway(t *testing.T) {
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
			req := httptest.NewRequest("PUT", "/v1/tenants/tnt_1/hq/customers/c1", strings.NewReader(tc.body))
			rec := httptest.NewRecorder()

			(&Server{}).handleHqCustomerUpdate(rec, req)

			if rec.Code != 400 {
				t.Fatalf("status = %d, want 400 (body %q)", rec.Code, tc.body)
			}
		})
	}
}

// handleHqCustomersBulkUpdate: empty/oversized id lists and a body with
// neither group_id nor price_tier all 400 before any gateway call.
func TestHandleHqCustomersBulkUpdate_InvalidBody400WithoutGateway(t *testing.T) {
	tooMany := `["` + strings.Repeat(`x","`, 501) + `x"]`
	cases := []struct {
		name string
		body string
	}{
		{"empty ids", `{"ids":[],"group_id":"g1"}`},
		{"too many ids", `{"ids":` + tooMany + `,"group_id":"g1"}`},
		{"no fields", `{"ids":["c1"]}`},
		{"malformed json", `not json`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("PUT", "/v1/tenants/tnt_1/hq/customers/bulk", strings.NewReader(tc.body))
			rec := httptest.NewRecorder()

			(&Server{}).handleHqCustomersBulkUpdate(rec, req)

			if rec.Code != 400 {
				t.Fatalf("status = %d, want 400 (body %q)", rec.Code, tc.body)
			}
		})
	}
}

// handleHqCustomersExport validates active/debt up front, same as the list
// endpoint, before touching claims or the hq service.
func TestHandleHqCustomersExport_InvalidFilters400WithoutGateway(t *testing.T) {
	for _, q := range []string{"active=maybe", "debt=bankrupt"} {
		t.Run(q, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/v1/tenants/tnt_1/hq/customers/export?"+q, nil)
			rec := httptest.NewRecorder()

			(&Server{}).handleHqCustomersExport(rec, req)

			if rec.Code != 400 {
				t.Fatalf("status = %d, want 400 (query %q)", rec.Code, q)
			}
		})
	}
}

// handleHqCustomersImport rejects a non-multipart body before touching
// claims or the hq service.
func TestHandleHqCustomersImport_RejectsNonMultipart400WithoutGateway(t *testing.T) {
	req := httptest.NewRequest("POST", "/v1/tenants/tnt_1/hq/customers/import", strings.NewReader("name,phone1,branch_id\n"))
	req.Header.Set("Content-Type", "text/csv")
	rec := httptest.NewRecorder()

	(&Server{}).handleHqCustomersImport(rec, req)

	if rec.Code != 400 {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}
