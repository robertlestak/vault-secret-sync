package sync

import (
	"context"
	"net"

	"github.com/robertlestak/vault-secret-sync/api/v1alpha1"
	"github.com/robertlestak/vault-secret-sync/internal/backend"
	"github.com/robertlestak/vault-secret-sync/internal/event"
	"github.com/robertlestak/vault-secret-sync/pkg/driver"
	log "github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type SyncClient interface {
	Meta() map[string]any
	Init(context.Context) error
	Validate() error
	Driver() driver.DriverName
	GetPath() string
	GetSecret(context.Context, string) (map[string]any, error)
	WriteSecret(context.Context, metav1.ObjectMeta, string, map[string]any) (map[string]any, error)
	DeleteSecret(context.Context, string) error
	ListSecrets(context.Context, string) ([]string, error)
	SetDefaults(any) error
	Close() error
}

func SetStoreDefaults(sc *v1alpha1.StoreConfig) {
	l := log.WithFields(log.Fields{
		"action": "setStoreDefaults",
	})
	l.Trace("start")
	defer l.Trace("end")
	if DefaultConfigs == nil {
		DefaultConfigs = make(map[driver.DriverName]*v1alpha1.StoreConfig)
	}
	if sc.AWS != nil {
		DefaultConfigs[driver.DriverNameAws] = sc
	}
	if sc.GCP != nil {
		DefaultConfigs[driver.DriverNameGcp] = sc
	}
	if sc.GitHub != nil {
		DefaultConfigs[driver.DriverNameGitHub] = sc
	}
	if sc.Vault != nil {
		DefaultConfigs[driver.DriverNameVault] = sc
	}
	if sc.HTTP != nil {
		DefaultConfigs[driver.DriverNameHttp] = sc
	}
}

func DestinationStoreNames(sc v1alpha1.VaultSecretSync) []driver.DriverName {
	var destDrivers []driver.DriverName
	for _, d := range sc.Spec.Dest {
		if d.AWS != nil {
			destDrivers = append(destDrivers, driver.DriverNameAws)
		}
		if d.GCP != nil {
			destDrivers = append(destDrivers, driver.DriverNameGcp)
		}
		if d.GitHub != nil {
			destDrivers = append(destDrivers, driver.DriverNameGitHub)
		}
		if d.Vault != nil {
			destDrivers = append(destDrivers, driver.DriverNameVault)
		}
		if d.HTTP != nil {
			destDrivers = append(destDrivers, driver.DriverNameHttp)
		}
	}
	return destDrivers
}

// getAddressForEvent returns the Vault address for a given vault Event
func GetAddressForEvent(event event.AuditEvent) string {
	l := log.WithFields(log.Fields{
		"action": "getAddressForEvent",
	})
	l.Trace("start")
	for _, v := range backend.SyncConfigs {
		scs, err := InitSyncConfigClients(v)
		if err != nil {
			l.Error(err)
			continue
		}
		if driver.DriverName(scs.Source.Driver()) != driver.DriverNameVault {
			l.Error("source driver is not vault")
			continue
		}
		l.Debugf("vaultTenant=%s sourceMeta=%+v", event.VaultTenant, scs.Source.Meta())
		var metaAddrStr, metaCidrStr string
		if maex, ok := scs.Source.Meta()["address"]; ok {
			if v, ok := maex.(string); ok {
				metaAddrStr = v
			}
		}
		if mcex, ok := scs.Source.Meta()["cidr"]; ok {
			if v, ok := mcex.(string); ok {
				metaCidrStr = v
			}
		}
		if event.VaultTenant != "" && event.VaultTenant == scs.Source.Meta()["address"] {
			l.WithField("address", metaAddrStr).Debug("found address in source meta")
			return metaAddrStr
		} else if metaCidrStr != "" && cidrContainsIP(metaCidrStr, event.RemoteAddr) {
			l.WithField("address", metaAddrStr).Debug("found address in source meta cidr")
			return metaAddrStr
		}
	}
	l.Trace("end")
	return "unknown"
}

func NewVaultEventFromAuditEvent(e event.AuditEvent) event.VaultEvent {
	var va string
	if e.VaultTenant != "" {
		va = e.VaultTenant
	} else {
		va = GetAddressForEvent(e)
	}
	evt := event.VaultEvent{
		EventId:   e.Event.Request.ID,
		Address:   va,
		Path:      e.Event.Request.Path,
		Operation: e.Event.Request.Operation,
		Manual:    false,
	}
	if e.Event.Request.Namespace != nil && e.Event.Request.Namespace.Path != "" {
		evt.Namespace = e.Event.Request.Namespace.Path
	}
	return evt
}

// cidrContainsIP checks if a given IP address is contained within a given CIDR
func cidrContainsIP(cidr string, ip string) bool {
	_, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		return false
	}
	return ipnet.Contains(net.ParseIP(ip))
}
