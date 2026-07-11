package httpapi

import (
	"encoding/json"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/aribpos/license-api/pkg/licensetoken"
	"github.com/go-chi/chi/v5"
)

// The Velopack update feed with the entitlement gate
// (desktop/tasks/spec-app-updates.md). Static files under updatesDir, laid out
// {channel}/{rid}/<file>. Per file class, when the gate is on:
//
//   - changelog.{channel}.json — free. The upsell surface: every machine can
//     always see what exists; it also doubles as the version→publish-date map.
//   - Setup.exe / Portable.zip — free, same as changelog. A browser download
//     (in-app "download installer" handoff, or a website link) can't attach a
//     token, and gating them added nothing: the actual entitlement boundary is
//     the auto-update feed below (which stays gated) plus version_not_entitled
//     on validate/bind — an installer of the head is no more of a bypass than
//     any commercial installer being downloadable without a license.
//   - releases.{channel}.json — requires a valid license token; the served
//     manifest is FILTERED to releases published inside the token's
//     updatesUntil window, so the client's Velopack naturally targets the
//     newest entitled version.
//   - *.nupkg — requires a token; 403 when the package's publish date is past
//     the window (defense in depth behind the filtered manifest).
//   - anything else (RELEASES-*, assets.*.json) — requires a token; metadata
//     only, no date check.
//
// Fail-open rule: a version absent from the changelog (pre-changelog fleet,
// odd filenames) is never blocked, and a token without updatesUntil
// (grandfathered) sees everything. Directory listings stay disabled.
func (s *Server) handleUpdates(w http.ResponseWriter, r *http.Request) {
	// releases.{channel}.json is filtered per-license (serveFilteredManifest) —
	// a shared cache (Cloudflare et al.) sitting in front of this route must
	// never cache any response from it, or one client's entitlement-filtered
	// view can leak to another. Set unconditionally, before any branch below.
	w.Header().Set("Cache-Control", "no-store")

	if s.updatesDir == "" {
		writeErr(w, http.StatusNotFound, "updates feed not configured")
		return
	}

	rel := chi.URLParam(r, "*")
	// path.Clean on a rooted copy collapses any ".." before it can escape;
	// the prefix check below is defense in depth (symlinks, odd separators).
	fp := filepath.Join(s.updatesDir, filepath.FromSlash(path.Clean("/"+rel)))
	root := filepath.Clean(s.updatesDir) + string(os.PathSeparator)
	if !strings.HasPrefix(fp, root) {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}

	st, err := os.Stat(fp)
	if err != nil || st.IsDir() {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}

	base := filepath.Base(fp)
	channel := strings.SplitN(path.Clean("/"+rel), "/", 3)[1] // {channel}/{rid}/file

	gateOff := !s.updatesAuth || s.tokenVerifier == nil
	isChangelog := base == "changelog."+channel+".json"
	isInstaller := strings.HasSuffix(base, "Setup.exe") || strings.HasSuffix(base, "Portable.zip")

	if gateOff || isChangelog || isInstaller {
		http.ServeFile(w, r, fp)
		return
	}

	tok, ok := s.verifyFeedToken(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "license token required")
		return
	}

	// Grandfathered (no updatesUntil in the token) ⇒ unlimited entitlement.
	if tok.UpdatesUntil == nil {
		http.ServeFile(w, r, fp)
		return
	}
	until := *tok.UpdatesUntil

	switch {
	case base == "releases."+channel+".json":
		s.serveFilteredManifest(w, fp, filepath.Dir(fp), channel, until)
	case strings.HasSuffix(base, ".nupkg"):
		if v, found := nupkgVersion(base, channel); found {
			if date, known := s.publishDate(filepath.Dir(fp), channel, v); known && date.After(until) {
				writeErr(w, http.StatusForbidden, "version not covered by this license's update plan")
				return
			}
		}
		http.ServeFile(w, r, fp)
	default:
		// Metadata (RELEASES-*, assets.*.json): token was enough.
		http.ServeFile(w, r, fp)
	}
}

// verifyFeedToken checks the Authorization header carries a validly signed
// license token. Only the signature is checked — entitlement is independent
// of license validity (an expired license may still reinstall in-window
// releases), so hardExpiry/machine binding are deliberately not enforced here.
func (s *Server) verifyFeedToken(r *http.Request) (licensetoken.Payload, bool) {
	h := strings.TrimSpace(r.Header.Get("Authorization"))
	h = strings.TrimPrefix(h, "Bearer ")
	if h == "" {
		return licensetoken.Payload{}, false
	}
	p, err := s.tokenVerifier.Verify(h)
	if err != nil {
		return licensetoken.Payload{}, false
	}
	return p, true
}

// serveFilteredManifest rewrites releases.{channel}.json to only the assets
// whose version was published inside the entitlement window. Versions unknown
// to the changelog fail open (kept).
func (s *Server) serveFilteredManifest(w http.ResponseWriter, fp, dir, channel string, until time.Time) {
	raw, err := os.ReadFile(fp)
	if err != nil {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil {
		writeErr(w, http.StatusInternalServerError, "malformed manifest")
		return
	}
	var assets []json.RawMessage
	_ = json.Unmarshal(doc["Assets"], &assets)

	dates := s.changelogDates(dir, channel)
	kept := make([]json.RawMessage, 0, len(assets))
	for _, a := range assets {
		var meta struct {
			Version string `json:"Version"`
		}
		if err := json.Unmarshal(a, &meta); err == nil {
			if date, known := dates[meta.Version]; known && date.After(until) {
				continue
			}
		}
		kept = append(kept, a)
	}
	filtered, err := json.Marshal(kept)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "filter failed")
		return
	}
	doc["Assets"] = filtered
	out, err := json.Marshal(doc)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "filter failed")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(out)
}

// changelogDates loads the channel changelog's version → publish-date map.
// Missing/malformed changelog yields an empty map — every lookup then fails
// open, which is the required behavior for pre-changelog feeds.
func (s *Server) changelogDates(dir, channel string) map[string]time.Time {
	raw, err := os.ReadFile(filepath.Join(dir, "changelog."+channel+".json"))
	if err != nil {
		return nil
	}
	var entries []struct {
		Version        string    `json:"version"`
		PublishedAtUtc time.Time `json:"publishedAtUtc"`
	}
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil
	}
	m := make(map[string]time.Time, len(entries))
	for _, e := range entries {
		m[e.Version] = e.PublishedAtUtc
	}
	return m
}

// publishDate resolves one version's publish date from the changelog.
func (s *Server) publishDate(dir, channel, version string) (time.Time, bool) {
	d, ok := s.changelogDates(dir, channel)[version]
	return d, ok
}

// nupkgVersion extracts the semver from a Velopack package filename:
// {PackId}-{version}-{channel}-{full|delta}.nupkg. Unparseable names report
// not-found and fail open.
func nupkgVersion(base, channel string) (string, bool) {
	name := strings.TrimSuffix(base, ".nupkg")
	for _, suffix := range []string{"-" + channel + "-full", "-" + channel + "-delta"} {
		if strings.HasSuffix(name, suffix) {
			v := strings.TrimSuffix(name, suffix)
			if i := strings.IndexByte(v, '-'); i > 0 {
				return v[i+1:], true
			}
		}
	}
	return "", false
}
