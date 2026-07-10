package httpapi

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/aribpos/license-api/pkg/licensetoken"
)

func newUpdatesTestServer(t *testing.T, updatesDir string, auth bool, verifier *licensetoken.Signer) http.Handler {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return New(nil, nil, nil, nil, nil, nil, log, updatesDir, auth, verifier).Router()
}

func loadFeedSigner(t *testing.T) *licensetoken.Signer {
	t.Helper()
	xml, err := os.ReadFile("../../keys/PrivateKey.xml")
	if err != nil {
		t.Skipf("private key not available: %v", err)
	}
	s, err := licensetoken.NewSigner(string(xml))
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}
	return s
}

// writeFeed lays out a two-release lts feed: 2.0.1 published long ago,
// 2.0.2 published recently.
func writeFeed(t *testing.T) (dir string, oldDate, newDate time.Time) {
	t.Helper()
	dir = t.TempDir()
	feed := filepath.Join(dir, "lts", "win-x64")
	if err := os.MkdirAll(feed, 0o755); err != nil {
		t.Fatal(err)
	}
	oldDate = time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC)
	newDate = time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)

	manifest := `{"Assets":[
		{"PackageId":"AribONE","Version":"2.0.2","Type":"Full","FileName":"AribONE-2.0.2-lts-full.nupkg"},
		{"PackageId":"AribONE","Version":"2.0.2","Type":"Delta","FileName":"AribONE-2.0.2-lts-delta.nupkg"},
		{"PackageId":"AribONE","Version":"2.0.1","Type":"Full","FileName":"AribONE-2.0.1-lts-full.nupkg"}
	]}`
	changelog := fmt.Sprintf(`[
		{"version":"2.0.2","publishedAtUtc":%q,"notesMarkdown":"new"},
		{"version":"2.0.1","publishedAtUtc":%q,"notesMarkdown":"old"}
	]`, newDate.Format(time.RFC3339), oldDate.Format(time.RFC3339))

	files := map[string]string{
		"releases.lts.json":            manifest,
		"changelog.lts.json":           changelog,
		"AribONE-2.0.1-lts-full.nupkg": "pkg-201",
		"AribONE-2.0.2-lts-full.nupkg": "pkg-202",
		"AribONE-lts-Setup.exe":        "setup-bytes",
		"RELEASES-lts":                 "meta",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(feed, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir, oldDate, newDate
}

func feedToken(t *testing.T, s *licensetoken.Signer, updatesUntil *time.Time) string {
	t.Helper()
	tok, err := s.Sign(licensetoken.Payload{
		MachineID:    "m1",
		Features:     "v1:sales",
		HardExpiry:   time.Now().Add(24 * time.Hour),
		RevalidateBy: time.Now().Add(12 * time.Hour),
		LicenseID:    "lic_t",
		UpdatesUntil: updatesUntil,
	})
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	return tok
}

func feedGet(h http.Handler, p, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, p, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func manifestVersions(t *testing.T, body []byte) []string {
	t.Helper()
	var doc struct {
		Assets []struct {
			Version string
			Type    string
		}
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatalf("parse manifest: %v (%s)", err, body)
	}
	var out []string
	for _, a := range doc.Assets {
		out = append(out, a.Version+":"+a.Type)
	}
	return out
}

func TestUpdatesFeedGate(t *testing.T) {
	signer := loadFeedSigner(t)
	dir, oldDate, _ := writeFeed(t)
	h := newUpdatesTestServer(t, dir, true, signer)

	// Entitlement windows: lapsed = between the two releases (only 2.0.1
	// entitled); fresh = after both.
	lapsed := oldDate.Add(24 * time.Hour)
	fresh := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	lapsedTok := feedToken(t, signer, &lapsed)
	freshTok := feedToken(t, signer, &fresh)
	grandTok := feedToken(t, signer, nil)

	t.Run("changelog is free", func(t *testing.T) {
		if rec := feedGet(h, "/updates/lts/win-x64/changelog.lts.json", ""); rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
	})

	t.Run("manifest without token 401s", func(t *testing.T) {
		if rec := feedGet(h, "/updates/lts/win-x64/releases.lts.json", ""); rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", rec.Code)
		}
	})

	t.Run("garbage token 401s", func(t *testing.T) {
		if rec := feedGet(h, "/updates/lts/win-x64/releases.lts.json", "not.a.token"); rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", rec.Code)
		}
	})

	t.Run("lapsed token gets filtered manifest", func(t *testing.T) {
		rec := feedGet(h, "/updates/lts/win-x64/releases.lts.json", lapsedTok)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		got := manifestVersions(t, rec.Body.Bytes())
		if len(got) != 1 || got[0] != "2.0.1:Full" {
			t.Fatalf("filtered manifest = %v, want only 2.0.1:Full", got)
		}
	})

	t.Run("in-window token gets full manifest", func(t *testing.T) {
		rec := feedGet(h, "/updates/lts/win-x64/releases.lts.json", freshTok)
		if got := manifestVersions(t, rec.Body.Bytes()); len(got) != 3 {
			t.Fatalf("manifest = %v, want all 3 assets", got)
		}
	})

	t.Run("grandfathered token gets full manifest", func(t *testing.T) {
		rec := feedGet(h, "/updates/lts/win-x64/releases.lts.json", grandTok)
		if got := manifestVersions(t, rec.Body.Bytes()); len(got) != 3 {
			t.Fatalf("manifest = %v, want all 3 assets", got)
		}
	})

	t.Run("unentitled package 403s, entitled serves", func(t *testing.T) {
		if rec := feedGet(h, "/updates/lts/win-x64/AribONE-2.0.2-lts-full.nupkg", lapsedTok); rec.Code != http.StatusForbidden {
			t.Fatalf("2.0.2 status = %d, want 403", rec.Code)
		}
		rec := feedGet(h, "/updates/lts/win-x64/AribONE-2.0.1-lts-full.nupkg", lapsedTok)
		if rec.Code != http.StatusOK || rec.Body.String() != "pkg-201" {
			t.Fatalf("2.0.1 status = %d body = %q", rec.Code, rec.Body.String())
		}
	})

	t.Run("package without token 401s", func(t *testing.T) {
		if rec := feedGet(h, "/updates/lts/win-x64/AribONE-2.0.1-lts-full.nupkg", ""); rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", rec.Code)
		}
	})

	t.Run("Setup.exe gated by channel head", func(t *testing.T) {
		if rec := feedGet(h, "/updates/lts/win-x64/AribONE-lts-Setup.exe", lapsedTok); rec.Code != http.StatusForbidden {
			t.Fatalf("lapsed status = %d, want 403", rec.Code)
		}
		if rec := feedGet(h, "/updates/lts/win-x64/AribONE-lts-Setup.exe", freshTok); rec.Code != http.StatusOK {
			t.Fatalf("fresh status = %d, want 200", rec.Code)
		}
	})

	t.Run("metadata needs token but no date check", func(t *testing.T) {
		if rec := feedGet(h, "/updates/lts/win-x64/RELEASES-lts", ""); rec.Code != http.StatusUnauthorized {
			t.Fatalf("no-token status = %d, want 401", rec.Code)
		}
		if rec := feedGet(h, "/updates/lts/win-x64/RELEASES-lts", lapsedTok); rec.Code != http.StatusOK {
			t.Fatalf("lapsed status = %d, want 200", rec.Code)
		}
	})

	t.Run("missing changelog fails open", func(t *testing.T) {
		if err := os.Remove(filepath.Join(dir, "lts", "win-x64", "changelog.lts.json")); err != nil {
			t.Fatal(err)
		}
		defer writeFeedChangelogBack(t, dir, oldDate)
		rec := feedGet(h, "/updates/lts/win-x64/AribONE-2.0.2-lts-full.nupkg", lapsedTok)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (fail open without changelog)", rec.Code)
		}
		rec = feedGet(h, "/updates/lts/win-x64/releases.lts.json", lapsedTok)
		if got := manifestVersions(t, rec.Body.Bytes()); len(got) != 3 {
			t.Fatalf("manifest = %v, want all 3 (fail open)", got)
		}
	})
}

