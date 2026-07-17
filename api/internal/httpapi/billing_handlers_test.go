package httpapi

import (
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aribpos/license-api/internal/billing"
	mongostore "github.com/aribpos/license-api/internal/store/mongo"
)

// handleAdminCreateBill decodes before touching claims or the billing
// service, so a malformed body 400s without any service wiring — a bare
// &Server{} would panic if the handler ever reached s.billing.
func TestHandleAdminCreateBill_InvalidBody400WithoutService(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"malformed json", `not json`},
		{"unknown field", `{"amount":100,"unexpected":true}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/v1/admin/tenants/tnt_1/bills", strings.NewReader(tc.body))
			rec := httptest.NewRecorder()

			(&Server{}).handleAdminCreateBill(rec, req)

			if rec.Code != 400 {
				t.Fatalf("status = %d, want 400 (body %q)", rec.Code, tc.body)
			}
		})
	}
}

// handleAdminVoidBill: same zero-service guarantee for a malformed body.
func TestHandleAdminVoidBill_InvalidBody400WithoutService(t *testing.T) {
	req := httptest.NewRequest("POST", "/v1/admin/bills/bil_1/void", strings.NewReader(`not json`))
	rec := httptest.NewRecorder()

	(&Server{}).handleAdminVoidBill(rec, req)

	if rec.Code != 400 {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

// writeBillingError must map each billing sentinel to the status the admin
// panel and its own retry logic expect: validation mistakes are the caller's
// fault (400), missing tenants/bills are 404, anything else is a 500.
func TestWriteBillingError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"invalid amount", billing.ErrInvalidAmount, 400},
		{"invalid period", billing.ErrInvalidPeriod, 400},
		{"void reason required", billing.ErrVoidReasonRequired, 400},
		{"bill not paid", billing.ErrBillNotPaid, 400},
		{"not found", mongostore.ErrNotFound, 404},
		{"billing not found alias", billing.ErrNotFound, 404},
		{"wrapped not found", errors.Join(errors.New("context"), mongostore.ErrNotFound), 404},
		{"unknown error", errors.New("boom"), 500},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			(&Server{}).writeBillingError(rec, tc.err)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d", rec.Code, tc.want)
			}
		})
	}
}
