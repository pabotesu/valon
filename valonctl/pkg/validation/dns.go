package validation

import (
	"fmt"
	"regexp"
	"strings"
)

const (
	// MaxLabelLength is the maximum length of a DNS label (RFC 1035)
	MaxLabelLength = 63

	// MaxFQDNLength is the maximum length of a fully qualified domain name (RFC 1035)
	MaxFQDNLength = 253

	// MaxAliasLength is the recommended maximum for user-friendly aliases
	MaxAliasLength = 32
)

var (
	// DNS label regex: starts and ends with alphanumeric, middle can include hyphens
	// RFC 952 and RFC 1123
	dnsLabelRegex = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)
)

// ValidateAlias validates an alias name for DNS compatibility.
// Returns error if the alias is invalid.
func ValidateAlias(alias string) error {
	if alias == "" {
		return fmt.Errorf("alias cannot be empty")
	}

	// Convert to lowercase for validation
	alias = strings.ToLower(alias)

	// Check maximum length (user-friendly limit)
	if len(alias) > MaxAliasLength {
		return fmt.Errorf("alias too long: %d characters (max %d)", len(alias), MaxAliasLength)
	}

	// Check DNS RFC limit
	if len(alias) > MaxLabelLength {
		return fmt.Errorf("alias exceeds DNS label limit: %d characters (max %d)", len(alias), MaxLabelLength)
	}

	// Validate DNS label format
	if !dnsLabelRegex.MatchString(alias) {
		return fmt.Errorf("invalid alias format: must contain only lowercase letters, numbers, and hyphens (not at start/end)")
	}

	// Additional checks
	if strings.HasPrefix(alias, "-") || strings.HasSuffix(alias, "-") {
		return fmt.Errorf("alias cannot start or end with a hyphen")
	}

	if strings.Contains(alias, "--") {
		return fmt.Errorf("alias cannot contain consecutive hyphens")
	}

	// Reserved prefixes (used by VALON internally)
	reservedPrefixes := []string{"lan", "nated", "_wireguard", "_udp"}
	for _, prefix := range reservedPrefixes {
		if strings.HasPrefix(alias, prefix) {
			return fmt.Errorf("alias cannot start with reserved prefix: %s", prefix)
		}
	}

	return nil
}

// ValidateFQDN validates the total length of a fully qualified domain name.
func ValidateFQDN(label, zone string) error {
	fqdn := fmt.Sprintf("%s.%s", label, zone)
	if len(fqdn) > MaxFQDNLength {
		return fmt.Errorf("FQDN too long: %d characters (max %d)", len(fqdn), MaxFQDNLength)
	}
	return nil
}

// SanitizeAlias converts alias to DNS-compatible format.
// Returns sanitized alias and any warnings.
func SanitizeAlias(alias string) (string, []string) {
	var warnings []string

	// Convert to lowercase
	original := alias
	alias = strings.ToLower(alias)
	if alias != original {
		warnings = append(warnings, "converted to lowercase")
	}

	// Replace invalid characters with hyphens
	sanitized := regexp.MustCompile(`[^a-z0-9-]`).ReplaceAllString(alias, "-")
	if sanitized != alias {
		warnings = append(warnings, "replaced invalid characters with hyphens")
		alias = sanitized
	}

	// Remove consecutive hyphens
	sanitized = regexp.MustCompile(`-+`).ReplaceAllString(alias, "-")
	if sanitized != alias {
		warnings = append(warnings, "removed consecutive hyphens")
		alias = sanitized
	}

	// Trim hyphens from start/end
	sanitized = strings.Trim(alias, "-")
	if sanitized != alias {
		warnings = append(warnings, "removed leading/trailing hyphens")
		alias = sanitized
	}

	// Truncate if too long
	if len(alias) > MaxAliasLength {
		alias = alias[:MaxAliasLength]
		warnings = append(warnings, fmt.Sprintf("truncated to %d characters", MaxAliasLength))
	}

	return alias, warnings
}
