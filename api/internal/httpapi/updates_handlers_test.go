package httpapi

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func newUpdatesTestServer(t *testing.T, updatesDir string) http.Handler {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return New(nil, nil, nil, nil, nil, nil, log, updatesDir).Router()
}

func TestUpdatesFeed(t *testing.T) {
	dir := t.TempDir()
	feed := filepath.Join(dir, "lts", "win-x64")
	if err := os.MkdirAll(feed, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `{"Assets":[]}`
	if err := os.WriteFile(filepath.Join(feed, "releases.lts.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(feed, "AribONE-2.0.1-lts-full.nupkg"), []byte("pkgbytes"), 0o644); err != nil {
		t.Fatal(err)
	}

	h := newUpdatesTestServer(t, dir)

	get := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec
	}

	t.Run("manifest served", func(t *testing.T) {
		rec := get("/updates/lts/win-x64/releases.lts.json")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		if rec.Body.String() != manifest {
			t.Fatalf("body = %q, want manifest", rec.Body.String())
		}
	})

	t.Run("package served", func(t *testing.T) {
		rec := get("/updates/lts/win-x64/AribONE-2.0.1-lts-full.nupkg")
		if rec.Code != http.StatusOK || rec.Body.String() != "pkgbytes" {
			t.Fatalf("status = %d body = %q", rec.Code, rec.Body.String())
		}
	})

	t.Run("missing file 404s", func(t *testing.T) {
		if rec := get("/updates/lts/win-x64/nope.nupkg"); rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", rec.Code)
		}
	})

	t.Run("directory listing 404s", func(t *testing.T) {
		for _, p := range []string{"/updates/lts", "/updates/lts/win-x64", "/updates/lts/win-x64/"} {
			if rec := get(p); rec.Code != http.StatusNotFound {
				t.Fatalf("%s: status = %d, want 404", p, rec.Code)
			}
		}
	})

	t.Run("path traversal 404s", func(t *testing.T) {
		// A secret outside the feed root must not be reachable.
		if err := os.WriteFile(filepath.Join(dir, "..", "secret.txt"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		for _, p := range []string{
			"/updates/../secret.txt",
			"/updates/lts/../../secret.txt",
			"/updates/%2e%2e/secret.txt",
		} {
			req := httptest.NewRequest(http.MethodGet, p, nil)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code == http.StatusOK {
				t.Fatalf("%s: served a file outside the feed root", p)
			}
		}
	})

	t.Run("unconfigured dir 404s", func(t *testing.T) {
		h := newUpdatesTestServer(t, "")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/updates/lts/win-x64/releases.lts.json", nil))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", rec.Code)
		}
	})
}
