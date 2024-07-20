package notifications

import (
	"bytes"
	"testing"

	"github.com/robertlestak/vault-secret-sync/api/v1alpha1"
	aws "github.com/robertlestak/vault-secret-sync/stores/aws"
	vault "github.com/robertlestak/vault-secret-sync/stores/vault"
	"github.com/stretchr/testify/assert"
	"gopkg.in/gomail.v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestCreateEmail(t *testing.T) {
	exampleMessage := v1alpha1.NotificationMessage{
		Event:   v1alpha1.NotificationEventSyncSuccess,
		Message: "Sync completed successfully",
		VaultSecretSync: v1alpha1.VaultSecretSync{
			TypeMeta: metav1.TypeMeta{
				Kind:       "VaultSecretSync",
				APIVersion: "vaultsecretsync.lestak.sh/v1alpha1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "example-vaultsecretsync",
				Namespace: "default",
			},
			Spec: v1alpha1.VaultSecretSyncSpec{
				Source: &vault.VaultClient{
					Address: "http://vault.example.com",
					Path:    "secret/data",
				},
				Dest: []*v1alpha1.StoreConfig{
					{
						AWS: &aws.AwsClient{
							Region: "us-east-1",
							Name:   "secret/data",
						},
					},
				},
			},
			Status: v1alpha1.VaultSecretSyncStatus{
				Status: "success",
			},
		},
	}

	tests := []struct {
		name      string
		message   v1alpha1.NotificationMessage
		email     v1alpha1.EmailNotification
		wantErr   bool
		assertion func(*testing.T, *gomail.Message)
	}{
		{
			name:    "missing required To field",
			message: exampleMessage,
			email:   v1alpha1.EmailNotification{},
			wantErr: true,
		},
		{
			name:    "email not configured",
			message: exampleMessage,
			email: v1alpha1.EmailNotification{
				To: "recipient@example.com",
			},
			wantErr: false,
		},
		{
			name:    "custom values override config",
			message: exampleMessage,
			email: v1alpha1.EmailNotification{
				From:    "custom-from@example.com",
				To:      "custom-to@example.com",
				Subject: "Custom Subject",
				Body:    "Custom Body {{.VaultSecretSync.Name}} {{.Message}}",
			},
			wantErr: false,
			assertion: func(t *testing.T, m *gomail.Message) {
				assert.Equal(t, "custom-from@example.com", m.GetHeader("From")[0])
				assert.Equal(t, "custom-to@example.com", m.GetHeader("To")[0])
				assert.Equal(t, "Custom Subject", m.GetHeader("Subject")[0])

				var buf bytes.Buffer
				_, err := m.WriteTo(&buf)
				assert.NoError(t, err)
				assert.Contains(t, buf.String(), "Custom Body example-vaultsecretsync Sync completed successfully")
			},
		},
		{
			name:    "plain text body",
			message: exampleMessage,
			email: v1alpha1.EmailNotification{
				To:   "example",
				Body: "hello",
			},
			wantErr: false,
			assertion: func(t *testing.T, m *gomail.Message) {
				var buf bytes.Buffer
				_, err := m.WriteTo(&buf)
				assert.NoError(t, err)
				assert.Contains(t, buf.String(), "hello")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := createEmail(tt.message, tt.email)
			if (err != nil) != tt.wantErr {
				t.Errorf("createEmail() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.assertion != nil && got != nil {
				tt.assertion(t, got)
			}
		})
	}
}
