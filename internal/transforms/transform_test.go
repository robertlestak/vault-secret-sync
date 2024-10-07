package transforms

import (
	"testing"

	"github.com/robertlestak/vault-secret-sync/api/v1alpha1"
	"github.com/stretchr/testify/assert"
)

func TestExecuteTransformTemplate(t *testing.T) {
	tests := []struct {
		name     string
		sc       v1alpha1.VaultSecretSync
		secret   []byte
		expected []byte
		wantErr  bool
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
			name: "Valid template",
			sc: v1alpha1.VaultSecretSync{
				Spec: v1alpha1.VaultSecretSyncSpec{
					Transforms: &v1alpha1.TransformSpec{
						Template: ptrToString(`{"newKey":"{{ .key }}"}`),
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
			result, err := ExecuteTransformTemplate(tt.sc, tt.secret)
			if (err != nil) != tt.wantErr {
				t.Errorf("ExecuteTransformTemplate() error = %v, wantErr %v", err, tt.wantErr)
				return
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

func ptrToString(s string) *string {
	return &s
}
