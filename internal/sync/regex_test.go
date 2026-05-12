package sync

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/robertlestak/vault-secret-sync/api/v1alpha1"
	"github.com/robertlestak/vault-secret-sync/pkg/driver"
	"github.com/robertlestak/vault-secret-sync/stores/vault"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type manualRegexTestClient struct {
	path    string
	list    []string
	secrets map[string][]byte
	writes  map[string][]byte
	deletes []string
	listErr error
}

func (m *manualRegexTestClient) Meta() map[string]any {
	return map[string]any{"path": m.path}
}

func (m *manualRegexTestClient) Init(context.Context) error {
	return nil
}

func (m *manualRegexTestClient) Validate() error {
	return nil
}

func (m *manualRegexTestClient) Driver() driver.DriverName {
	return driver.DriverNameVault
}

func (m *manualRegexTestClient) GetPath() string {
	return m.path
}

func (m *manualRegexTestClient) GetSecret(_ context.Context, path string) ([]byte, error) {
	secret, ok := m.secrets[path]
	if !ok {
		return nil, errors.New("secret not found")
	}
	return secret, nil
}

func (m *manualRegexTestClient) WriteSecret(_ context.Context, _ metav1.ObjectMeta, path string, secret []byte) ([]byte, error) {
	if m.writes == nil {
		m.writes = make(map[string][]byte)
	}
	m.writes[path] = append([]byte(nil), secret...)
	return secret, nil
}

func (m *manualRegexTestClient) DeleteSecret(_ context.Context, path string) error {
	m.deletes = append(m.deletes, path)
	delete(m.secrets, path)
	delete(m.writes, path)
	return nil
}

func (m *manualRegexTestClient) ListSecrets(context.Context, string) ([]string, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	return m.list, nil
}

func (m *manualRegexTestClient) SetDefaults(any) error {
	return nil
}

func (m *manualRegexTestClient) Close() error {
	return nil
}

func manualRegexSyncJob(sourcePath string, dryRun bool) SyncJob {
	return SyncJob{
		SyncConfig: v1alpha1.VaultSecretSync{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-sync",
				Namespace: "test-namespace",
			},
			Spec: v1alpha1.VaultSecretSyncSpec{
				Source: &vault.VaultClient{Path: sourcePath},
				DryRun: &dryRun,
			},
		},
	}
}

func runManualRegexWithTimeout(t *testing.T, fn func() error) error {
	t.Helper()

	done := make(chan error, 1)
	go func() {
		done <- fn()
	}()

	select {
	case err := <-done:
		return err
	case <-time.After(2 * time.Second):
		t.Fatal("manual regex operation did not complete")
		return nil
	}
}

func TestHandleManualRegexSyncOnlyWaitsForQueuedMatches(t *testing.T) {
	const sourcePath = "dev-tempo/(GLOBAL)"

	source := &manualRegexTestClient{
		path: sourcePath,
		list: []string{"GLOBAL", "other"},
	}
	dest := &manualRegexTestClient{path: "dev-tempo/$1"}
	sc := &SyncClients{Source: source, Dest: []SyncClient{dest}}

	err := runManualRegexWithTimeout(t, func() error {
		return handleManualRegexSync(context.Background(), sc, manualRegexSyncJob(sourcePath, true))
	})
	if err != nil {
		t.Fatalf("handleManualRegexSync returned error: %v", err)
	}
}

func TestHandleManualRegexSyncNoMatchesDoesNotBlock(t *testing.T) {
	const sourcePath = "dev-tempo/(GLOBAL)"

	source := &manualRegexTestClient{
		path: sourcePath,
		list: []string{"other"},
	}
	dest := &manualRegexTestClient{path: "dev-tempo/$1"}
	sc := &SyncClients{Source: source, Dest: []SyncClient{dest}}

	err := runManualRegexWithTimeout(t, func() error {
		return handleManualRegexSync(context.Background(), sc, manualRegexSyncJob(sourcePath, true))
	})
	if err != nil {
		t.Fatalf("handleManualRegexSync returned error: %v", err)
	}
	if len(dest.writes) != 0 {
		t.Fatalf("expected no writes, got %d", len(dest.writes))
	}
}

func TestHandleManualRegexDeleteOnlyWaitsForQueuedMatches(t *testing.T) {
	const sourcePath = "dev-tempo/(GLOBAL)"

	source := &manualRegexTestClient{
		path: sourcePath,
		list: []string{"GLOBAL", "other"},
	}
	dest := &manualRegexTestClient{
		path: "dev-tempo/$1",
		secrets: map[string][]byte{
			"dev-tempo/GLOBAL": []byte(`{"value":"ok"}`),
		},
	}
	sc := &SyncClients{Source: source, Dest: []SyncClient{dest}}

	err := runManualRegexWithTimeout(t, func() error {
		return handleManualRegexDelete(context.Background(), sc, manualRegexSyncJob(sourcePath, false))
	})
	if err != nil {
		t.Fatalf("handleManualRegexDelete returned error: %v", err)
	}

	if len(dest.deletes) != 1 || dest.deletes[0] != "dev-tempo/GLOBAL" {
		t.Fatalf("expected one delete for dev-tempo/GLOBAL, got %#v", dest.deletes)
	}
}
