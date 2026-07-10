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
	})

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
