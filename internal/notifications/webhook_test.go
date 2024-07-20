package notifications

import (
	"reflect"
	"testing"

	"github.com/robertlestak/vault-secret-sync/api/v1alpha1"
	"github.com/robertlestak/vault-secret-sync/stores/aws"
	"github.com/robertlestak/vault-secret-sync/stores/vault"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestMessagePayload(t *testing.T) {
	tests := []struct {
		name    string
		message v1alpha1.NotificationMessage
		body    string
		want    string
	}{
		{
			name: "plain text body",
			message: v1alpha1.NotificationMessage{
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
				},
			},
			body: "hello",
			want: "hello",
		},
		{
			name: "templated message",
			message: v1alpha1.NotificationMessage{
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
				},
			},
			body: "status: {{.Event}}",
			want: "status: success",
		},
		{
			name: "deeply nested templated message",
			message: v1alpha1.NotificationMessage{
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
			},
			body: "status: {{.Event}}, message: {{.Message}}, sync status: {{.VaultSecretSync.Status.Status}}",
			want: "status: success, message: Sync completed successfully, sync status: success",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := messagePayload(tt.message, tt.body); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("messagePayload() = %v, want %v", got, tt.want)
			}
		})
	}
}
