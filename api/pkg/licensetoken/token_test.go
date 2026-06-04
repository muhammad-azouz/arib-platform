package licensetoken

import (
	"os"
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

func TestLegacyThreeFieldDecodes(t *testing.T) {
	if _, err := decodePayload("machineX|Trial|" + time.Now().UTC().Format(time.RFC3339)); err != nil {
		t.Fatalf("legacy decode failed: %v", err)
	}
}
