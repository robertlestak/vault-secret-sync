package sync

import (
	"context"
	"errors"

	"github.com/robertlestak/vault-secret-sync/pkg/driver"
)

type MockSyncClient struct {
	Path       string
	Secrets    map[string]map[string]interface{}
	Include    []string
	Exclude    []string
	ShouldFail bool
}

func (m *MockSyncClient) Meta() map[string]interface{} {
	return map[string]interface{}{
		"path":    m.Path,
		"include": m.Include,
		"exclude": m.Exclude,
	}
}

func (m *MockSyncClient) Init(ctx context.Context) error {
	if m.ShouldFail {
		return errors.New("init failed")
	}
	return nil
}

func (m *MockSyncClient) Validate() error {
	if m.ShouldFail {
		return errors.New("validate failed")
	}
	return nil
}

func (m *MockSyncClient) Driver() driver.DriverName {
	return driver.DriverNameVault
}

func (m *MockSyncClient) GetPath() string {
	return m.Path
}

func (m *MockSyncClient) GetSecret(ctx context.Context, path string) (map[string]interface{}, error) {
	if m.ShouldFail {
		return nil, errors.New("get secret failed")
	}
	secret, exists := m.Secrets[path]
	if !exists {
		return nil, errors.New("secret not found")
	}
	return secret, nil
}

func (m *MockSyncClient) WriteSecret(ctx context.Context, path string, data map[string]interface{}) (map[string]interface{}, error) {
	if m.ShouldFail {
		return nil, errors.New("write secret failed")
	}
	m.Secrets[path] = data
	return data, nil
}

func (m *MockSyncClient) DeleteSecret(ctx context.Context, path string) error {
	if m.ShouldFail {
		return errors.New("delete secret failed")
	}
	delete(m.Secrets, path)
	return nil
}

func (m *MockSyncClient) ListSecrets(ctx context.Context, path string) ([]string, error) {
	if m.ShouldFail {
		return nil, errors.New("list secrets failed")
	}
	var secrets []string
	for key := range m.Secrets {
		if key == path || len(key) > len(path) && key[:len(path)+1] == path+"/" {
			secrets = append(secrets, key[len(path)+1:])
		}
	}
	return secrets, nil
}

func (m *MockSyncClient) SetDefaults(config interface{}) error {
	return nil
}

func (m *MockSyncClient) Close() error {
	return nil
}
