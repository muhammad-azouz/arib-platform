package device

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/aribpos/license-api/internal/model"
	"github.com/aribpos/license-api/internal/updates"
)

// Entitlement matrix for the version_not_entitled refusal
// (desktop/tasks/spec-app-updates.md): only a changelog-known version
// published after the license's UpdatesUntil is refused; everything else —
// grandfathered license, old client, unknown version, disabled resolver —
// must pass.
func TestCheckVersionEntitled(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "lts", "win-x64")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	changelog := `[
		{"version":"2.1.0","publishedAtUtc":"2026-07-01T00:00:00Z"},
		{"version":"2.0.0","publishedAtUtc":"2026-01-01T00:00:00Z"}
	]`
	if err := os.WriteFile(filepath.Join(dir, "changelog.lts.json"), []byte(changelog), 0o644); err != nil {
		t.Fatal(err)
	}

	svc := &Service{versions: updates.NewResolver(root)}
	until := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC) // covers 2.0.0, not 2.1.0

	lic := func(u *time.Time) *model.License { return &model.License{UpdatesUntil: u} }

	t.Run("known post-window version refused with details", func(t *testing.T) {
		err := svc.checkVersionEntitled(lic(&until), "2.1.0")
		var vne *VersionNotEntitledError
		if !errors.As(err, &vne) {
			t.Fatalf("want VersionNotEntitledError, got %v", err)
		}
		if !vne.UpdatesUntil.Equal(until) || vne.MaxEntitledVersion != "2.0.0" {
			t.Fatalf("details = until %v, max %q; want %v, 2.0.0", vne.UpdatesUntil, vne.MaxEntitledVersion, until)
		}
	})

	t.Run("in-window version passes", func(t *testing.T) {
		if err := svc.checkVersionEntitled(lic(&until), "2.0.0"); err != nil {
			t.Fatalf("in-window version refused: %v", err)
		}
	})

	t.Run("changelog-unknown version fails open", func(t *testing.T) {
		if err := svc.checkVersionEntitled(lic(&until), "3.0.0-dev"); err != nil {
			t.Fatalf("unknown version refused: %v", err)
		}
	})

	t.Run("grandfathered license (nil UpdatesUntil) passes", func(t *testing.T) {
		if err := svc.checkVersionEntitled(lic(nil), "2.1.0"); err != nil {
			t.Fatalf("grandfathered license refused: %v", err)
		}
	})

	t.Run("old client (empty appVersion) passes", func(t *testing.T) {
		if err := svc.checkVersionEntitled(lic(&until), ""); err != nil {
			t.Fatalf("empty appVersion refused: %v", err)
		}
	})

	t.Run("disabled resolver passes", func(t *testing.T) {
		off := &Service{versions: updates.NewResolver("")}
		if err := off.checkVersionEntitled(lic(&until), "2.1.0"); err != nil {
			t.Fatalf("disabled resolver refused: %v", err)
		}
	})
}
