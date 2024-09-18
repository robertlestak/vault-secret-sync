package httpstore

import (
	"encoding/json"
	"testing"
)

func TestApplyTemplate(t *testing.T) {
	tests := []struct {
		name           string
		template       string
		secrets        map[string]interface{}
		expectedOutput string
		expectError    bool
	}{
		{
			name:     "No template provided",
			template: "",
			secrets: map[string]interface{}{
				"username": "admin",
				"password": "secret",
			},
			expectedOutput: `{"password":"secret","username":"admin"}`,
			expectError:    false,
		},
		{
			name:     "Valid template provided",
			template: `Username: {{.username}}, Password: {{.password}}`,
			secrets: map[string]interface{}{
				"username": "admin",
				"password": "secret",
			},
			expectedOutput: "Username: admin, Password: secret",
			expectError:    false,
		},
		{
			name:        "Invalid template provided",
			template:    `{{.username} {{.password}}`, // missing closing curly brace
			secrets:     map[string]interface{}{"username": "admin", "password": "secret"},
			expectError: true,
		},
		{
			name:           "Non existent key in template",
			template:       `{{.non_exist}} {{.password}}`,
			secrets:        map[string]interface{}{"username": "admin", "password": "secret"},
			expectedOutput: " secret",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &HTTPClient{
				Template: tt.template,
			}
			secData, err := json.Marshal(tt.secrets)
			if err != nil {
				t.Errorf("ApplyTemplate() error = %v", err)
				return
			}
			output, err := client.ApplyTemplate(secData)
			if (err != nil) != tt.expectError {
				t.Errorf("ApplyTemplate() error = %v, expectError %v", err, tt.expectError)
				return
			}
			if !tt.expectError && output != tt.expectedOutput {
				t.Errorf("ApplyTemplate() output = %v, expectedOutput %v", output, tt.expectedOutput)
			}
		})
	}
}
