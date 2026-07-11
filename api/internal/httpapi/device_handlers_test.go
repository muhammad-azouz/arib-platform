package httpapi

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/aribpos/license-api/internal/device"
)

// The version_not_entitled refusal must reach the client as a typed 403 with
// the renew/reinstall details (updates_until + max_entitled_version).
func TestWriteDeviceErrorVersionNotEntitled(t *testing.T) {
	until := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	rec := httptest.NewRecorder()
	(&Server{}).writeDeviceError(rec, &device.VersionNotEntitledError{
		UpdatesUntil:       until,
		MaxEntitledVersion: "2.0.0",
	}, nil)

	if rec.Code != 403 {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	var body struct {
		Code               string    `json:"code"`
		UpdatesUntil       time.Time `json:"updates_until"`
		MaxEntitledVersion string    `json:"max_entitled_version"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Code != "version_not_entitled" || !body.UpdatesUntil.Equal(until) || body.MaxEntitledVersion != "2.0.0" {
		t.Fatalf("body = %+v", body)
	}
}

// A version refusal must still carry the token issue() minted for the
// license's current state — the client caches it so license.lic (and the
// feed's own Authorization header, sourced from that same file) isn't stuck
// on the last-entitled snapshot forever just because the running build can't
// be renewed into (desktop/tasks/cp3-recipe.md section H).
func TestWriteDeviceErrorVersionNotEntitledCarriesToken(t *testing.T) {
	until := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	rec := httptest.NewRecorder()
	(&Server{}).writeDeviceError(rec, &device.VersionNotEntitledError{
		UpdatesUntil:       until,
		MaxEntitledVersion: "2.0.0",
	}, &device.Result{
		License:   "signed-token",
		LicenseID: "lic_abc",
	})

	var body struct {
		Code      string `json:"code"`
		License   string `json:"license"`
		LicenseID string `json:"license_id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Code != "version_not_entitled" || body.License != "signed-token" || body.LicenseID != "lic_abc" {
		t.Fatalf("body = %+v", body)
	}
}
