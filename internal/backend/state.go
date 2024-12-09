package backend

import (
	"cmp"
	"errors"
	"fmt"
	"strings"

	"github.com/robertlestak/vault-secret-sync/api/v1alpha1"
	"github.com/robertlestak/vault-secret-sync/internal/event"
	log "github.com/sirupsen/logrus"
)

type TenantName string
type TenantNamespace string
type TenantSyncs map[TenantNamespace][]v1alpha1.VaultSecretSync

var (
	SyncConfigs map[string]v1alpha1.VaultSecretSync
	SyncMaps    map[TenantName]TenantSyncs
)

func init() {
	SyncConfigs = make(map[string]v1alpha1.VaultSecretSync)
	SyncMaps = make(map[TenantName]TenantSyncs)
}

func addToSyncMaps(config v1alpha1.VaultSecretSync) {
	tenant, namespace, _ := SourceTenantNamespace(config)
	tn := TenantName(tenant)
	tns := TenantNamespace(namespace)

	if _, ok := SyncMaps[tn]; !ok {
		SyncMaps[tn] = make(TenantSyncs)
	}
	SyncMaps[tn][tns] = append(SyncMaps[tn][tns], config)
}

func removeFromSyncMaps(config v1alpha1.VaultSecretSync) {
	tenant, namespace, _ := SourceTenantNamespace(config)
	tn := TenantName(tenant)
	tns := TenantNamespace(namespace)

	if tenantSyncs, ok := SyncMaps[tn]; ok {
		if namespaceSyncs, ok := tenantSyncs[tns]; ok {
			for i, c := range namespaceSyncs {
				if c.Name == config.Name && c.Namespace == config.Namespace {
					SyncMaps[tn][tns] = append(namespaceSyncs[:i], namespaceSyncs[i+1:]...)
					break
				}
			}
			if len(SyncMaps[tn][tns]) == 0 {
				delete(tenantSyncs, tns)
			}
			if len(tenantSyncs) == 0 {
				delete(SyncMaps, tn)
			}
		}
	}
}

func SourceTenantNamespace(sc v1alpha1.VaultSecretSync) (string, string, error) {
	if sc.Spec.Source == nil {
		return "", "", errors.New("source is nil")
	}
	tenant := sc.Spec.Source.Address
	namespace := sc.Spec.Source.Namespace
	if tenant == "" {
		return "", "", errors.New("tenant is empty")
	}
	if namespace == "" {
		namespace = "default"
	}
	return tenant, namespace, nil
}

func GetSyncConfigByName(name string) (v1alpha1.VaultSecretSync, error) {
	v, ok := SyncConfigs[name]
	if ok {
		return v, nil
	}
	return v1alpha1.VaultSecretSync{}, errors.New("no config found")
}

func TenantNamespaceConfigs(evt event.VaultEvent) []v1alpha1.VaultSecretSync {
	l := log.WithFields(log.Fields{
		"action":  "TenantNamespaceConfigs",
		"eventId": evt.ID,
		"path":    evt.Path,
		"op":      evt.Operation,
	})
	ns := strings.TrimRight(evt.Namespace, "/")
	ns = cmp.Or(ns, "default")
	l = l.WithFields(log.Fields{"namespace": ns})
	evt.Address = strings.TrimRight(evt.Address, "/")
	l = l.WithFields(log.Fields{"tenant": evt.Address})
	l.Trace("start")
	defer l.Trace("end")
	tn := TenantName(evt.Address)
	tns := TenantNamespace(ns)
	var result []v1alpha1.VaultSecretSync
	if tenantSyncs, ok := SyncMaps[tn]; ok {
		if namespaceSyncs, ok := tenantSyncs[tns]; ok {
			result = append(result, namespaceSyncs...)
		}
	}
	return result
}

func AddSyncConfig(s v1alpha1.VaultSecretSync) error {
	internalName := InternalName(s.Namespace, s.Name)

	// Check if the config already exists
	if existingConfig, exists := SyncConfigs[internalName]; exists {
		// If it exists, remove the old config from SyncMaps
		removeFromSyncMaps(existingConfig)
	}

	// Add the new config
	SyncConfigs[internalName] = s
	addToSyncMaps(s)
	return nil
}

func RemoveSyncConfig(name string) error {
	config, exists := SyncConfigs[name]
	if !exists {
		return errors.New("sync config not found")
	}
	delete(SyncConfigs, name)
	removeFromSyncMaps(config)
	return nil
}

func InternalName(namespace, name string) string {
	return fmt.Sprintf("%s/%s", namespace, name)
}

func FromInternalName(in string) (string, string) {
	s := strings.Split(in, "/")
	if len(s) != 2 {
		return "", ""
	}
	return s[0], s[1]
}
