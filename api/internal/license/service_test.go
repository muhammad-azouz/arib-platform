package license

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/aribpos/license-api/internal/model"
	"github.com/aribpos/license-api/pkg/licensetoken"
)

func loadSigner(t *testing.T) *licensetoken.Signer {
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

func TestTokenForPerpetualPaid(t *testing.T) {
	svc := &Service{signer: loadSigner(t), clocks: Clocks{RevalidateAfter: 14 * 24 * time.Hour, HardExpireAfter: 28 * 24 * time.Hour}}
	l := &model.License{ID: "lic_1", Modules: []string{"sales", "accounting"}, ExpiresAt: nil}
	tok, reval, hard, err := svc.TokenFor(l, "machine-1")
	if err != nil {
		t.Fatalf("TokenFor: %v", err)
	}
	if tok == "" {
		t.Fatal("expected non-empty token")
	}
	now := time.Now().UTC()
	if hard.Sub(now) < 99*365*24*time.Hour {
		t.Fatalf("expected far-future hard expiry, got %v", hard)
	}
	if reval.After(now.Add(time.Minute)) {
		t.Fatalf("expected reval ~= now, got %v", reval)
	}
}

func TestTokenForTrial(t *testing.T) {
	svc := &Service{signer: loadSigner(t), clocks: Clocks{RevalidateAfter: 14 * 24 * time.Hour, TrialDuration: 7 * 24 * time.Hour}}
	end := time.Now().UTC().Add(7 * 24 * time.Hour)
	l := &model.License{ID: "lic_2", Modules: model.AllModules, ExpiresAt: &end}
	_, reval, hard, err := svc.TokenFor(l, "machine-1")
	if err != nil {
		t.Fatalf("TokenFor: %v", err)
	}
	if !hard.Equal(end) || !reval.Equal(end) {
		t.Fatalf("expected both clocks == trial end, got hard=%v reval=%v end=%v", hard, reval, end)
	}
}

func TestEncodeModulesFallsBackToAll(t *testing.T) {
	got := encodeModules(nil)
	if !strings.HasPrefix(got, "v1:") {
		t.Fatalf("expected v1: prefix, got %q", got)
	}
	for _, m := range model.AllModules {
		if !strings.Contains(got, m) {
			t.Fatalf("expected %q to contain module %q", got, m)
		}
	}
}

func TestUsable(t *testing.T) {
	future := time.Now().UTC().Add(time.Hour)
	past := time.Now().UTC().Add(-time.Hour)
	cases := []struct {
		name string
		l    *model.License
		want bool
	}{
		{"perpetual active", &model.License{Status: model.LicenseActive, ExpiresAt: nil}, true},
		{"perpetual suspended", &model.License{Status: model.LicenseSuspended, ExpiresAt: nil}, false},
		{"dated active not expired", &model.License{Status: model.LicenseActive, ExpiresAt: &future}, true},
		{"dated active expired", &model.License{Status: model.LicenseActive, ExpiresAt: &past}, false},
	}
	for _, c := range cases {
		if got := Usable(c.l); got != c.want {
			t.Errorf("%s: Usable() = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestNormalizeModulesRejectsUnknown(t *testing.T) {
	if _, err := model.NormalizeModules([]string{"sales", "bogus"}); err == nil {
		t.Fatal("expected error for unknown module")
	}
}

func TestNormalizeModulesDedupesAndLowercases(t *testing.T) {
	got, err := model.NormalizeModules([]string{"Sales", "sales", " accounting "})
	if err != nil {
		t.Fatalf("NormalizeModules: %v", err)
	}
	if len(got) != 2 || got[0] != "sales" || got[1] != "accounting" {
		t.Fatalf("unexpected result: %v", got)
	}
}
