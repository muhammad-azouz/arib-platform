package httpapi

import (
	"errors"
	"net/http"

	"github.com/aribpos/license-api/internal/device"
)

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	c := claimsFrom(r.Context())
	view, err := s.admin.GetClient(r.Context(), c.Subject)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "could not load account")
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) handleBind(w http.ResponseWriter, r *http.Request) {
	c := claimsFrom(r.Context())
	var req struct {
		MachineID   string `json:"machine_id"`
		MachineName string `json:"machine_name"`
		OS          string `json:"os"`
		// Optional; sending it marks the client as 6-field-token capable
		// (update entitlement — desktop/tasks/spec-app-updates.md).
		AppVersion string `json:"app_version"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	res, err := s.device.Bind(r.Context(), c.Subject, req.MachineID, req.MachineName, req.OS, req.AppVersion)
	if err != nil {
		s.writeDeviceError(w, err, res)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleValidate(w http.ResponseWriter, r *http.Request) {
	c := claimsFrom(r.Context())
	var req struct {
		MachineID string `json:"machine_id"`
		// Optional; see handleBind.
		AppVersion string `json:"app_version"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	res, err := s.device.Validate(r.Context(), c.Subject, req.MachineID, req.AppVersion)
	if err != nil {
		s.writeDeviceError(w, err, res)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleRelease(w http.ResponseWriter, r *http.Request) {
	c := claimsFrom(r.Context())
	var req struct {
		DeviceID string `json:"device_id"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if err := s.device.Release(r.Context(), c.Subject, req.DeviceID, true); err != nil {
		s.writeDeviceError(w, err, nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "released"})
}

func (s *Server) writeDeviceError(w http.ResponseWriter, err error, res *device.Result) {
	var notEntitled *device.VersionNotEntitledError
	switch {
	case errors.As(err, &notEntitled):
		// Typed refusal (spec: revalidation enforcement). The client surfaces
		// updates_until + max_entitled_version in its license-nag UI.
		body := map[string]any{
			"code":                 "version_not_entitled",
			"error":                "this app version is not covered by the license's update plan — renew, or reinstall an entitled version",
			"updates_until":        notEntitled.UpdatesUntil,
			"max_entitled_version": notEntitled.MaxEntitledVersion,
		}
		if res != nil {
			// issue() still mints a token reflecting the license's *current*
			// state even on refusal — the client caches it so license.lic (and
			// the feed's own Authorization token, sourced from that same file)
			// isn't stuck on the last-entitled snapshot forever.
			body["license"] = res.License
			body["license_id"] = res.LicenseID
			body["revalidate_by"] = res.RevalidateBy
			body["hard_expiry"] = res.HardExpiry
		}
		writeJSON(w, http.StatusForbidden, body)
	case errors.Is(err, device.ErrNoLicense):
		writeErr(w, http.StatusPaymentRequired, "no available license to bind — contact support")
	case errors.Is(err, device.ErrTrialUsed):
		writeErr(w, http.StatusForbidden, "this device has already used a free trial")
	case errors.Is(err, device.ErrNotBound):
		writeErr(w, http.StatusNotFound, "this device is not bound to a license")
	case errors.Is(err, device.ErrLicenseInactive):
		writeJSON(w, http.StatusForbidden, map[string]string{"code": "license_inactive", "error": "license is suspended or expired"})
	case errors.Is(err, device.ErrCooldown):
		writeErr(w, http.StatusTooManyRequests, "device released too recently; try again later")
	case errors.Is(err, device.ErrReleaseLimit):
		writeErr(w, http.StatusTooManyRequests, "release limit reached for this period")
	case errors.Is(err, device.ErrForbidden):
		writeErr(w, http.StatusForbidden, "device does not belong to this account")
	default:
		writeErr(w, http.StatusInternalServerError, "request failed")
	}
}
