package notifications

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/robertlestak/vault-secret-sync/api/v1alpha1"
	"github.com/robertlestak/vault-secret-sync/internal/kube"
	log "github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

func handleNotificationTemplate(ctx context.Context, kc kubernetes.Interface, message *v1alpha1.NotificationMessage) error {
	l := log.WithFields(log.Fields{
		"pkg":           "notifications",
		"action":        "handleNotificationTemplate",
		"syncConfig":    message.VaultSecretSync.ObjectMeta.Name,
		"syncNamespace": message.VaultSecretSync.ObjectMeta.Namespace,
	})
	l.Trace("start")
	defer l.Trace("end")

	if message.VaultSecretSync.Spec.NotificationsTemplate == nil {
		return fmt.Errorf("notifications template is not configured")
	}

	template := *message.VaultSecretSync.Spec.NotificationsTemplate
	parts := strings.Split(template, "/")
	if len(parts) < 2 || len(parts) > 3 {
		return fmt.Errorf("invalid template format: %s", template)
	}

	var namespace, configMapName, key string
	if len(parts) == 2 {
		namespace = message.VaultSecretSync.ObjectMeta.Namespace
		configMapName = parts[0]
		key = parts[1]
	} else {
		namespace = parts[0]
		configMapName = parts[1]
		key = parts[2]
	}

	cm, err := kc.CoreV1().ConfigMaps(namespace).Get(ctx, configMapName, v1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get configmap: %w", err)
	}

	templateContent, ok := cm.Data[key]
	if !ok {
		return fmt.Errorf("key %s not found in configmap %s", key, configMapName)
	}

	var notifConfig []*v1alpha1.NotificationSpec
	if err := yaml.Unmarshal([]byte(templateContent), &notifConfig); err != nil {
		return fmt.Errorf("failed to unmarshal notification template: %w", err)
	}

	message.VaultSecretSync.Spec.Notifications = notifConfig
	return nil
}

// Trigger triggers the webhook for the specified event with the provided data.
func Trigger(ctx context.Context, message v1alpha1.NotificationMessage) error {
	l := log.WithFields(log.Fields{
		"pkg":           "notifications",
		"action":        "notifications.Trigger",
		"syncConfig":    message.VaultSecretSync.ObjectMeta.Name,
		"syncNamespace": message.VaultSecretSync.ObjectMeta.Namespace,
		"notifications": len(message.VaultSecretSync.Spec.Notifications),
	})
	l.Trace("start")
	defer l.Trace("end")
	if message.VaultSecretSync.Spec.NotificationsTemplate != nil {
		l.Debug("notifications template configured")
		kc, err := kube.CreateKubeClient()
		if err != nil {
			l.WithError(err).Error("failed to create kubernetes client")
			return fmt.Errorf("failed to create kubernetes client: %w", err)
		}

		if err := handleNotificationTemplate(ctx, kc, &message); err != nil {
			l.WithError(err).Error("failed to handle notifications template")
			return fmt.Errorf("failed to handle notifications template: %w", err)
		}
	}
	if len(message.VaultSecretSync.Spec.Notifications) == 0 {
		l.Debug("no notifications configured")
		return nil
	}
	wg := &sync.WaitGroup{}
	var mu sync.Mutex
	var errs []error
	wg.Add(3)
	go func() {
		defer wg.Done()
		ll := l.WithField("notificationType", "webhooks")
		if err := handleWebhooks(ctx, message); err != nil {
			ll.WithError(err).Error("failed to handle webhooks")
			mu.Lock()
			errs = append(errs, err)
			mu.Unlock()
		}
	}()
	go func() {
		defer wg.Done()
		ll := l.WithField("notificationType", "slack")
		if err := handleSlack(ctx, message); err != nil {
			ll.WithError(err).Error("failed to handle slack")
			mu.Lock()
			errs = append(errs, err)
			mu.Unlock()
		}
	}()
	go func() {
		defer wg.Done()
		ll := l.WithField("notificationType", "email")
		if err := handleEmail(ctx, message); err != nil {
			ll.WithError(err).Error("failed to handle email")
			mu.Lock()
			errs = append(errs, err)
			mu.Unlock()
		}
	}()
	wg.Wait()
	if len(errs) > 0 {
		l.WithFields(log.Fields{
			"errorCount": len(errs),
			"errors":     errs,
		}).Errorf("failed to handle %d/%d notifications", len(errs), len(message.VaultSecretSync.Spec.Notifications))
		return fmt.Errorf("failed to handle notifications: %v", errs)
	} else {
		l.Info("all notifications handled successfully")
	}
	return nil
}
