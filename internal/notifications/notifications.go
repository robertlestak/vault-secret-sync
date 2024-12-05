package notifications

import (
	"context"
	"fmt"
	"sync"

	"github.com/robertlestak/vault-secret-sync/api/v1alpha1"
	log "github.com/sirupsen/logrus"
)

// Trigger triggers the webhook for the specified event with the provided data.
func Trigger(ctx context.Context, message v1alpha1.NotificationMessage) error {
	l := log.WithFields(log.Fields{
		"pkg":           "notifications",
		"action":        "notifications.Trigger",
		"syncConfig":    message.VaultSecretSync.ObjectMeta.Name,
		"syncNamespace": message.VaultSecretSync.ObjectMeta.Namespace,
	})
	l.Trace("start")
	defer l.Trace("end")
	if message.VaultSecretSync.Spec.Notifications == nil || len(message.VaultSecretSync.Spec.Notifications) == 0 {
		l.Debug("no notifications configured")
		return nil
	}
	wg := &sync.WaitGroup{}
	wg.Add(3)
	var errs []error
	go func() {
		ll := l.WithField("notificationType", "webhooks")
		if err := handleWebhooks(ctx, message); err != nil {
			ll.WithError(err).Error("failed to handle webhooks")
			errs = append(errs, err)
		}
		wg.Done()
	}()
	go func() {
		ll := l.WithField("notificationType", "slack")
		if err := handleSlack(ctx, message); err != nil {
			ll.WithError(err).Error("failed to handle slack")
			errs = append(errs, err)
		}
		wg.Done()
	}()
	go func() {
		ll := l.WithField("notificationType", "email")
		if err := handleEmail(ctx, message); err != nil {
			ll.WithError(err).Error("failed to handle email")
			errs = append(errs, err)
		}
		wg.Done()
	}()
	wg.Wait()
	if len(errs) > 0 {
		l.WithField("errors", errs).Error("failed to handle notifications")
		return fmt.Errorf("failed to handle notifications: %v", errs)
	} else {
		l.Info("all notifications handled successfully")
	}
	return nil
}
