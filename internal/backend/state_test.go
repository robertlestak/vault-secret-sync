package backend

import (
	"testing"

	"github.com/robertlestak/vault-secret-sync/api/v1alpha1"
	"github.com/robertlestak/vault-secret-sync/internal/event"
	"github.com/robertlestak/vault-secret-sync/stores/vault"
	"github.com/stretchr/testify/assert"
	metav1alpha1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestSourceTenantNamespace(t *testing.T) {
	syncConfig := v1alpha1.VaultSecretSync{
		Spec: v1alpha1.VaultSecretSyncSpec{
			Source: &vault.VaultClient{
				Address:   "tenant1",
				Namespace: "namespace1",
			},
		},
	}

	tenant, namespace, err := SourceTenantNamespace(syncConfig)

	assert.NoError(t, err)
	assert.Equal(t, "tenant1", tenant)
	assert.Equal(t, "namespace1", namespace)
}

func TestSourceTenantNamespace_MissingSource(t *testing.T) {
	syncConfig := v1alpha1.VaultSecretSync{
		Spec: v1alpha1.VaultSecretSyncSpec{
			Dest: []*v1alpha1.StoreConfig{
				{
					Vault: &vault.VaultClient{
						Address:   "tenant1",
						Namespace: "namespace1",
					},
				},
			},
		},
	}

	tenant, namespace, err := SourceTenantNamespace(syncConfig)

	assert.Error(t, err)
	assert.Equal(t, "", tenant)
	assert.Equal(t, "", namespace)
}

func TestGetSyncConfigByName(t *testing.T) {
	syncConfig := v1alpha1.VaultSecretSync{
		Spec: v1alpha1.VaultSecretSyncSpec{
			Source: &vault.VaultClient{
				Address:   "tenant1",
				Namespace: "namespace1",
			},
			Dest: []*v1alpha1.StoreConfig{
				{
					Vault: &vault.VaultClient{
						Address:   "tenant1",
						Namespace: "namespace1",
					},
				},
			},
		},
	}

	SyncConfigs = map[string]v1alpha1.VaultSecretSync{
		"config1": syncConfig,
	}

	result, err := GetSyncConfigByName("config1")

	assert.NoError(t, err)
	assert.Equal(t, syncConfig, result)
}

func TestGetSyncConfigByName_NotFound(t *testing.T) {
	SyncConfigs = map[string]v1alpha1.VaultSecretSync{}

	result, err := GetSyncConfigByName("config1")

	assert.Error(t, err)
	assert.Equal(t, v1alpha1.VaultSecretSync{}, result)
}

func TestTenantNamespaceConfigs(t *testing.T) {
	syncConfig1 := v1alpha1.VaultSecretSync{
		ObjectMeta: metav1alpha1.ObjectMeta{
			Name:      "config1",
			Namespace: "namespace1",
		},
		Spec: v1alpha1.VaultSecretSyncSpec{
			Source: &vault.VaultClient{
				Address:   "tenant1",
				Namespace: "namespace1",
			},
		},
	}
	syncConfig2 := v1alpha1.VaultSecretSync{
		ObjectMeta: metav1alpha1.ObjectMeta{
			Name:      "config2",
			Namespace: "namespace2",
		},
		Spec: v1alpha1.VaultSecretSyncSpec{
			Source: &vault.VaultClient{
				Address:   "tenant2",
				Namespace: "namespace2",
			},
		},
	}
	syncConfig3 := v1alpha1.VaultSecretSync{
		ObjectMeta: metav1alpha1.ObjectMeta{
			Name:      "config3",
			Namespace: "namespace1",
		},
		Spec: v1alpha1.VaultSecretSyncSpec{
			Source: &vault.VaultClient{
				Address:   "tenant1",
				Namespace: "namespace1",
			},
		},
	}

	syncConfig4 := v1alpha1.VaultSecretSync{
		ObjectMeta: metav1alpha1.ObjectMeta{
			Name:      "config4",
			Namespace: "namespace1",
		},
		Spec: v1alpha1.VaultSecretSyncSpec{
			Source: &vault.VaultClient{
				Address:   "tenant1/",
				Namespace: "namespace1/",
			},
		},
	}

	SyncMaps = map[TenantName]TenantSyncs{
		"tenant1": {
			"namespace1": []v1alpha1.VaultSecretSync{syncConfig1, syncConfig3, syncConfig4},
		},
		"tenant2": {
			"namespace2": []v1alpha1.VaultSecretSync{syncConfig2},
		},
	}

	evt := event.VaultEvent{
		Address:   "tenant1",
		Namespace: "namespace1",
	}

	result := TenantNamespaceConfigs(evt)

	assert.Len(t, result, 3)
	assert.Contains(t, result, syncConfig1)
	assert.Contains(t, result, syncConfig3)
	assert.Contains(t, result, syncConfig4)
}

func TestAddSyncConfig_DuplicateAddressNamespacePath(t *testing.T) {
	syncConfig1 := v1alpha1.VaultSecretSync{
		ObjectMeta: metav1alpha1.ObjectMeta{
			Name:      "config1",
			Namespace: "namespace1",
		},
		Spec: v1alpha1.VaultSecretSyncSpec{
			Source: &vault.VaultClient{
				Address:   "tenant1",
				Namespace: "namespace1",
			},
		},
	}
	syncConfig2 := v1alpha1.VaultSecretSync{
		ObjectMeta: metav1alpha1.ObjectMeta{
			Name:      "config2",
			Namespace: "namespace1",
		},
		Spec: v1alpha1.VaultSecretSyncSpec{
			Source: &vault.VaultClient{
				Address:   "tenant1",
				Namespace: "namespace1",
			},
		},
	}

	SyncConfigs = map[string]v1alpha1.VaultSecretSync{}
	SyncMaps = make(map[TenantName]TenantSyncs)

	err1 := AddSyncConfig(syncConfig1)
	err2 := AddSyncConfig(syncConfig2)

	assert.NoError(t, err1)
	assert.NoError(t, err2)

	evt := event.VaultEvent{
		Address:   "tenant1",
		Namespace: "namespace1",
	}

	result := TenantNamespaceConfigs(evt)

	assert.Len(t, result, 2)
	assert.Contains(t, result, syncConfig1)
	assert.Contains(t, result, syncConfig2)
}
