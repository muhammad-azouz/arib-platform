package licensetoken

import (
	"crypto/rsa"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"math/big"
	"strings"
)

// rsaKeyValue mirrors the .NET RSACryptoServiceProvider XML format
// (the same shape produced by RSA.ToXmlString(true) and consumed by FromXmlString).
// All fields are standard base64 big-endian integers.
type rsaKeyValue struct {
	XMLName  xml.Name `xml:"RSAKeyValue"`
	Modulus  string   `xml:"Modulus"`
	Exponent string   `xml:"Exponent"`
	P        string   `xml:"P"`
	Q        string   `xml:"Q"`
	DP       string   `xml:"DP"`
	DQ       string   `xml:"DQ"`
	InverseQ string   `xml:"InverseQ"`
	D        string   `xml:"D"`
}

func b64ToInt(s string) (*big.Int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, fmt.Errorf("empty value")
	}
	raw, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, err
	}
	return new(big.Int).SetBytes(raw), nil
}

// ParsePrivateKeyXML parses a .NET RSAKeyValue private-key XML document into an
// *rsa.PrivateKey. The resulting key produces signatures that verify against the
// matching .NET public key embedded in the POS app.
func ParsePrivateKeyXML(doc string) (*rsa.PrivateKey, error) {
	var kv rsaKeyValue
	if err := xml.Unmarshal([]byte(doc), &kv); err != nil {
		return nil, fmt.Errorf("parse rsa xml: %w", err)
	}
	if kv.D == "" || kv.P == "" || kv.Q == "" {
		return nil, fmt.Errorf("xml is not a private key (missing D/P/Q)")
	}

	n, err := b64ToInt(kv.Modulus)
	if err != nil {
		return nil, fmt.Errorf("modulus: %w", err)
	}
	e, err := b64ToInt(kv.Exponent)
	if err != nil {
		return nil, fmt.Errorf("exponent: %w", err)
	}
	d, err := b64ToInt(kv.D)
	if err != nil {
		return nil, fmt.Errorf("d: %w", err)
	}
	p, err := b64ToInt(kv.P)
	if err != nil {
		return nil, fmt.Errorf("p: %w", err)
	}
	q, err := b64ToInt(kv.Q)
	if err != nil {
		return nil, fmt.Errorf("q: %w", err)
	}

	key := &rsa.PrivateKey{
		PublicKey: rsa.PublicKey{N: n, E: int(e.Int64())},
		D:         d,
		Primes:    []*big.Int{p, q},
	}
	// Recompute precomputed values; also validates the key is consistent.
	key.Precompute()
	if err := key.Validate(); err != nil {
		return nil, fmt.Errorf("invalid rsa private key: %w", err)
	}
	return key, nil
}
