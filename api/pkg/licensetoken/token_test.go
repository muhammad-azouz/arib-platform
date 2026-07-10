package licensetoken

import (
	"encoding/base64"
	"os"
	"strings"
	"testing"
	"time"
)

func loadSigner(t *testing.T) *Signer {
	t.Helper()
	xml, err := os.ReadFile("../../keys/PrivateKey.xml")
	if err != nil {
		t.Skipf("private key not available: %v", err)
	}
	s, err := NewSigner(string(xml))
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}
	return s
}

func TestSignVerifyRoundTrip(t *testing.T) {
	s := loadSigner(t)
	now := time.Now().UTC().Truncate(time.Second)
	in := Payload{
		MachineID:    "abc123machine",
		Features:     "Pro",
		HardExpiry:   now.Add(28 * 24 * time.Hour),
		RevalidateBy: now.Add(14 * 24 * time.Hour),
		LicenseID:    "lic_0001",
	}
	lic, err := s.Sign(in)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	out, err := s.Verify(lic)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if out.MachineID != in.MachineID || out.Features != in.Features || out.LicenseID != in.LicenseID {
		t.Fatalf("payload mismatch: %+v != %+v", out, in)
	}
	if !out.HardExpiry.Equal(in.HardExpiry) || !out.RevalidateBy.Equal(in.RevalidateBy) {
		t.Fatalf("time mismatch: %v/%v", out.HardExpiry, out.RevalidateBy)
	}
}

func TestTamperFails(t *testing.T) {
	s := loadSigner(t)
	lic, err := s.Sign(Payload{MachineID: "m", Features: "Trial",
		HardExpiry: time.Now().Add(time.Hour), RevalidateBy: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	// Flip a character in the payload segment.
	b := []byte(lic)
	b[0] ^= 0x01
	if _, err := s.Verify(string(b)); err == nil {
		t.Fatal("expected verification failure on tampered license")
	}
}

func TestSignVerifyRoundTripModules(t *testing.T) {
	s := loadSigner(t)
	now := time.Now().UTC().Truncate(time.Second)
	in := Payload{
		MachineID:    "abc123machine",
		Features:     "v1:sales,accounting",
		HardExpiry:   now.AddDate(100, 0, 0),
		RevalidateBy: now,
		LicenseID:    "lic_0002",
	}
	lic, err := s.Sign(in)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	out, err := s.Verify(lic)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if out.Features != in.Features {
		t.Fatalf("features mismatch: %q != %q", out.Features, in.Features)
	}
	if !out.HardExpiry.Equal(in.HardExpiry) || !out.RevalidateBy.Equal(in.RevalidateBy) {
		t.Fatalf("time mismatch: %v/%v", out.HardExpiry, out.RevalidateBy)
	}
}

func TestLegacyThreeFieldDecodes(t *testing.T) {
	if _, err := decodePayload("machineX|Trial|" + time.Now().UTC().Format(time.RFC3339)); err != nil {
		t.Fatalf("legacy decode failed: %v", err)
	}
}

func TestSignVerifyRoundTripUpdatesUntil(t *testing.T) {
	s := loadSigner(t)
	until := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	in := Payload{
		MachineID:    "machine-6f",
		Features:     "v1:sales",
		HardExpiry:   time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
		RevalidateBy: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC),
		LicenseID:    "lic_6f",
		UpdatesUntil: &until,
	}
	tok, err := s.Sign(in)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	// The envelope must literally contain 6 pipe-delimited fields.
	rawB64 := strings.Split(tok, ".")[0]
	raw, err := base64.StdEncoding.DecodeString(rawB64)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got := len(strings.Split(string(raw), "|")); got != 6 {
		t.Fatalf("expected 6 payload fields, got %d (%s)", got, raw)
	}
	out, err := s.Verify(tok)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if out.UpdatesUntil == nil || !out.UpdatesUntil.Equal(until) {
		t.Fatalf("UpdatesUntil mismatch: %v", out.UpdatesUntil)
	}

	// Omitting UpdatesUntil must produce the legacy 5-field payload.
	in.UpdatesUntil = nil
	tok, err = s.Sign(in)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	rawB64 = strings.Split(tok, ".")[0]
	raw, _ = base64.StdEncoding.DecodeString(rawB64)
	if got := len(strings.Split(string(raw), "|")); got != 5 {
		t.Fatalf("expected 5 payload fields, got %d (%s)", got, raw)
	}
	out, err = s.Verify(tok)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if out.UpdatesUntil != nil {
		t.Fatalf("expected nil UpdatesUntil, got %v", out.UpdatesUntil)
	}
}
