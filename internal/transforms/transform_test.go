package transforms

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/robertlestak/vault-secret-sync/api/v1alpha1"
)

func TestExecuteTransformTemplate(t *testing.T) {
	// Define a sample VaultSecretSync with a simple template transformation
	simpleTemplate := `{"newField": "{{.oldField}}"}`
	complexTemplate := `{
		"concatenated": "{{.field1}}-{{.field2}}",
		"nested": {
			"field": "{{.nestedField}}"
		},
		"list": [
			{{range $index, $element := .list}}{{if $index}}, {{end}}"{{$element}}"{{end}}
		]
	}`

	tests := []struct {
		name           string
		template       string
		secret         map[string]any
		expectedSecret map[string]any
		expectError    bool
	}{
		{
			name:     "Simple template",
			template: simpleTemplate,
			secret: map[string]any{
				"oldField": "oldValue",
			},
			expectedSecret: map[string]any{
				"newField": "oldValue",
			},
			expectError: false,
		},
		{
			name:     "Complex template",
			template: complexTemplate,
			secret: map[string]any{
				"field1":      "value1",
				"field2":      "value2",
				"nestedField": "nestedValue",
				"list":        []string{"item1", "item2", "item3"},
			},
			expectedSecret: map[string]any{
				"concatenated": "value1-value2",
				"nested": map[string]any{
					"field": "nestedValue",
				},
				"list": []string{"item1", "item2", "item3"},
			},
			expectError: false,
		},
		{
			name:     "Template with missing fields",
			template: `{"missingField": "{{.nonexistent}}"}`,
			secret: map[string]any{
				"existingField": "value",
			},
			expectedSecret: map[string]any{
				"missingField": "<no value>",
			},
			expectError: false,
		},
		{
			name: "Template with conditional logic",
			template: `{
				{{if .condition}} "conditionalField": "true" {{else}} "conditionalField": "false" {{end}}
			}`,
			secret: map[string]any{
				"condition": true,
			},
			expectedSecret: map[string]any{
				"conditionalField": "true",
			},
			expectError: false,
		},
		{
			name:     "Invalid template syntax",
			template: `{"newField": "{{.oldField}"}`,
			secret: map[string]any{
				"oldField": "oldValue",
			},
			expectedSecret: nil,
			expectError:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vaultSecretSync := v1alpha1.VaultSecretSync{
				Spec: v1alpha1.VaultSecretSyncSpec{
					Transforms: &v1alpha1.TransformSpec{
						Template: &tt.template,
					},
				},
			}

			newSecret, err := ExecuteTransformTemplate(vaultSecretSync, tt.secret)
			if (err != nil) != tt.expectError {
				t.Errorf("ExecuteTransformTemplate() error = %v, expectError %v", err, tt.expectError)
				return
			}
			if !tt.expectError && !compareSecrets(newSecret, tt.expectedSecret) {
				t.Errorf("ExecuteTransformTemplate() newSecret = %v, expectedSecret %v", newSecret, tt.expectedSecret)
			}
		})
	}
}

// Helper function to compare two secret maps
func compareSecrets(secret1, secret2 map[string]any) bool {
	secret1Bytes, _ := json.Marshal(secret1)
	secret2Bytes, _ := json.Marshal(secret2)
	return bytes.Equal(secret1Bytes, secret2Bytes)
}

func TestExecuteIncludeTransforms(t *testing.T) {
	// Define a sample VaultSecretSync with include transformations
	vaultSecretSync := v1alpha1.VaultSecretSync{
		Spec: v1alpha1.VaultSecretSyncSpec{
			Transforms: &v1alpha1.TransformSpec{
				Include: []string{"includedField"},
			},
		},
	}

	secret := map[string]any{
		"includedField": "includeValue",
		"excludedField": "excludeValue",
	}

	expectedSecret := map[string]any{
		"includedField": "includeValue",
	}

	newSecret, err := ExecuteIncludeTransforms(vaultSecretSync, secret)
	if err != nil {
		t.Errorf("ExecuteIncludeTransforms() error = %v", err)
		return
	}
	if !compareSecrets(newSecret, expectedSecret) {
		t.Errorf("ExecuteIncludeTransforms() newSecret = %v, expectedSecret %v", newSecret, expectedSecret)
	}
}

func TestExecuteExcludeTransforms(t *testing.T) {
	// Define a sample VaultSecretSync with exclude transformations
	vaultSecretSync := v1alpha1.VaultSecretSync{
		Spec: v1alpha1.VaultSecretSyncSpec{
			Transforms: &v1alpha1.TransformSpec{
				Exclude: []string{"excludedField"},
			},
		},
	}

	secret := map[string]any{
		"includedField": "includeValue",
		"excludedField": "excludeValue",
	}

	expectedSecret := map[string]any{
		"includedField": "includeValue",
	}

	newSecret, err := ExecuteExcludeTransforms(vaultSecretSync, secret)
	if err != nil {
		t.Errorf("ExecuteExcludeTransforms() error = %v", err)
		return
	}
	if !compareSecrets(newSecret, expectedSecret) {
		t.Errorf("ExecuteExcludeTransforms() newSecret = %v, expectedSecret %v", newSecret, expectedSecret)
	}
}
