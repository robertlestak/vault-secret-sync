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
	"github.com/robertlestak/vault-secret-sync/pkg/kubesecret"
	log "github.com/sirupsen/logrus"
)

func sendSlackNotification(ctx context.Context, message v1alpha1.NotificationMessage, slack v1alpha1.SlackNotification) error {
	l := log.WithFields(log.Fields{"action": "sendSlackNotification"})
	if (slack.URL == nil || *slack.URL == "") && slack.URLSecret != nil && *slack.URLSecret != "" {
		sc, err := kubesecret.GetSecret(ctx, message.VaultSecretSync.Namespace, *slack.URLSecret)
		if err != nil {
			l.WithError(err).Error("failed to get secret for Slack URL")
			return err
		}
		sk := "url"
		if slack.URLSecretKey != nil && *slack.URLSecretKey != "" {
			sk = *slack.URLSecretKey
		}
		if v, ok := sc[sk]; ok {
			vs := string(v)
			slack.URL = &vs
		} else {
			err := fmt.Errorf("secret %s does not contain key %s", *slack.URLSecret, sk)
			l.WithError(err).Error("failed to get Slack URL from secret")
			return err
		}
	}
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
		l.WithError(err).Error("failed to marshal Slack notification payload")
		return err
	}
	if (slack.URL == nil || *slack.URL == "") && config.Config.Notifications.Slack.URL != "" {
		slack.URL = &config.Config.Notifications.Slack.URL
	}
	resp, err := http.Post(*slack.URL, "application/json", bytes.NewBuffer(payloadBytes))
	if err != nil {
		backend.WriteEvent(
			ctx,
			message.VaultSecretSync.Namespace,
			message.VaultSecretSync.Name,
			"Warning",
			string(backend.SyncStatusFailed),
			fmt.Sprintf("failed to send Slack notification: %v", err),
		)
		l.WithError(err).Error("failed to send Slack notification")
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
		err := fmt.Errorf("failed to send Slack notification, status code: %d", resp.StatusCode)
		l.WithError(err).Error("failed to send Slack notification")
		return err
	}
	backend.WriteEvent(
		ctx,
		message.VaultSecretSync.Namespace,
		message.VaultSecretSync.Name,
		"Normal",
		"SlackNotificationSent",
		"Slack notification sent successfully",
	)
	l.Info("Slack notification sent successfully")
	return nil
}

type slackJob struct {
	slack   v1alpha1.SlackNotification
	message v1alpha1.NotificationMessage
	Error   error
}

func slackWorker(ctx context.Context, jobs chan slackJob, res chan slackJob) {
	for job := range jobs {
		if err := sendSlackNotification(ctx, job.message, job.slack); err != nil {
			job.Error = err
		}
		res <- job
	}
}

func handleSlack(ctx context.Context, message v1alpha1.NotificationMessage) error {
	l := log.WithFields(log.Fields{
		"pkg":              "notifications",
		"action":           "notifications.handleSlack",
		"notificationType": "slack",
		"syncConfig":       message.VaultSecretSync.ObjectMeta.Name,
		"syncNamespace":    message.VaultSecretSync.ObjectMeta.Namespace,
	})
	l.Trace("start")
	defer l.Trace("end")
	jobsToDo := []slackJob{}
NotifLoop:
	for _, slack := range message.VaultSecretSync.Spec.Notifications {
		if slack.Slack == nil {
			l.Debugf("skipping Slack notification: %v", slack)
			continue NotifLoop
		}
	EventLoop:
		for _, o := range slack.Slack.Events {
			if o != message.Event {
				l.Debugf("skipping Slack notification: %v", slack)
				continue EventLoop
			}
		}
		l.Debugf("adding Slack notification: %v", slack)
		jobsToDo = append(jobsToDo, slackJob{
			slack:   *slack.Slack,
			message: message,
		})
	}
	if len(jobsToDo) == 0 {
		l.Debug("no webhooks to trigger")
		return nil
	}
	workers := 100
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
		if job.Error != nil {
			errs = append(errs, job.Error)
		}
	}
	if len(errs) > 0 {
		l.WithField("errors", errs).Error("failed to trigger Slack notifications")
		return fmt.Errorf("failed to trigger Slack notifications: %v", errs)
	} else {
		l.Info("all Slack notifications handled successfully")
	}
	return nil
}
