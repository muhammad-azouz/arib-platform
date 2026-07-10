package updates

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeFeed(t *testing.T, root, channel, body string) {
	t.Helper()
	dir := filepath.Join(root, channel, "win-x64")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "changelog."+channel+".json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestResolver(t *testing.T) {
	root := t.TempDir()
	writeFeed(t, root, "lts", `[
		{"version":"2.1.0","publishedAtUtc":"2026-07-01T00:00:00Z","notesMarkdown":"new"},
		{"version":"2.0.0","publishedAtUtc":"2026-01-01T00:00:00Z","notesMarkdown":"old"}
	]`)
	writeFeed(t, root, "canary", `[
		{"version":"2.2.0-canary.1","publishedAtUtc":"2026-07-05T00:00:00Z","notesMarkdown":"edge"}
	]`)

	r := NewResolver(root)

	t.Run("publish date across channels", func(t *testing.T) {
		d, ok := r.PublishDate("2.0.0")
		if !ok || !d.Equal(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)) {
			t.Fatalf("lts lookup = %v, %v", d, ok)
		}
		if _, ok := r.PublishDate("2.2.0-canary.1"); !ok {
			t.Fatal("canary version should resolve via the union scan")
		}
	})

	t.Run("unknown version fails open", func(t *testing.T) {
		if _, ok := r.PublishDate("9.9.9"); ok {
			t.Fatal("unknown version must report not-found")
		}
	})

	t.Run("max entitled is latest-published in window", func(t *testing.T) {
		until := time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC)
		v, ok := r.MaxEntitledVersion(until)
		if !ok || v != "2.1.0" {
			t.Fatalf("MaxEntitledVersion = %q, %v; want 2.1.0", v, ok)
		}
		// Window before every release ⇒ nothing entitled.
		if _, ok := r.MaxEntitledVersion(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)); ok {
			t.Fatal("pre-history window must report no entitled version")
		}
	})

	t.Run("nil resolver disables lookups", func(t *testing.T) {
		var nilR *Resolver = NewResolver("")
		if _, ok := nilR.PublishDate("2.0.0"); ok {
			t.Fatal("nil resolver must report not-found")
		}
		if _, ok := nilR.MaxEntitledVersion(time.Now()); ok {
			t.Fatal("nil resolver must report no entitled version")
		}
	})

	t.Run("malformed changelog is skipped", func(t *testing.T) {
		broken := t.TempDir()
		writeFeed(t, broken, "lts", `{not json`)
		if _, ok := NewResolver(broken).PublishDate("2.0.0"); ok {
			t.Fatal("malformed changelog must fail open")
		}
	})
}
