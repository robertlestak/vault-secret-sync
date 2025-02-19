package notifications

import (
	"context"
	"testing"

	"github.com/robertlestak/vault-secret-sync/api/v1alpha1"
	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestHandleNotificationTemplate(t *testing.T) {
	// Create a fake Kubernetes client with a predefined ConfigMap
	client := fake.NewSimpleClientset(&v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-configmap",
			Namespace: "test-namespace",
		},
		Data: map[string]string{
			"test-key.yaml": `
- type: slack
  slack:
    url: "https://hooks.slack.com/services/T00000000/B00000000/XXXXXXXXXXXXXXXXXXXXXXXX"
    events:
      - "success"
      - "failure"
`,
		},
	})

	message := v1alpha1.NotificationMessage{
		VaultSecretSync: v1alpha1.VaultSecretSync{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-sync",
				Namespace: "test-namespace",
			},
			Spec: v1alpha1.VaultSecretSyncSpec{
				NotificationsTemplate: func(s string) *string { return &s }("test-configmap/test-key.yaml"),
			},
		},
	}

	err := handleNotificationTemplate(context.Background(), client, &message)
	assert.NoError(t, err)
	assert.NotNil(t, message.VaultSecretSync.Spec.Notifications)
	assert.Len(t, message.VaultSecretSync.Spec.Notifications, 1)
	assert.NotNil(t, message.VaultSecretSync.Spec.Notifications[0].Slack)
	assert.Equal(t, "https://hooks.slack.com/services/T00000000/B00000000/XXXXXXXXXXXXXXXXXXXXXXXX", *message.VaultSecretSync.Spec.Notifications[0].Slack.URL)

	expectedEvents := []string{"success", "failure"}
	actualEvents := make([]string, len(message.VaultSecretSync.Spec.Notifications[0].Slack.Events))
	for i, event := range message.VaultSecretSync.Spec.Notifications[0].Slack.Events {
		actualEvents[i] = string(event)
	}
	assert.ElementsMatch(t, expectedEvents, actualEvents)
}
