package httpapi

import (
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
)

// handleUpdates serves the Velopack update feed: static files under
// updatesDir, laid out {channel}/{rid}/<file> (manifest, packages, installer,
// changelog — see tasks/spec-app-updates.md in the desktop repo). Directory
// listings are disabled; only plain files are served. The entitlement gate
// (token-filtered manifest, gated packages) ships separately behind
// UPDATES_AUTH.
func (s *Server) handleUpdates(w http.ResponseWriter, r *http.Request) {
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
	// ServeFile handles Range requests (package downloads), HEAD and
	// Content-Type detection.
	http.ServeFile(w, r, fp)
}
