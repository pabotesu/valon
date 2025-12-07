package encoding

import (
	"encoding/base32"
	"encoding/base64"
	"fmt"
	"strings"
)

// Base32 encoding without padding, lowercase (RFC 4648)
var base32Encoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// LabelToPubkey converts DNS-safe label (base32) to WireGuard public key (base64).
// DNS query: "mfrggzdfmztwq2lk.valon.internal"
// → WireGuard pubkey: "abCD1234+/efGH5678=="
func LabelToPubkey(label string) (string, error) {
	if label == "" {
		return "", fmt.Errorf("label cannot be empty")
	}

	// DNS labels are case-insensitive, convert to uppercase for Base32
	label = strings.ToUpper(label)

	// Decode from Base32
	decoded, err := base32Encoding.DecodeString(label)
	if err != nil {
		return "", fmt.Errorf("invalid base32 label: %w", err)
	}

	// Encode to Base64 (standard WireGuard format)
	pubkey := base64.StdEncoding.EncodeToString(decoded)
	return pubkey, nil
}

// PubkeyToLabel converts WireGuard public key (base64) to DNS-safe label (base32).
// WireGuard pubkey: "abCD1234+/efGH5678=="
// → DNS label: "mfrggzdfmztwq2lk"
func PubkeyToLabel(pubkey string) (string, error) {
	if pubkey == "" {
		return "", fmt.Errorf("pubkey cannot be empty")
	}

	// Decode from Base64
	decoded, err := base64.StdEncoding.DecodeString(pubkey)
	if err != nil {
		return "", fmt.Errorf("invalid base64 pubkey: %w", err)
	}

	// WireGuard public keys are always 32 bytes
	if len(decoded) != 32 {
		return "", fmt.Errorf("invalid pubkey length: %d bytes (expected 32)", len(decoded))
	}

	// Encode to Base32 without padding, lowercase
	label := base32Encoding.EncodeToString(decoded)
	label = strings.ToLower(label)

	return label, nil
}

// DetectFormat detects if input is a Base32 label or Base64 pubkey.
// Returns "base32", "base64", or "unknown".
func DetectFormat(input string) string {
	if input == "" {
		return "unknown"
	}

	// Base64 contains +, /, =
	if strings.ContainsAny(input, "+/=") {
		return "base64"
	}

	// Base32 (lowercase) contains only a-z, 2-7
	if len(input) == 52 && !strings.ContainsAny(input, "+/=") {
		return "base32"
	}

	return "unknown"
}

// NormalizePubkey attempts to normalize any input to a Base64 WireGuard public key.
// Accepts both Base32 labels and Base64 pubkeys.
func NormalizePubkey(input string) (string, error) {
	if input == "" {
		return "", fmt.Errorf("input cannot be empty")
	}

	format := DetectFormat(input)
	switch format {
	case "base64":
		// Validate by decoding
		decoded, err := base64.StdEncoding.DecodeString(input)
		if err != nil {
			return "", fmt.Errorf("invalid base64 format: %w", err)
		}
		if len(decoded) != 32 {
			return "", fmt.Errorf("invalid pubkey length: %d bytes (expected 32)", len(decoded))
		}
		return input, nil

	case "base32":
		// Convert to base64
		return LabelToPubkey(input)

	default:
		return "", fmt.Errorf("unknown format: expected base64 pubkey or base32 label")
	}
}
