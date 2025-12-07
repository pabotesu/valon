package validation

import "testing"

func TestValidateAlias(t *testing.T) {
	tests := []struct {
		name    string
		alias   string
		wantErr bool
	}{
		{"valid simple", "alice", false},
		{"valid with hyphen", "alice-macbook", false},
		{"empty", "", true},
		{"starts with hyphen", "-invalid", true},
		{"too long", "this-is-a-very-long-alias-name-that-exceeds-the-maximum-length", true},
		{"reserved prefix lan", "lan-server", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateAlias(tt.alias)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateAlias(%q) error = %v, wantErr %v", tt.alias, err, tt.wantErr)
			}
		})
	}
}
