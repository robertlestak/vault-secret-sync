package vault

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/hashicorp/vault/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockVaultLogical simulates Vault's logical backend for testing
type mockVaultLogical struct {
	mu              sync.Mutex
	secrets         map[string]map[string]interface{}
	versions        map[string]int
	readCalls       int
	writeCalls      int
	casFailureCount int
}

func newMockVaultLogical() *mockVaultLogical {
	return &mockVaultLogical{
		secrets:  make(map[string]map[string]interface{}),
		versions: make(map[string]int),
	}
}

func (m *mockVaultLogical) read(ctx context.Context, path string) (map[string]interface{}, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.readCalls++

	if contains(path, "/metadata/") {
		basePath := extractBasePath(path)
		version := m.versions[basePath]
		if version == 0 {
			return nil, fmt.Errorf("secret not found")
		}
		return map[string]interface{}{
			"current_version": version,
		}, nil
	}

	if contains(path, "/data/") {
		basePath := extractBasePath(path)
		data := m.secrets[basePath]
		if data == nil {
			return nil, fmt.Errorf("secret not found")
		}
		return map[string]interface{}{
			"data": data,
		}, nil
	}

	return nil, fmt.Errorf("unknown path: %s", path)
}

func (m *mockVaultLogical) write(ctx context.Context, path string, data map[string]interface{}) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.writeCalls++

	basePath := extractBasePath(path)
	currentVersion := m.versions[basePath]

	if options, ok := data["options"].(map[string]interface{}); ok {
		if casVal, ok := options["cas"]; ok {
			expectedCAS := casVal.(int)

			if m.casFailureCount > 0 {
				m.casFailureCount--
				return fmt.Errorf("check-and-set parameter did not match")
			}

			if expectedCAS != currentVersion {
				return fmt.Errorf("check-and-set parameter did not match: expected %d, got %d", currentVersion, expectedCAS)
			}
		}
	}

	if secretData, ok := data["data"].(map[string]interface{}); ok {
		m.secrets[basePath] = secretData
		m.versions[basePath] = currentVersion + 1
	}

	return nil
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func extractBasePath(path string) string {
	path = replaceAll(path, "/data/", "/")
	path = replaceAll(path, "/metadata/", "/")
	return path
}

func replaceAll(s, old, new string) string {
	result := ""
	for {
		idx := indexOf(s, old)
		if idx == -1 {
			result += s
			break
		}
		result += s[:idx] + new
		s = s[idx+len(old):]
	}
	return result
}

func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

func TestConcurrentWrites_CASConflict(t *testing.T) {
	mock := newMockVaultLogical()

	mock.secrets["kv/test/secret"] = map[string]interface{}{
		"existing_key": "existing_value",
	}
	mock.versions["kv/test/secret"] = 1

	ctx := context.Background()

	var wg sync.WaitGroup
	errors := make([]error, 2)

	wg.Add(2)
	go func() {
		defer wg.Done()
		dataToWrite := map[string]interface{}{
			"existing_key": "existing_value",
			"key1":         "value1",
		}
		errors[0] = mock.write(ctx, "kv/data/test/secret", map[string]interface{}{
			"data": dataToWrite,
			"options": map[string]interface{}{
				"cas": 1,
			},
		})
	}()

	go func() {
		defer wg.Done()
		dataToWrite := map[string]interface{}{
			"existing_key": "existing_value",
			"key2":         "value2",
		}
		errors[1] = mock.write(ctx, "kv/data/test/secret", map[string]interface{}{
			"data": dataToWrite,
			"options": map[string]interface{}{
				"cas": 1,
			},
		})
	}()

	wg.Wait()

	successCount := 0
	casFailCount := 0
	for _, err := range errors {
		if err == nil {
			successCount++
		} else if contains(err.Error(), "check-and-set") {
			casFailCount++
		}
	}

	assert.Equal(t, 1, successCount, "exactly one write should succeed")
	assert.Equal(t, 1, casFailCount, "exactly one write should fail with CAS error")
	assert.Equal(t, 2, mock.versions["kv/test/secret"], "version should be incremented once")
}