func writeFeedChangelogBack(t *testing.T, dir string, oldDate time.Time) {
	t.Helper()
	changelog := fmt.Sprintf(`[{"version":"2.0.1","publishedAtUtc":%q,"notesMarkdown":"old"}]`,
		oldDate.Format(time.RFC3339))
	_ = os.WriteFile(filepath.Join(dir, "lts", "win-x64", "changelog.lts.json"), []byte(changelog), 0o644)
}

func TestUpdatesFeedAuthOff(t *testing.T) {
	dir, _, _ := writeFeed(t)
	h := newUpdatesTestServer(t, dir, false, nil)

	for _, p := range []string{
		"/updates/lts/win-x64/releases.lts.json",
		"/updates/lts/win-x64/AribONE-2.0.2-lts-full.nupkg",
		"/updates/lts/win-x64/AribONE-lts-Setup.exe",
	} {
		if rec := feedGet(h, p, ""); rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d, want 200 with auth off", p, rec.Code)
		}
	}
	rec := feedGet(h, "/updates/lts/win-x64/releases.lts.json", "")
	if got := manifestVersions(t, rec.Body.Bytes()); len(got) != 3 {
		t.Fatalf("manifest = %v, want unfiltered with auth off", got)
	}
}

func TestUpdatesFeedStatics(t *testing.T) {
	dir, _, _ := writeFeed(t)
	h := newUpdatesTestServer(t, dir, false, nil)

	t.Run("missing file 404s", func(t *testing.T) {
		if rec := feedGet(h, "/updates/lts/win-x64/nope.nupkg", ""); rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", rec.Code)
		}
	})

	t.Run("directory listing 404s", func(t *testing.T) {
		for _, p := range []string{"/updates/lts", "/updates/lts/win-x64", "/updates/lts/win-x64/"} {
			if rec := feedGet(h, p, ""); rec.Code != http.StatusNotFound {
				t.Fatalf("%s: status = %d, want 404", p, rec.Code)
			}
		}
	})

	t.Run("path traversal 404s", func(t *testing.T) {
		if err := os.WriteFile(filepath.Join(dir, "..", "secret.txt"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		for _, p := range []string{
			"/updates/../secret.txt",
			"/updates/lts/../../secret.txt",
			"/updates/%2e%2e/secret.txt",
		} {
			if rec := feedGet(h, p, ""); rec.Code == http.StatusOK {
				t.Fatalf("%s: served a file outside the feed root", p)
			}
		}
	})

	t.Run("unconfigured dir 404s", func(t *testing.T) {
		h := newUpdatesTestServer(t, "", false, nil)
		if rec := feedGet(h, "/updates/lts/win-x64/releases.lts.json", ""); rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", rec.Code)
		}
	})

	t.Run("nupkg version parsing", func(t *testing.T) {
		if v, ok := nupkgVersion("AribONE-2.0.2-lts-full.nupkg", "lts"); !ok || v != "2.0.2" {
			t.Fatalf("full: got %q %v", v, ok)
		}
		if v, ok := nupkgVersion("AribONE-2.0.2-lts-delta.nupkg", "lts"); !ok || v != "2.0.2" {
			t.Fatalf("delta: got %q %v", v, ok)
		}
		if v, ok := nupkgVersion("AribONE-2.1.0-canary.1-canary-full.nupkg", "canary"); !ok || v != "2.1.0-canary.1" {
			t.Fatalf("canary: got %q %v", v, ok)
		}
		if _, ok := nupkgVersion("weird-name.nupkg", "lts"); ok {
			t.Fatal("unparseable name must report not-found (fail open)")
		}
	})
}
