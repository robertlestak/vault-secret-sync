package transforms

import (
	"testing"

	"github.com/robertlestak/vault-secret-sync/api/v1alpha1"
)

func TestShouldFilterStringRegex(t *testing.T) {
	tests := []struct {
		name     string
		sc       v1alpha1.VaultSecretSync
		str      string
		expected bool
	}{
		{
			name: "Exclude regex match",
			sc: v1alpha1.VaultSecretSync{
				Spec: v1alpha1.VaultSecretSyncSpec{
					Filters: &v1alpha1.FilterConfig{
						Regex: &v1alpha1.RegexpFilterConfig{
							Exclude: []string{"^exclude.*"},
						},
					},
				},
			},
			str:      "excludeMe",
			expected: true,
		},
		{
			name: "Include regex match",
			sc: v1alpha1.VaultSecretSync{
				Spec: v1alpha1.VaultSecretSyncSpec{
					Filters: &v1alpha1.FilterConfig{
						Regex: &v1alpha1.RegexpFilterConfig{
							Include: []string{"^include.*"},
						},
					},
				},
			},
			str:      "includeMe",
			expected: false,
		},
		{
			name: "No match in include regexes",
			sc: v1alpha1.VaultSecretSync{
				Spec: v1alpha1.VaultSecretSyncSpec{
					Filters: &v1alpha1.FilterConfig{
						Regex: &v1alpha1.RegexpFilterConfig{
							Include: []string{"^include.*"},
						},
					},
				},
			},
			str:      "excludeMe",
			expected: true,
		},
		{
			name: "No regex filters",
			sc: v1alpha1.VaultSecretSync{
				Spec: v1alpha1.VaultSecretSyncSpec{
					Filters: &v1alpha1.FilterConfig{},
				},
			},
			str:      "noFilter",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := shouldFilterStringRegex(tt.sc, tt.str)
			if result != tt.expected {
				t.Errorf("shouldFilterStringRegex() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

func TestShouldFilterStringPath(t *testing.T) {
	tests := []struct {
		name     string
		sc       v1alpha1.VaultSecretSync
		str      string
		expected bool
	}{
		{
			name: "Exclude path match",
			sc: v1alpha1.VaultSecretSync{
				Spec: v1alpha1.VaultSecretSyncSpec{
					Filters: &v1alpha1.FilterConfig{
						Path: &v1alpha1.PathFilterConfig{
							Exclude: []string{"path/to/exclude"},
						},
					},
				},
			},
			str:      "path/to/exclude",
			expected: true,
		},
		{
			name: "Include path match",
			sc: v1alpha1.VaultSecretSync{
				Spec: v1alpha1.VaultSecretSyncSpec{
					Filters: &v1alpha1.FilterConfig{
						Path: &v1alpha1.PathFilterConfig{
							Include: []string{"path/to/include"},
						},
					},
				},
			},
			str:      "path/to/include",
			expected: false,
		},
		{
			name: "No match in include paths",
			sc: v1alpha1.VaultSecretSync{
				Spec: v1alpha1.VaultSecretSyncSpec{
					Filters: &v1alpha1.FilterConfig{
						Path: &v1alpha1.PathFilterConfig{
							Include: []string{"path/to/include"},
						},
					},
				},
			},
			str:      "path/to/exclude",
			expected: true,
		},
		{
			name: "No path filters",
			sc: v1alpha1.VaultSecretSync{
				Spec: v1alpha1.VaultSecretSyncSpec{
					Filters: &v1alpha1.FilterConfig{},
				},
			},
			str:      "path/noFilter",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := shouldFilterStringPath(tt.sc, tt.str)
			if result != tt.expected {
				t.Errorf("shouldFilterStringPath() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

func TestShouldFilterString(t *testing.T) {
	tests := []struct {
		name     string
		sc       v1alpha1.VaultSecretSync
		str      string
		expected bool
	}{
		{
			name: "Regex filter match",
			sc: v1alpha1.VaultSecretSync{
				Spec: v1alpha1.VaultSecretSyncSpec{
					Filters: &v1alpha1.FilterConfig{
						Regex: &v1alpha1.RegexpFilterConfig{
							Exclude: []string{"^exclude.*"},
						},
					},
				},
			},
			str:      "excludeMe",
			expected: true,
		},
		{
			name: "Path filter match",
			sc: v1alpha1.VaultSecretSync{
				Spec: v1alpha1.VaultSecretSyncSpec{
					Filters: &v1alpha1.FilterConfig{
						Path: &v1alpha1.PathFilterConfig{
							Exclude: []string{"path/to/exclude"},
						},
					},
				},
			},
			str:      "path/to/exclude",
			expected: true,
		},
		{
			name: "No filters match",
			sc: v1alpha1.VaultSecretSync{
				Spec: v1alpha1.VaultSecretSyncSpec{
					Filters: &v1alpha1.FilterConfig{
						Regex: &v1alpha1.RegexpFilterConfig{
							Include: []string{"^include.*"},
						},
						Path: &v1alpha1.PathFilterConfig{
							Include: []string{"path/to/include"},
						},
					},
				},
			},
			str:      "excludeMe",
			expected: true,
		},
		{
			name: "No filters",
			sc: v1alpha1.VaultSecretSync{
				Spec: v1alpha1.VaultSecretSyncSpec{},
			},
			str:      "noFilter",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ShouldFilterString(tt.sc, tt.str)
			if result != tt.expected {
				t.Errorf("ShouldFilterString() = %v, expected %v", result, tt.expected)
			}
		})
	}
}
