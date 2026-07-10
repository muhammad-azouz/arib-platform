// Package licensetoken encodes, signs and verifies the offline license string
// consumed by the AribPOS desktop app.
//
// Wire format (unchanged envelope from the original implementation):
//
//	base64(raw) "." base64(rsaSig)
//
// where raw is a pipe-delimited payload and rsaSig is an RSA-SHA256 PKCS#1 v1.5
// signature over the UTF-8 bytes of raw. The signature verifies against the
// public key already embedded in AribPOS/Services/LicenseValidator.cs.
//
// Payload (extended, 5 fields; the old 3-field form is still understood):
//
//	machineId | features | hardExpiry(RFC3339) | revalidateBy(RFC3339) | licenseId
package licensetoken

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strings"
	"time"
)

// Payload is the decoded license content.
type Payload struct {
	MachineID    string
	Features     string
	HardExpiry   time.Time // app blocks after this instant
	RevalidateBy time.Time // app should re-check the server after this instant
	LicenseID    string
	// UpdatesUntil is the end of the license's update-entitlement window
	// (desktop/tasks/spec-app-updates.md). nil omits the field entirely and
	// encodes the legacy 5-field payload — REQUIRED for deployed clients,
	// whose parser switches on field count and rejects 6 fields. Callers only
	// set it for clients that advertised they can parse it (appVersion sent).
	UpdatesUntil *time.Time
}

// Signer holds the RSA private key used to sign license payloads.
type Signer struct {
	key *rsa.PrivateKey
}

// NewSigner builds a Signer from a .NET RSAKeyValue private-key XML document.
func NewSigner(privateKeyXML string) (*Signer, error) {
	key, err := ParsePrivateKeyXML(privateKeyXML)
	if err != nil {
		return nil, err
	}
	return &Signer{key: key}, nil
}

func encodePayload(p Payload) string {
	fields := []string{
		p.MachineID,
		p.Features,
		p.HardExpiry.UTC().Format(time.RFC3339),
		p.RevalidateBy.UTC().Format(time.RFC3339),
		p.LicenseID,
	}
	if p.UpdatesUntil != nil {
		fields = append(fields, p.UpdatesUntil.UTC().Format(time.RFC3339))
	}
	return strings.Join(fields, "|")
}

// Sign returns the encoded, signed license string for the given payload.
func (s *Signer) Sign(p Payload) (string, error) {
	raw := encodePayload(p)
	digest := sha256.Sum256([]byte(raw))
	sig, err := rsa.SignPKCS1v15(rand.Reader, s.key, crypto.SHA256, digest[:])
	if err != nil {
		return "", fmt.Errorf("sign: %w", err)
	}
	return base64.StdEncoding.EncodeToString([]byte(raw)) + "." +
		base64.StdEncoding.EncodeToString(sig), nil
}

// Verify checks the signature on a license string and decodes its payload.
// It does not check expiry or machine binding — callers decide policy.
func (s *Signer) Verify(license string) (Payload, error) {
	parts := strings.Split(license, ".")
	if len(parts) != 2 {
		return Payload{}, fmt.Errorf("malformed license: expected 2 segments")
	}
	rawBytes, err := base64.StdEncoding.DecodeString(parts[0])
	if err != nil {
		return Payload{}, fmt.Errorf("decode payload: %w", err)
	}
	sig, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		return Payload{}, fmt.Errorf("decode signature: %w", err)
	}
	digest := sha256.Sum256(rawBytes)
	if err := rsa.VerifyPKCS1v15(&s.key.PublicKey, crypto.SHA256, digest[:], sig); err != nil {
		return Payload{}, fmt.Errorf("signature verification failed: %w", err)
	}
	return decodePayload(string(rawBytes))
}

func decodePayload(raw string) (Payload, error) {
	f := strings.Split(raw, "|")
	switch len(f) {
	case 5, 6:
		hard, err := time.Parse(time.RFC3339, f[2])
		if err != nil {
			return Payload{}, fmt.Errorf("parse hardExpiry: %w", err)
		}
		reval, err := time.Parse(time.RFC3339, f[3])
		if err != nil {
			return Payload{}, fmt.Errorf("parse revalidateBy: %w", err)
		}
		p := Payload{
			MachineID:    f[0],
			Features:     f[1],
			HardExpiry:   hard,
			RevalidateBy: reval,
			LicenseID:    f[4],
		}
		if len(f) == 6 {
			u, err := time.Parse(time.RFC3339, f[5])
			if err != nil {
				return Payload{}, fmt.Errorf("parse updatesUntil: %w", err)
			}
			p.UpdatesUntil = &u
		}
		return p, nil
	case 3:
		// Legacy offline format: machineId|features|expiry
		exp, err := time.Parse(time.RFC3339, f[2])
		if err != nil {
			return Payload{}, fmt.Errorf("parse expiry: %w", err)
		}
		return Payload{
			MachineID:    f[0],
			Features:     f[1],
			HardExpiry:   exp,
			RevalidateBy: exp,
		}, nil
	default:
		return Payload{}, fmt.Errorf("unexpected payload field count: %d", len(f))
	}
}
