package notifications

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/robertlestak/vault-secret-sync/api/v1alpha1"
	"github.com/robertlestak/vault-secret-sync/internal/backend"
	"github.com/robertlestak/vault-secret-sync/internal/config"
	"github.com/robertlestak/vault-secret-sync/pkg/kubesecret"
	log "github.com/sirupsen/logrus"
)

func triggerWebhook(ctx context.Context, message v1alpha1.NotificationMessage, webhook v1alpha1.WebhookNotification) error {
	if webhook.Method == "" && config.Config.Notifications.Webhook.Method != "" {
		webhook.Method = config.Config.Notifications.Webhook.Method
	}
	if len(webhook.Headers) == 0 && len(config.Config.Notifications.Webhook.Headers) > 0 {
		webhook.Headers = config.Config.Notifications.Webhook.Headers
	}
	if webhook.URL == "" && config.Config.Notifications.Webhook.URL != "" {
		webhook.URL = config.Config.Notifications.Webhook.URL
	}
	if webhook.Body == "" && config.Config.Notifications.Webhook.Body != "" {
		webhook.Body = config.Config.Notifications.Webhook.Body
	}
	if webhook.Method == "" {
		webhook.Method = "POST"
	}
	var payloadBuffer bytes.Buffer
	if webhook.ExcludeBody {
		payloadBuffer.Write([]byte{})
	} else {
		payload := messagePayload(message, webhook.Body)
		payloadBuffer.Write([]byte(payload))
	}
	// Create the HTTP request
	req, err := http.NewRequestWithContext(ctx, webhook.Method, webhook.URL, &payloadBuffer)
	if err != nil {
		log.WithError(err).Error("failed to create webhook request")
		return fmt.Errorf("failed to create webhook request: %v", err)
	}
	if webhook.HeaderSecret != nil && *webhook.HeaderSecret != "" {
		sc, err := kubesecret.GetSecret(ctx, message.VaultSecretSync.Namespace, *webhook.HeaderSecret)
		if err != nil {
			log.WithError(err).Error("failed to get secret for webhook headers")
			return err
		}
		for key, value := range sc {
			webhook.Headers[key] = string(value)
		}
	}
	// Set headers
	for key, value := range webhook.Headers {
		req.Header.Set(key, value)
	}
	c := &http.Client{}
	// Execute the request
	resp, err := c.Do(req)
	if err != nil {
		log.WithError(err).Error("failed to execute webhook request")
		return fmt.Errorf("failed to execute webhook request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			backend.WriteEvent(
				ctx,
				message.VaultSecretSync.Namespace,
				message.VaultSecretSync.Name,
				"Warning",
				string(backend.SyncStatusFailed),
				fmt.Sprintf("webhook request failed with status %d", resp.StatusCode),
			)
			log.WithError(err).Error("failed to read response body")
			return fmt.Errorf("webhook request failed with status %d", resp.StatusCode)
		}
		backend.WriteEvent(
			ctx,
			message.VaultSecretSync.Namespace,
			message.VaultSecretSync.Name,
			"Warning",
			string(backend.SyncStatusFailed),
			fmt.Sprintf("webhook request failed with status %d: %s", resp.StatusCode, body),
		)
		return fmt.Errorf("webhook request failed with status %d: %s", resp.StatusCode, body)
	}
	backend.WriteEvent(
		ctx,
		message.VaultSecretSync.Namespace,
		message.VaultSecretSync.Name,
		"Normal",
		"WebhookSent",
		"webhook request successful",
	)
	return nil
}

type webhookJob struct {
	webhook v1alpha1.WebhookNotification
	message v1alpha1.NotificationMessage
	Error   error
}

func webhookWorker(ctx context.Context, jobs chan webhookJob, res chan webhookJob) {
	for job := range jobs {
		if err := triggerWebhook(ctx, job.message, job.webhook); err != nil {
			job.Error = err
		}
		res <- job
	}
}

func handleWebhooks(ctx context.Context, message v1alpha1.NotificationMessage) error {
	l := log.WithFields(log.Fields{
		"pkg":              "notifications",
		"action":           "notifications.handleWebhooks",
		"notificationType": "webhooks",
		"syncConfig":       message.VaultSecretSync.ObjectMeta.Name,
		"syncNamespace":    message.VaultSecretSync.ObjectMeta.Namespace,
	})
	l.Trace("start")
	defer l.Trace("end")
	jobsToDo := []webhookJob{}
NotifLoop:
	for _, webhook := range message.VaultSecretSync.Spec.Notifications {
		if webhook.Webhook == nil {
			l.Debugf("skipping webhook notification: %v", webhook)
			continue NotifLoop
		}
		eventMatch := false
		for _, configuredEvent := range webhook.Webhook.Events {
			if configuredEvent == message.Event {
				eventMatch = true
				break
			}
		}

		// Skip this notification if event doesn't match
		if !eventMatch {
			l.Debugf("skipping email notification for non-matching event: %v", message.Event)
			continue NotifLoop
		}
		if webhook.Webhook != nil {
			l.Debugf("adding webhook notification: %v", webhook)
			jobsToDo = append(jobsToDo, webhookJob{
				webhook: *webhook.Webhook,
				message: message,
			})
		}
	}
	if len(jobsToDo) == 0 {
		l.Debug("no webhooks to trigger")
		return nil
	}
	workers := 100
	jobs := make(chan webhookJob, len(jobsToDo))
	res := make(chan webhookJob, len(jobsToDo))
	if len(jobsToDo) < workers {
		workers = len(jobsToDo)
	}
	for w := 1; w <= workers; w++ {
		go webhookWorker(ctx, jobs, res)
	}
	for _, job := range jobsToDo {
		jobs <- job
	}
	close(jobs)
	var errs []error
	for range jobsToDo {
		job := <-res
		if job.Error != nil {
			errs = append(errs, job.Error)
		}
	}
	if len(errs) > 0 {
		l.WithField("errors", errs).Error("failed to trigger webhooks")
		return fmt.Errorf("failed to trigger webhooks: %v", errs)
	} else {
		l.Info("all webhooks handled successfully")
	}
	return nil
}
