package notifications

import (
	"context"
	"sync"

	"github.com/robertlestak/vault-secret-sync/api/v1alpha1"
	log "github.com/sirupsen/logrus"
)

// Trigger triggers the webhook for the specified event with the provided data.
func Trigger(ctx context.Context, message v1alpha1.NotificationMessage) error {
	l := log.WithFields(log.Fields{"action": "notifications.Trigger"})
	l.Trace("start")
	defer l.Trace("end")
	if message.VaultSecretSync.Spec.Notifications == nil || len(message.VaultSecretSync.Spec.Notifications) == 0 {
		l.Debug("no notifications configured")
		return nil
	}
	wg := &sync.WaitGroup{}
	wg.Add(3)
	go func() {
		if err := handleWebhooks(ctx, message); err != nil {
			l.WithError(err).Error("failed to handle webhooks")
		}
		wg.Done()
	}()
	go func() {
		if err := handleSlack(ctx, message); err != nil {
			l.WithError(err).Error("failed to handle slack")
		}
		wg.Done()
	}()
	go func() {
		if err := handleEmail(ctx, message); err != nil {
			l.WithError(err).Error("failed to handle email")
		}
		wg.Done()
	}()
	return nil
}
