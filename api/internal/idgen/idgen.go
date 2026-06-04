// Package idgen produces unique identifiers and secrets.
package idgen

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"
)

// New returns a prefixed, URL-safe, time-unordered unique id, e.g. "acc_3f9a...".
func New(prefix string) string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return prefix + "_" + base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b)
}

// LicenseKey returns a human-friendly grouped key, e.g. "ARIB-7QF3-K9ZP-2MX8".
func LicenseKey() string {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789" // no ambiguous chars
	groups := make([]string, 3)
	for g := range groups {
		var sb strings.Builder
		for i := 0; i < 4; i++ {
			n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
			sb.WriteByte(alphabet[n.Int64()])
		}
		groups[g] = sb.String()
	}
	return "ARIB-" + strings.Join(groups, "-")
}

// NumericCode returns an n-digit numeric one-time code (e.g. OTP).
func NumericCode(n int) string {
	var sb strings.Builder
	for i := 0; i < n; i++ {
		d, _ := rand.Int(rand.Reader, big.NewInt(10))
		sb.WriteString(d.String())
	}
	return sb.String()
}

// Secret returns a high-entropy URL-safe secret token (for refresh tokens, etc.).
func Secret() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// Hash returns the hex-encoded SHA-256 of s (used to store tokens/codes at rest).
func Hash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// VerifyHash reports whether s hashes to the stored hex digest.
func VerifyHash(s, hexDigest string) bool {
	return Hash(s) == strings.ToLower(strings.TrimSpace(hexDigest))
}

// Fingerprint returns a short stable hash, handy for log correlation.
func Fingerprint(s string) string { return fmt.Sprintf("%s", Hash(s)[:12]) }
