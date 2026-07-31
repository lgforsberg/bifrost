package mail

import "testing"

func TestNormalizeAddress(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"user+tag@domain.com", "user@domain.com"},
		{"user@domain.com", "user@domain.com"},
		{"user+foo+bar@domain.com", "user@domain.com"},
		{"User+Tag@Domain.COM", "User@Domain.COM"},
		{"noat", "noat@"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := NormalizeAddress(tt.input)
			if got != tt.want {
				t.Errorf("NormalizeAddress(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestNormalizeAddressLower(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"User+Tag@Domain.COM", "user@domain.com"},
		{"Alice@Example.COM", "alice@example.com"},
		{"user@domain.com", "user@domain.com"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := NormalizeAddressLower(tt.input)
			if got != tt.want {
				t.Errorf("NormalizeAddressLower(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestEffectiveUsername(t *testing.T) {
	t.Run("explicit username", func(t *testing.T) {
		a := AccountConfig{Address: "user@example.com", Username: "custom"}
		if got := a.EffectiveUsername(); got != "custom" {
			t.Errorf("got %q, want %q", got, "custom")
		}
	})
	t.Run("falls back to address", func(t *testing.T) {
		a := AccountConfig{Address: "user@example.com"}
		if got := a.EffectiveUsername(); got != "user@example.com" {
			t.Errorf("got %q, want %q", got, "user@example.com")
		}
	})
}

func TestParsePlusAddress(t *testing.T) {
	tests := []struct {
		input                          string
		wantLocal, wantTag, wantDomain string
	}{
		{"user+tag@domain.com", "user", "tag", "domain.com"},
		{"user@domain.com", "user", "", "domain.com"},
		{"user+foo+bar@domain.com", "user", "foo+bar", "domain.com"},
		{"user+@domain.com", "user", "", "domain.com"},
		{"noat", "noat", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			local, tag, domain := ParsePlusAddress(tt.input)
			if local != tt.wantLocal || tag != tt.wantTag || domain != tt.wantDomain {
				t.Errorf("ParsePlusAddress(%q) = (%q, %q, %q), want (%q, %q, %q)",
					tt.input, local, tag, domain, tt.wantLocal, tt.wantTag, tt.wantDomain)
			}
		})
	}
}

func TestAddressString(t *testing.T) {
	tests := []struct {
		addr Address
		want string
	}{
		{Address{Name: "John Doe", Address: "john@example.com"}, "John Doe <john@example.com>"},
		{Address{Address: "john@example.com"}, "john@example.com"},
		{Address{Name: "", Address: "john@example.com"}, "john@example.com"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.addr.String(); got != tt.want {
				t.Errorf("Address.String() = %q, want %q", got, tt.want)
			}
		})
	}
}
