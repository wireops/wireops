package hooks

import (
	"testing"
)

func TestMaskEmailForLog(t *testing.T) {
	tests := []struct {
		name     string
		email    string
		expected string
	}{
		{
			name:     "normal email",
			email:    "user@example.com",
			expected: "u***@example.com",
		},
		{
			name:     "single char local part",
			email:    "a@domain.org",
			expected: "a***@domain.org",
		},
		{
			name:     "empty email",
			email:    "",
			expected: "[empty]",
		},
		{
			name:     "no @ sign",
			email:    "invalidemail",
			expected: "[invalid]",
		},
		{
			name:     "long local part",
			email:    "verylongemail@domain.org",
			expected: "v***@domain.org",
		},
		{
			name:     "subdomain in domain",
			email:    "admin@mail.example.com",
			expected: "a***@mail.example.com",
		},
		{
			name:     "numbers in email",
			email:    "user123@test.io",
			expected: "u***@test.io",
		},
		{
			name:     "plus addressing",
			email:    "user+tag@example.com",
			expected: "u***@example.com",
		},
		{
			name:     "dots in local part",
			email:    "first.last@company.com",
			expected: "f***@company.com",
		},
		{
			name:     "multiple @ signs (invalid but handled)",
			email:    "bad@@example.com",
			expected: "b***@@example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := maskEmailForLog(tt.email)
			if result != tt.expected {
				t.Errorf("maskEmailForLog(%q) = %q, want %q", tt.email, result, tt.expected)
			}
		})
	}
}

func TestMaskEmailForLog_ConsistentOutput(t *testing.T) {
	// Ensure the function produces consistent output for the same input
	email := "consistent@test.com"
	expected := "c***@test.com"

	for i := 0; i < 100; i++ {
		result := maskEmailForLog(email)
		if result != expected {
			t.Errorf("Inconsistent output on iteration %d: got %q, want %q", i, result, expected)
		}
	}
}
