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
		return fmt.Errorf("failed to create webhook request: %v", err)
	}
	if webhook.HeaderSecret != nil && *webhook.HeaderSecret != "" {
		sc, err := kubesecret.GetSecret(ctx, message.VaultSecretSync.Namespace, *webhook.HeaderSecret)
		if err != nil {
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
	error   error
}

func webhookWorker(ctx context.Context, jobs chan webhookJob, res chan webhookJob) {
	for job := range jobs {
		if err := triggerWebhook(ctx, job.message, job.webhook); err != nil {
			job.error = err
		}
		res <- job
	}
}

func handleWebhooks(ctx context.Context, message v1alpha1.NotificationMessage) error {
	jobsToDo := []webhookJob{}
	for _, webhook := range message.VaultSecretSync.Spec.Notifications {
		if webhook.Webhook == nil {
			continue
		}
		for _, o := range webhook.Email.Events {
			if o != message.Event {
				continue
			}
		}
		if webhook.Webhook != nil {
			jobsToDo = append(jobsToDo, webhookJob{
				webhook: *webhook.Webhook,
				message: message,
			})
		}
	}
	workers := 10
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
		if job.error != nil {
			errs = append(errs, job.error)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("failed to trigger webhooks: %v", errs)
	}
	return nil
}
