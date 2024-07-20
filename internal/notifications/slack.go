package notifications

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/robertlestak/vault-secret-sync/api/v1alpha1"
	"github.com/robertlestak/vault-secret-sync/internal/backend"
	"github.com/robertlestak/vault-secret-sync/internal/config"
)

func sendSlackNotification(ctx context.Context, message v1alpha1.NotificationMessage, slack v1alpha1.SlackNotification) error {
	if slack.Body == "" && config.Config.Notifications.Slack.Message != "" {
		slack.Body = config.Config.Notifications.Slack.Message
	}
	payload := map[string]string{"text": messagePayload(message, slack.Body)}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		backend.WriteEvent(
			ctx,
			message.VaultSecretSync.Namespace,
			message.VaultSecretSync.Name,
			"Warning",
			string(backend.SyncStatusFailed),
			fmt.Sprintf("failed to marshal Slack notification payload: %v", err),
		)
		return err
	}
	if slack.URL == "" && config.Config.Notifications.Slack.URL != "" {
		slack.URL = config.Config.Notifications.Slack.URL
	}
	resp, err := http.Post(slack.URL, "application/json", bytes.NewBuffer(payloadBytes))
	if err != nil {
		backend.WriteEvent(
			ctx,
			message.VaultSecretSync.Namespace,
			message.VaultSecretSync.Name,
			"Warning",
			string(backend.SyncStatusFailed),
			fmt.Sprintf("failed to send Slack notification: %v", err),
		)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		backend.WriteEvent(
			ctx,
			message.VaultSecretSync.Namespace,
			message.VaultSecretSync.Name,
			"Warning",
			string(backend.SyncStatusFailed),
			fmt.Sprintf("failed to send Slack notification, status code: %d", resp.StatusCode),
		)
		return fmt.Errorf("failed to send Slack notification, status code: %d", resp.StatusCode)
	}
	backend.WriteEvent(
		ctx,
		message.VaultSecretSync.Namespace,
		message.VaultSecretSync.Name,
		"Normal",
		"SlackNotificationSent",
		"Slack notification sent successfully",
	)
	return nil
}

type slackJob struct {
	slack   v1alpha1.SlackNotification
	message v1alpha1.NotificationMessage
	error   error
}

func slackWorker(ctx context.Context, jobs chan slackJob, res chan slackJob) {
	for job := range jobs {
		if err := sendSlackNotification(ctx, job.message, job.slack); err != nil {
			job.error = err
		}
		res <- job
	}
}

func handleSlack(ctx context.Context, message v1alpha1.NotificationMessage) error {
	jobsToDo := []slackJob{}
	for _, slack := range message.VaultSecretSync.Spec.Notifications {
		if slack.Slack == nil {
			continue
		}
		for _, o := range slack.Slack.Events {
			if o != message.Event {
				continue
			}
		}
		jobsToDo = append(jobsToDo, slackJob{
			slack:   *slack.Slack,
			message: message,
		})
	}
	workers := 10
	jobs := make(chan slackJob, len(jobsToDo))
	res := make(chan slackJob, len(jobsToDo))
	if len(jobsToDo) < workers {
		workers = len(jobsToDo)
	}
	for w := 1; w <= workers; w++ {
		go slackWorker(ctx, jobs, res)
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
		return fmt.Errorf("failed to trigger slacks: %v", errs)
	}
	return nil
}
