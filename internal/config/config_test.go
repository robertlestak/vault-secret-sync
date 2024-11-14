package config

import (
	"os"
	"testing"

	"github.com/robertlestak/vault-secret-sync/internal/backend"
	"github.com/robertlestak/vault-secret-sync/internal/queue"
	"github.com/stretchr/testify/assert"
)

func TestLoadFile(t *testing.T) {
	testCases := []struct {
		name          string
		fileContent   string
		fileFormat    string
		expectedError bool
	}{
		{
			name: "ValidYAMLConfig",
			fileContent: `
log:
  level: "debug"
events:
  enabled: true
  port: 8080
operator:
  enabled: true
  backend:
    type: "kubernetes"
stores:
  vault:
    address: "http://127.0.0.1:8200"
queue:
  type: "memory"
metrics:
  port: 9090
`,
			fileFormat:    "yaml",
			expectedError: false,
		},
		{
			name: "ValidJSONConfig",
			fileContent: `
{
  "log": {
    "level": "debug"
},
  "events": {
    "enabled": true,
    "port": 8080
  },
  "operator": {
    "enabled": true,
    "backend": {
      "type": "kubernetes"
    }
  },
  "stores": {
    "vault": {
      "address": "http://127.0.0.1:8200"
    }
  },
  "queue": {
    "type": "memory"
  },
  "metrics": {
    "port": 9090
  }
}
`,
			fileFormat:    "json",
			expectedError: false,
		},
		{
			name:          "InvalidFileFormat",
			fileContent:   `invalid content`,
			fileFormat:    "invalid",
			expectedError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tempFile, err := os.CreateTemp("", "config.*."+tc.fileFormat)
			assert.NoError(t, err)
			defer os.Remove(tempFile.Name())

			_, err = tempFile.WriteString(tc.fileContent)
			assert.NoError(t, err)
			err = tempFile.Close()
			assert.NoError(t, err)

			err = LoadFile(tempFile.Name())
			if tc.expectedError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, Config)
			}
		})
	}
}

func TestSetFromEnv(t *testing.T) {
	t.Setenv("VSS_LOG_LEVEL", "debug")
	t.Setenv("VSS_EVENTS_ENABLED", "true")
	t.Setenv("VSS_EVENTS_PORT", "8080")
	t.Setenv("VSS_OPERATOR_ENABLED", "true")
	t.Setenv("VSS_OPERATOR_BACKEND_TYPE", "kubernetes")
	t.Setenv("VSS_STORES_VAULT_ADDRESS", "http://127.0.0.1:8200")
	t.Setenv("VSS_QUEUE_TYPE", "memory")
	t.Setenv("VSS_METRICS_PORT", "9090")

	var cfg ConfigFile
	err := cfg.SetFromEnv()
	assert.NoError(t, err)
	assert.Equal(t, "debug", cfg.Log.Level)
	assert.NotNil(t, cfg.Events)
	assert.True(t, *cfg.Events.Enabled)
	assert.Equal(t, 8080, cfg.Events.Port)
	assert.NotNil(t, cfg.Operator)
	assert.True(t, *cfg.Operator.Enabled)
	assert.NotNil(t, cfg.Operator.Backend)
	assert.Equal(t, backend.BackendType("kubernetes"), cfg.Operator.Backend.Type)
	assert.NotNil(t, cfg.Stores)
	assert.Equal(t, "http://127.0.0.1:8200", cfg.Stores.Vault.Address)
	assert.NotNil(t, cfg.Queue)
	assert.Equal(t, queue.QueueType("memory"), cfg.Queue.Type)
	assert.NotNil(t, cfg.Metrics)
	assert.Equal(t, 9090, cfg.Metrics.Port)
}

func TestSetDefaults(t *testing.T) {
	var cfg ConfigFile

	err := cfg.SetDefaults()
	assert.NoError(t, err)
	assert.Equal(t, "info", cfg.Log.Level)
	assert.NotNil(t, cfg.Queue)
	assert.Equal(t, queue.QueueType("memory"), cfg.Queue.Type)
	assert.NotNil(t, cfg.Operator.Backend)
	assert.Equal(t, backend.BackendType("kubernetes"), cfg.Operator.Backend.Type)
}
