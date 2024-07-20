package backend

import (
	"context"
	"fmt"

	"github.com/hashicorp/vault/sdk/logical"
	"github.com/robertlestak/vault-secret-sync/api/v1alpha1"
	"github.com/robertlestak/vault-secret-sync/internal/metrics"
	log "github.com/sirupsen/logrus"
)

var (
	B             Backend
	ManualTrigger func(ctx context.Context, cfg v1alpha1.VaultSecretSync, op logical.Operation) error
)

const (
	BackendTypeKubernetes BackendType = "kubernetes"
)

type BackendType string

type Backend interface {
	Start(context.Context, map[string]any) error
	Type() BackendType
}

func NewBackend(t BackendType) (Backend, error) {
	l := log.WithFields(log.Fields{
		"action": "NewBackend",
		"type":   t,
	})
	l.Trace("start")
	defer l.Trace("end")
	switch t {
	case BackendTypeKubernetes:
		return NewKubernetesBackend(), nil
	default:
		return nil, fmt.Errorf("unknown backend type: %s", t)
	}
}

func InitBackend(ctx context.Context, params map[string]any) error {
	// default to kubernetes backend for now until app is more mature
	t := BackendTypeKubernetes
	l := log.WithFields(log.Fields{
		"action": "InitBackend",
		"type":   t,
	})
	l.Trace("start")
	defer l.Trace("end")
	if t == "" {
		t = BackendTypeKubernetes
	}
	b, err := NewBackend(t)
	if err != nil {
		l.Errorf("error: %v", err)
		metrics.RegisterServiceHealth("backend", metrics.ServiceHealthStatusCritical)
		return err
	}
	if err := b.Start(ctx, params); err != nil {
		l.Errorf("error: %v", err)
		metrics.RegisterServiceHealth("backend", metrics.ServiceHealthStatusCritical)
		return err
	}
	B = b
	metrics.RegisterServiceHealth("backend", metrics.ServiceHealthStatusOK)
	return nil
}
