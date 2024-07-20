package sync

import (
	"testing"
)

// TestIsRegexPath tests the isRegexPath function
func TestIsRegexPath(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{
			name:     "Simple static path",
			path:     "/simple/path",
			expected: false,
		},
		{
			name:     "Path with wildcard",
			path:     "/path/.*",
			expected: true,
		},
		{
			name:     "Path with character set",
			path:     "/path/[a-z]+",
			expected: true,
		},
		{
			name:     "Path with escaped characters",
			path:     "/path/\\d+",
			expected: true,
		},
		{
			name:     "Path with parentheses for grouping",
			path:     "/path/(grouping)",
			expected: true,
		},
		{
			name:     "Invalid regex path",
			path:     "/path/[unclosed-bracket",
			expected: false,
		},
		{
			name:     "Empty path",
			path:     "",
			expected: false,
		},
		{
			name:     "Path with special characters but no regex",
			path:     "/path/with-$-chars",
			expected: false,
		},
		{
			name:     "Escaped regex-like characters",
			path:     "/path/with-a-.dot",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := isRegexPath(tt.path)
			if actual != tt.expected {
				t.Errorf("isRegexPath(%q) = %v; expected %v", tt.path, actual, tt.expected)
			}
		})
	}
}
