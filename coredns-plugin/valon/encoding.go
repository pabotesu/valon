package valon

import (
	"encoding/base32"
	"encoding/base64"
	"strings"
)

// Base32 encoding without padding, lowercase (RFC 4648)
var base32Encoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// dnsLabelToPubkey converts DNS-safe label (base32) to WireGuard public key (base64).
// DNS query: "mfrggzdfmztwq2lk.valon.internal"
// → WireGuard pubkey: "abCD1234+/efGH5678=="
func dnsLabelToPubkey(label string) (string, error) {
	// DNS labels are case-insensitive, convert to uppercase for Base32
	label = strings.ToUpper(label)

	// Decode from Base32
	decoded, err := base32Encoding.DecodeString(label)
	if err != nil {
		return "", err
	}

	// Encode to Base64 (standard WireGuard format)
	pubkey := base64.StdEncoding.EncodeToString(decoded)
	return pubkey, nil
}

// pubkeyToDnsLabel converts WireGuard public key (base64) to DNS-safe label (base32).
// WireGuard pubkey: "abCD1234+/efGH5678=="
// → DNS label: "mfrggzdfmztwq2lk"
func pubkeyToDnsLabel(pubkey string) (string, error) {
	// Decode from Base64
	decoded, err := base64.StdEncoding.DecodeString(pubkey)
	if err != nil {
		return "", err
	}

	// Encode to Base32 without padding, lowercase
	label := base32Encoding.EncodeToString(decoded)
	label = strings.ToLower(label)

	return label, nil
}