func TestGetSecretWithVersion_Success(t *testing.T) {
	mock := newMockVaultLogical()

	mock.secrets["kv/test/secret"] = map[string]interface{}{
		"key1": "value1",
		"key2": "value2",
	}
	mock.versions["kv/test/secret"] = 5

	ctx := context.Background()

	metadata, err := mock.read(ctx, "kv/metadata/test/secret")
	require.NoError(t, err)

	secret, err := mock.read(ctx, "kv/data/test/secret")
	require.NoError(t, err)

	version := metadata["current_version"].(int)
	assert.Equal(t, 5, version)

	data := secret["data"].(map[string]interface{})
	assert.Equal(t, "value1", data["key1"])
	assert.Equal(t, "value2", data["key2"])
}

func TestCASRetry_EventualSuccess(t *testing.T) {
	mock := newMockVaultLogical()

	mock.secrets["kv/test/secret"] = map[string]interface{}{
		"existing": "data",
	}
	mock.versions["kv/test/secret"] = 1
	mock.casFailureCount = 2

	ctx := context.Background()

	metadata, err := mock.read(ctx, "kv/metadata/test/secret")
	require.NoError(t, err)
	version := metadata["current_version"].(int)
	assert.Equal(t, 1, version)

	dataToWrite := map[string]interface{}{
		"existing": "data",
		"new_key":  "new_value",
	}

	err = mock.write(ctx, "kv/data/test/secret", map[string]interface{}{
		"data": dataToWrite,
		"options": map[string]interface{}{
			"cas": version,
		},
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "check-and-set")

	err = mock.write(ctx, "kv/data/test/secret", map[string]interface{}{
		"data": dataToWrite,
		"options": map[string]interface{}{
			"cas": version,
		},
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "check-and-set")

	err = mock.write(ctx, "kv/data/test/secret", map[string]interface{}{
		"data": dataToWrite,
		"options": map[string]interface{}{
			"cas": version,
		},
	})
	assert.NoError(t, err)

	assert.Equal(t, 2, mock.versions["kv/test/secret"])
	assert.Equal(t, "new_value", mock.secrets["kv/test/secret"]["new_key"])
}

func TestMergeLogic(t *testing.T) {
	tests := []struct {
		name     string
		existing map[string]interface{}
		newData  map[string]interface{}
		merge    bool
		expected map[string]interface{}
	}{
		{
			name:     "merge enabled - combines keys",
			existing: map[string]interface{}{"key1": "value1"},
			newData:  map[string]interface{}{"key2": "value2"},
			merge:    true,
			expected: map[string]interface{}{"key1": "value1", "key2": "value2"},
		},
		{
			name:     "merge enabled - overwrites existing keys",
			existing: map[string]interface{}{"key1": "old"},
			newData:  map[string]interface{}{"key1": "new"},
			merge:    true,
			expected: map[string]interface{}{"key1": "new"},
		},
		{
			name:     "merge disabled - replaces all",
			existing: map[string]interface{}{"key1": "value1"},
			newData:  map[string]interface{}{"key2": "value2"},
			merge:    false,
			expected: map[string]interface{}{"key2": "value2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dataToWrite := tt.newData
			if tt.merge && tt.existing != nil {
				dataToWrite = make(map[string]interface{})
				for k, v := range tt.existing {
					dataToWrite[k] = v
				}
				for k, v := range tt.newData {
					dataToWrite[k] = v
				}
			}

			assert.Equal(t, tt.expected, dataToWrite)
		})
	}
}

func TestSimulateConcurrentUflipRotation(t *testing.T) {
	mock := newMockVaultLogical()

	// Initial state - both old secrets
	mock.secrets["kv/apps/example-api/secrets"] = map[string]interface{}{
		"api_secret": "old_api_secret",
		"ui_secret":  "old_ui_secret",
	}
	mock.versions["kv/apps/example-api/secrets"] = 10

	ctx := context.Background()

	// Both syncs read the same version first
	metadata, _ := mock.read(ctx, "kv/metadata/apps/example-api/secrets")
	initialVersion := metadata["current_version"].(int)

	secret, _ := mock.read(ctx, "kv/data/apps/example-api/secrets")
	initialData := secret["data"].(map[string]interface{})

	// Prepare both merged datasets
	apiMerged := make(map[string]interface{})
	for k, v := range initialData {
		apiMerged[k] = v
	}
	apiMerged["api_secret"] = "new_api_secret"

	uiMerged := make(map[string]interface{})
	for k, v := range initialData {
		uiMerged[k] = v
	}
	uiMerged["ui_secret"] = "new_ui_secret"

	// Now both try to write with the same CAS version
	var wg sync.WaitGroup
	results := make([]error, 2)

	wg.Add(2)
	go func() {
		defer wg.Done()
		results[0] = mock.write(ctx, "kv/data/apps/example-api/secrets", map[string]interface{}{
			"data": apiMerged,
			"options": map[string]interface{}{
				"cas": initialVersion,
			},
		})
	}()

	go func() {
		defer wg.Done()
		results[1] = mock.write(ctx, "kv/data/apps/example-api/secrets", map[string]interface{}{
			"data": uiMerged,
			"options": map[string]interface{}{
				"cas": initialVersion,
			},
		})
	}()

	wg.Wait()

	// One should succeed, one should get CAS error (needs retry)
	successCount := 0
	failCount := 0
	for _, err := range results {
		if err == nil {
			successCount++
		} else {
			failCount++
		}
	}

	assert.Equal(t, 1, successCount, "one sync should succeed")
	assert.Equal(t, 1, failCount, "one sync should fail and need retry")
	assert.Equal(t, 11, mock.versions["kv/apps/example-api/secrets"], "version should increment once")

	// The failed sync should retry with new version
	metadata, _ = mock.read(ctx, "kv/metadata/apps/example-api/secrets")
	newVersion := metadata["current_version"].(int)

	secret, _ = mock.read(ctx, "kv/data/apps/example-api/secrets")
	currentData := secret["data"].(map[string]interface{})

	// Merge the other secret
	merged := make(map[string]interface{})
	for k, v := range currentData {
		merged[k] = v
	}
	merged["api_secret"] = "new_api_secret"
	merged["ui_secret"] = "new_ui_secret"

	err := mock.write(ctx, "kv/data/apps/example-api/secrets", map[string]interface{}{
		"data": merged,
		"options": map[string]interface{}{
			"cas": newVersion,
		},
	})

	assert.NoError(t, err, "retry should succeed")
	assert.Equal(t, 12, mock.versions["kv/apps/example-api/secrets"])

	// Verify both secrets are now updated
	finalSecret, _ := mock.read(ctx, "kv/data/apps/example-api/secrets")
	finalData := finalSecret["data"].(map[string]interface{})
	assert.Equal(t, "new_api_secret", finalData["api_secret"])
	assert.Equal(t, "new_ui_secret", finalData["ui_secret"])
}

func TestListSecretsOnceAllowsMountRoot(t *testing.T) {
	var method string
	var requestedPath string
	var requestedQuery string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method = r.Method
		requestedPath = r.URL.Path
		requestedQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"keys":["GLOBAL","stores/"]}}`))
	}))
	defer server.Close()

	client, err := api.NewClient(&api.Config{Address: server.URL})
	require.NoError(t, err)

	vc := &VaultClient{Client: client}
	keys, err := vc.ListSecretsOnce(context.Background(), "dev-tempo")
	require.NoError(t, err)

	assert.Equal(t, http.MethodGet, method)
	assert.Equal(t, "/v1/dev-tempo/metadata", requestedPath)
	assert.Equal(t, "list=true", requestedQuery)
	assert.Equal(t, []string{"GLOBAL", "stores/"}, keys)
}
