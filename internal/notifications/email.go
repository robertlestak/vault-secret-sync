package notifications

import (
	"context"
	"crypto/tls"
	"fmt"

	"github.com/robertlestak/vault-secret-sync/api/v1alpha1"
	"github.com/robertlestak/vault-secret-sync/internal/backend"
	"github.com/robertlestak/vault-secret-sync/internal/config"
	log "github.com/sirupsen/logrus"
	"gopkg.in/gomail.v2"
)

func createEmail(message v1alpha1.NotificationMessage, email v1alpha1.EmailNotification) (*gomail.Message, error) {
	l := log.WithFields(log.Fields{"action": "createEmail"})
	l.Debugf("creating email notification: %v", email)
	if email.To == "" {
		return nil, fmt.Errorf("email notification is missing required")
	}
	if config.Config.Notifications == nil {
		config.Config.Notifications = &config.NotificationsConfig{}
	}
	if config.Config.Notifications.Email == nil {
		config.Config.Notifications.Email = &config.EmailNotificationConfig{}
	}
	if email.From == "" && config.Config.Notifications.Email.From != "" {
		email.From = config.Config.Notifications.Email.From
	}
	if email.To == "" && config.Config.Notifications.Email.To != "" {
		email.To = config.Config.Notifications.Email.To
	}
	if email.Subject == "" && config.Config.Notifications.Email.Subject != "" {
		email.Subject = config.Config.Notifications.Email.Subject
	}
	if email.Body == "" && config.Config.Notifications.Email.Body != "" {
		email.Body = config.Config.Notifications.Email.Body
	}
	if email.Subject == "" {
		email.Subject = "Vault Secret Sync Notification"
	}
	if email.From == "" {
		email.From = "no-reply@vault-secret-sync"
	}
	l.Debugf("sending filled email notification: %v", email)
	sub, err := renderTemplate(email.Subject, message)
	if err != nil {
		l.Errorf("failed to render email subject: %v", err)
		return nil, err
	}
	mp := messagePayload(message, email.Body)
	l.Debugf("email message payload: %v", mp)
	m := gomail.NewMessage()
	m.SetHeader("From", email.From)
	m.SetHeader("To", email.To)
	m.SetHeader("Subject", sub)
	m.SetBody("text/html", mp)
	l.Debugf("email notification created: %+v", m)
	return m, nil
}

func sendEmailNotification(ctx context.Context, message v1alpha1.NotificationMessage, email v1alpha1.EmailNotification) error {
	l := log.WithFields(log.Fields{"action": "sendEmailNotification"})
	l.Debugf("sending email notification: %v", email)
	m, err := createEmail(message, email)
	if err != nil {
		backend.WriteEvent(
			ctx,
			message.VaultSecretSync.Namespace,
			message.VaultSecretSync.Name,
			"Warning",
			string(backend.SyncStatusFailed),
			fmt.Sprintf("failed to create email notification: %v", err),
		)
		return err
	}
	if email.Host == "" && config.Config.Notifications.Email.Host != "" {
		email.Host = config.Config.Notifications.Email.Host
	}
	if email.Port == 0 && config.Config.Notifications.Email.Port != 0 {
		email.Port = config.Config.Notifications.Email.Port
	}
	if email.Username == "" && config.Config.Notifications.Email.Username != "" {
		email.Username = config.Config.Notifications.Email.Username
	}
	if email.Password == "" && config.Config.Notifications.Email.Password != "" {
		email.Password = config.Config.Notifications.Email.Password
	}
	if !email.InsecureSkipVerify && config.Config.Notifications.Email.InsecureSkipVerify {
		email.InsecureSkipVerify = config.Config.Notifications.Email.InsecureSkipVerify
	}
	if email.Password == "" {
		email.Username = ""
	}
	d := gomail.NewDialer(
		email.Host,
		email.Port,
		email.Username,
		email.Password,
	)
	if email.InsecureSkipVerify {
		d.TLSConfig = &tls.Config{InsecureSkipVerify: true}
	}
	if err := d.DialAndSend(m); err != nil {
		backend.WriteEvent(
			ctx,
			message.VaultSecretSync.Namespace,
			message.VaultSecretSync.Name,
			"Warning",
			string(backend.SyncStatusFailed),
			fmt.Sprintf("failed to send email notification: %v", err),
		)
		return err
	}
	backend.WriteEvent(
		ctx,
		message.VaultSecretSync.Namespace,
		message.VaultSecretSync.Name,
		"Normal",
		"EmailSent",
		"Email notification sent successfully",
	)
	return nil
}

type emailJob struct {
	email   v1alpha1.EmailNotification
	message v1alpha1.NotificationMessage
	error   error
}

func emailWorker(ctx context.Context, jobs chan emailJob, res chan emailJob) {
	for job := range jobs {
		if err := sendEmailNotification(ctx, job.message, job.email); err != nil {
			job.error = err
		}
		res <- job
	}
}

func handleEmail(ctx context.Context, message v1alpha1.NotificationMessage) error {
	jobsToDo := []emailJob{}
	for _, email := range message.VaultSecretSync.Spec.Notifications {
		if email.Email == nil {
			continue
		}
		for _, o := range email.Email.Events {
			if o != message.Event {
				continue
			}
		}
		jobsToDo = append(jobsToDo, emailJob{
			email:   *email.Email,
			message: message,
		})
	}
	workers := 10
	jobs := make(chan emailJob, len(jobsToDo))
	res := make(chan emailJob, len(jobsToDo))
	if len(jobsToDo) < workers {
		workers = len(jobsToDo)
	}
	for w := 1; w <= workers; w++ {
		go emailWorker(ctx, jobs, res)
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
		return fmt.Errorf("failed to trigger emails: %v", errs)
	}
	return nil
}
