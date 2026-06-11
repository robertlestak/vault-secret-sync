package transforms

import (
	"testing"

	"github.com/robertlestak/vault-secret-sync/api/v1alpha1"
	"github.com/stretchr/testify/assert"
)

func TestExecuteTransformTemplate(t *testing.T) {
	tests := []struct {
		name        string
		sc          v1alpha1.VaultSecretSync
		secret      []byte
		expected    []byte
		wantErr     bool
		errContains string
	}{
		{
			name: "No template",
			sc: v1alpha1.VaultSecretSync{
				Spec: v1alpha1.VaultSecretSyncSpec{
					Transforms: &v1alpha1.TransformSpec{},
				},
			},
			secret:   []byte(`{"key":"value"}`),
			expected: []byte(`{"key":"value"}`),
			wantErr:  false,
		},
		{
			name:     "Valid template",
			sc:       vaultSecretSyncWithTemplate(`{"newKey":"{{ .key }}"}`),
			secret:   []byte(`{"key":"value"}`),
			expected: []byte(`{"newKey":"value"}`),
			wantErr:  false,
		},
		{
			// this behavior could be changed by setting `.Option("missingkey=error")` on the template
			name:     "⚠️ requesting unknown key leads to '<no value>' value",
			sc:       vaultSecretSyncWithTemplate(`{"newKey":"{{ .unknownKey }}"}`),
			secret:   []byte(`{"key":"value"}`),
			expected: []byte(`{"newKey":"<no value>"}`),
			wantErr:  false,
		},
		{
			name:        "⚠️ int function cannot handle ints (42 unmarshals to float64)",
			sc:          vaultSecretSyncWithTemplate(`{"newKey":{{ .key | int }}}`),
			secret:      []byte(`{"key":42}`),
			expected:    []byte(`{"key":42}`),
			wantErr:     true,
			errContains: "interface conversion: interface {} is float64, not int",
		},
		{
			name:        "⚠️ int function cannot handle floats",
			sc:          vaultSecretSyncWithTemplate(`{"newKey":{{ .key | int }}}`),
			secret:      []byte(`{"key":42.0}`),
			expected:    []byte(`{"key":42.0}`),
			wantErr:     true,
			errContains: "interface conversion: interface {} is float64, not int",
		},
		{
			name:        "⚠️ int function cannot handle int strings",
			sc:          vaultSecretSyncWithTemplate(`{"newKey":{{ .key | int }}}`),
			secret:      []byte(`{"key":"42"}`),
			expected:    []byte(`{"key":"42"}`),
			wantErr:     true,
			errContains: "interface conversion: interface {} is string, not int",
		},
		{
			name:        "⚠️ int function cannot handle float strings",
			sc:          vaultSecretSyncWithTemplate(`{"newKey":{{ .key | int }}}`),
			secret:      []byte(`{"key":"42.0"}`),
			expected:    []byte(`{"key":"42.0"}`),
			wantErr:     true,
			errContains: "interface conversion: interface {} is string, not int",
		},
		{
			name:     "Nested object",
			sc:       vaultSecretSyncWithTemplate(`{"newKey":"{{ .key.foo }}"}`),
			secret:   []byte(`{"key":{"foo":"bar"}}`),
			expected: []byte(`{"newKey":"bar"}`),
			wantErr:  false,
		},
		{
			name:        "Invalid template returns error and original secret",
			sc:          vaultSecretSyncWithTemplate(`{"newKey":"{{ .key "}`),
			secret:      []byte(`{"key":"value"}`),
			expected:    []byte(`{"key":"value"}`),
			wantErr:     true,
			errContains: "unterminated quoted string",
		},
		{
			name:     "Escape json chars",
			sc:       vaultSecretSyncWithTemplate(`{"field":{{ .field | json }}}`),
			secret:   []byte(`{"field":"foo\"bar"}`),
			expected: []byte(`{"field":"foo\"bar"}`),
			wantErr:  false,
		},
		{
			name:     "Escape password with json chars",
			sc:       vaultSecretSyncWithTemplate(`{"field":{{ .field | json }}}`),
			secret:   []byte(`{"field":"password with special chars \"§$%?\\/()="}`),
			expected: []byte(`{"field":"password with special chars \"§$%?\\/()="}`),
			wantErr:  false,
		},
		{
			name:     "⚠️ unescaped password with json chars creates invalid json",
			sc:       vaultSecretSyncWithTemplate(`{"field":"{{ .field }}"}`),
			secret:   []byte(`{"field":"password with special chars \"§$%?\\/()="}`),
			expected: []byte(`{"field":"password with special chars "§$%?\/()="}`),
			wantErr:  false,
		},
		{
			name:     "Templates can produce invalid json",
			sc:       vaultSecretSyncWithTemplate(`{"field":"{{ .field }}"}`),
			secret:   []byte(`{"field":"foo\"bar"}`),
			expected: []byte(`{"field":"foo"bar"}`),
			wantErr:  false,
		},
		{
			name:     "String function converts number",
			sc:       vaultSecretSyncWithTemplate(`{"count":"{{ .count | string }}"}`),
			secret:   []byte(`{"count":3}`),
			expected: []byte(`{"count":"3"}`),
			wantErr:  false,
		},
		{
			name:     "String function on string",
			sc:       vaultSecretSyncWithTemplate(`{"count":"{{ .count | string }}"}`),
			secret:   []byte(`{"count":"3"}`),
			expected: []byte(`{"count":"3"}`),
			wantErr:  false,
		},
		{
			name:     "base64encode on string",
			sc:       vaultSecretSyncWithTemplate(`{"field":"{{ .field | base64encode }}"}`),
			secret:   []byte(`{"field":"foobar"}`),
			expected: []byte(`{"field":"Zm9vYmFy"}`),
			wantErr:  false,
		},
		{
			name:     "base64encode on empty string",
			sc:       vaultSecretSyncWithTemplate(`{"field":"{{ .field | base64encode }}"}`),
			secret:   []byte(`{"field":""}`),
			expected: []byte(`{"field":""}`),
			wantErr:  false,
		},
		{
			name:     "base64encode handles unicode string",
			sc:       vaultSecretSyncWithTemplate(`{"field":"{{ .field | base64encode }}"}`),
			secret:   []byte(`{"field":"こんにちは"}`),
			expected: []byte(`{"field":"44GT44KT44Gr44Gh44Gv"}`),
			wantErr:  false,
		},
		{
			name:        "base64encode on number returns error",
			sc:          vaultSecretSyncWithTemplate(`{"field":"{{ .field | base64encode }}"}`),
			secret:      []byte(`{"field":42}`),
			expected:    []byte(`{"field":42}`),
			wantErr:     true,
			errContains: "base64encode expects string",
		},
		{
			name:     "base64decode on string",
			sc:       vaultSecretSyncWithTemplate(`{"field":"{{ .field | base64decode }}"}`),
			secret:   []byte(`{"field":"Zm9vYmFy"}`),
			expected: []byte(`{"field":"foobar"}`),
			wantErr:  false,
		},
		{
			name:     "base64decode on empty string",
			sc:       vaultSecretSyncWithTemplate(`{"field":"{{ .field | base64decode }}"}`),
			secret:   []byte(`{"field":""}`),
			expected: []byte(`{"field":""}`),
			wantErr:  false,
		},
		{
			name:     "base64decode handles unicode string",
			sc:       vaultSecretSyncWithTemplate(`{"field":"{{ .field | base64decode }}"}`),
			secret:   []byte(`{"field":"44GT44KT44Gr44Gh44Gv"}`),
			expected: []byte(`{"field":"こんにちは"}`),
			wantErr:  false,
		},
		{
			name:     "base64encode preserves padding for single byte",
			sc:       vaultSecretSyncWithTemplate(`{"field":"{{ .field | base64encode }}"}`),
			secret:   []byte(`{"field":"f"}`),
			expected: []byte(`{"field":"Zg=="}`),
			wantErr:  false,
		},
		{
			name:        "base64decode on number returns error",
			sc:          vaultSecretSyncWithTemplate(`{"field":"{{ .field | base64decode }}"}`),
			secret:      []byte(`{"field":42}`),
			expected:    []byte(`{"field":42}`),
			wantErr:     true,
			errContains: "base64decode expects string",
		},
		{
			name:        "base64decode invalid base64 returns error",
			sc:          vaultSecretSyncWithTemplate(`{"field":"{{ .field | base64decode }}"}`),
			secret:      []byte(`{"field":"not-valid-base64!"}`),
			expected:    []byte(`{"field":"not-valid-base64!"}`),
			wantErr:     true,
			errContains: "illegal base64 data",
		},
		{
			name:     "base64decode with JSON characters",
			sc:       vaultSecretSyncWithTemplate(`{"field":{{ .field | base64decode | json }}}`),
			secret:   []byte(`{"field":"Zm9vImJhcg=="}`),
			expected: []byte(`{"field":"foo\"bar"}`),
			wantErr:  false,
		},
		{
			name:     "base64decode JSON construct",
			sc:       vaultSecretSyncWithTemplate(`{"field":{{ .field | base64decode | json }}}`),
			secret:   []byte(`{"field":"eyJhIjoiYiJ9"}`),
			expected: []byte(`{"field":"{\"a\":\"b\"}"}`),
			wantErr:  false,
		},
		{
			name:     "base64encode then base64decode round trip",
			sc:       vaultSecretSyncWithTemplate(`{"field":{{ .field | base64encode | base64decode | json }}}`),
			secret:   []byte(`{"field":"\"foo\"bar\""}`),
			expected: []byte(`{"field":"\"foo\"bar\""}`),
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ExecuteTransformTemplate(tt.sc, tt.secret)
			if (err != nil) != tt.wantErr {
				t.Errorf("ExecuteTransformTemplate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				assert.ErrorContains(t, err, tt.errContains)
			}
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExecuteRenameTransforms(t *testing.T) {
	tests := []struct {
		name     string
		sc       v1alpha1.VaultSecretSync
		secret   []byte
		expected []byte
		wantErr  bool
	}{
		{
			name: "No rename",
			sc: v1alpha1.VaultSecretSync{
				Spec: v1alpha1.VaultSecretSyncSpec{
					Transforms: &v1alpha1.TransformSpec{},
				},
			},
			secret:   []byte(`{"key":"value"}`),
			expected: []byte(`{"key":"value"}`),
			wantErr:  false,
		},
		{
			name: "Valid rename",
			sc: v1alpha1.VaultSecretSync{
				Spec: v1alpha1.VaultSecretSyncSpec{
					Transforms: &v1alpha1.TransformSpec{
						Rename: []v1alpha1.RenameTransform{
							{From: "key", To: "newKey"},
						},
					},
				},
			},
			secret:   []byte(`{"key":"value"}`),
			expected: []byte(`{"newKey":"value"}`),
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ExecuteRenameTransforms(tt.sc, tt.secret)
			if (err != nil) != tt.wantErr {
				t.Errorf("ExecuteRenameTransforms() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExecuteIncludeTransforms(t *testing.T) {
	tests := []struct {
		name     string
		sc       v1alpha1.VaultSecretSync
		secret   []byte
		expected []byte
		wantErr  bool
	}{
		{
			name: "No include",
			sc: v1alpha1.VaultSecretSync{
				Spec: v1alpha1.VaultSecretSyncSpec{
					Transforms: &v1alpha1.TransformSpec{},
				},
			},
			secret:   []byte(`{"key":"value"}`),
			expected: []byte(`{"key":"value"}`),
			wantErr:  false,
		},
		{
			name: "Valid include",
			sc: v1alpha1.VaultSecretSync{
				Spec: v1alpha1.VaultSecretSyncSpec{
					Transforms: &v1alpha1.TransformSpec{
						Include: []string{"key"},
					},
				},
			},
			secret:   []byte(`{"key":"value","otherKey":"otherValue"}`),
			expected: []byte(`{"key":"value"}`),
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ExecuteIncludeTransforms(tt.sc, tt.secret)
			if (err != nil) != tt.wantErr {
				t.Errorf("ExecuteIncludeTransforms() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExecuteExcludeTransforms(t *testing.T) {
	tests := []struct {
		name     string
		sc       v1alpha1.VaultSecretSync
		secret   []byte
		expected []byte
		wantErr  bool
	}{
		{
			name: "No exclude",
			sc: v1alpha1.VaultSecretSync{
				Spec: v1alpha1.VaultSecretSyncSpec{
					Transforms: &v1alpha1.TransformSpec{},
				},
			},
			secret:   []byte(`{"key":"value"}`),
			expected: []byte(`{"key":"value"}`),
			wantErr:  false,
		},
		{
			name: "Valid exclude",
			sc: v1alpha1.VaultSecretSync{
				Spec: v1alpha1.VaultSecretSyncSpec{
					Transforms: &v1alpha1.TransformSpec{
						Exclude: []string{"key"},
					},
				},
			},
			secret:   []byte(`{"key":"value","otherKey":"otherValue"}`),
			expected: []byte(`{"otherKey":"otherValue"}`),
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ExecuteExcludeTransforms(tt.sc, tt.secret)
			if (err != nil) != tt.wantErr {
				t.Errorf("ExecuteExcludeTransforms() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExecuteTransforms(t *testing.T) {
	tests := []struct {
		name     string
		sc       v1alpha1.VaultSecretSync
		secret   []byte
		expected []byte
		wantErr  bool
	}{
		{
			name: "No transforms",
			sc: v1alpha1.VaultSecretSync{
				Spec: v1alpha1.VaultSecretSyncSpec{
					Transforms: &v1alpha1.TransformSpec{},
				},
			},
			secret:   []byte(`{"key":"value"}`),
			expected: []byte(`{"key":"value"}`),
			wantErr:  false,
		},
		{
			name: "All transforms",
			sc: v1alpha1.VaultSecretSync{
				Spec: v1alpha1.VaultSecretSyncSpec{
					Transforms: &v1alpha1.TransformSpec{
						Exclude: []string{"excludeKey"},
						Include: []string{"includeKey"},
						Rename: []v1alpha1.RenameTransform{
							{From: "renameKey", To: "newRenameKey"},
						},
						Template: ptrToString(`{"templateKey":"{{ .includeKey }}"}`),
					},
				},
			},
			secret:   []byte(`{"excludeKey":"excludeValue","includeKey":"includeValue","renameKey":"renameValue"}`),
			expected: []byte(`{"templateKey":"includeValue"}`),
			wantErr:  false,
		},
		{
			name: "Regex include",
			sc: v1alpha1.VaultSecretSync{
				Spec: v1alpha1.VaultSecretSyncSpec{
					Transforms: &v1alpha1.TransformSpec{
						Include: []string{"NEXT_PUBLIC_.*"},
					},
				},
			},
			secret:   []byte(`{"NEXT_PUBLIC_KEY":"value","NEXT_PRIVATE_KEY":"otherValue"}`),
			expected: []byte(`{"NEXT_PUBLIC_KEY":"value"}`),
			wantErr:  false,
		},
		{
			name: "Regex exclude",
			sc: v1alpha1.VaultSecretSync{
				Spec: v1alpha1.VaultSecretSyncSpec{
					Transforms: &v1alpha1.TransformSpec{
						Exclude: []string{"NEXT_PRIVATE_.*"},
					},
				},
			},
			secret:   []byte(`{"NEXT_PUBLIC_KEY":"value","NEXT_PRIVATE_KEY":"otherValue"}`),
			expected: []byte(`{"NEXT_PUBLIC_KEY":"value"}`),
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ExecuteTransforms(tt.sc, tt.secret)
			if (err != nil) != tt.wantErr {
				t.Errorf("ExecuteTransforms() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			assert.Equal(t, tt.expected, result)
		})
	}
}

func vaultSecretSyncWithTemplate(template string) v1alpha1.VaultSecretSync {
	return v1alpha1.VaultSecretSync{
		Spec: v1alpha1.VaultSecretSyncSpec{
			Transforms: &v1alpha1.TransformSpec{
				Template: ptrToString(template),
			},
		},
	}
}

func ptrToString(s string) *string {
	return &s
}
