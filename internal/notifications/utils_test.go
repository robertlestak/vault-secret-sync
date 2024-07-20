package notifications

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/robertlestak/vault-secret-sync/api/v1alpha1"
	aws "github.com/robertlestak/vault-secret-sync/stores/aws"
	vault "github.com/robertlestak/vault-secret-sync/stores/vault"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Tests for renderTemplate function
func TestRenderTemplate_Success(t *testing.T) {
	vaultClient := &vault.VaultClient{
		Address: "http://vault.example.com",
		Path:    "secret/data",
	}

	vaultSecretSync := v1alpha1.VaultSecretSync{
		TypeMeta: metav1.TypeMeta{
			Kind:       "VaultSecretSync",
			APIVersion: "vaultsecretsync.lestak.sh/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "example-vaultsecretsync",
			Namespace: "default",
		},
		Spec: v1alpha1.VaultSecretSyncSpec{
			Source: vaultClient,
			Dest: []*v1alpha1.StoreConfig{
				{
					AWS: &aws.AwsClient{
						Region: "us-east-1",
						Name:   "secret/data",
					},
				},
			},
			SyncDelete: new(bool),
			DryRun:     new(bool),
		},
		Status: v1alpha1.VaultSecretSyncStatus{
			Status: "success",
		},
	}

	notificationMessage := v1alpha1.NotificationMessage{
		Event:           v1alpha1.NotificationEventSyncSuccess,
		Message:         "Sync completed successfully",
		VaultSecretSync: vaultSecretSync,
	}

	templateString := `
Event: {{.Event}}
Message: {{.Message}}
VaultSecretSync:
  Name: {{.VaultSecretSync.ObjectMeta.Name}}
  Namespace: {{.VaultSecretSync.ObjectMeta.Namespace}}
  Source Address: {{.VaultSecretSync.Spec.Source.Address}}
  Destination: {{range .VaultSecretSync.Spec.Dest}}{{.AWS.Name}} ({{.AWS.Region}}){{end}}
  Status: {{.VaultSecretSync.Status.Status}}
`

	expectedOutput := `
Event: success
Message: Sync completed successfully
VaultSecretSync:
  Name: example-vaultsecretsync
  Namespace: default
  Source Address: http://vault.example.com
  Destination: secret/data (us-east-1)
  Status: success
`

	output, err := renderTemplate(templateString, notificationMessage)
	assert.NoError(t, err)
	assert.Equal(t, expectedOutput, output)
}

func TestRenderTemplate_WithJSON(t *testing.T) {
	vaultClient := &vault.VaultClient{
		Address: "http://vault.example.com",
		Path:    "secret/data",
	}

	vaultSecretSync := v1alpha1.VaultSecretSync{
		TypeMeta: metav1.TypeMeta{
			Kind:       "VaultSecretSync",
			APIVersion: "vaultsecretsync.lestak.sh/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "example-vaultsecretsync",
			Namespace: "default",
		},
		Spec: v1alpha1.VaultSecretSyncSpec{
			Source: vaultClient,
			Dest: []*v1alpha1.StoreConfig{
				{
					AWS: &aws.AwsClient{
						Region: "us-east-1",
						Name:   "secret/data",
					},
				},
			},
			SyncDelete: new(bool),
			DryRun:     new(bool),
		},
		Status: v1alpha1.VaultSecretSyncStatus{
			Status: "success",
		},
	}

	notificationMessage := v1alpha1.NotificationMessage{
		Event:           v1alpha1.NotificationEventSyncSuccess,
		Message:         "Sync completed successfully",
		VaultSecretSync: vaultSecretSync,
	}

	templateString := `
Event: {{.Event}}
VaultSecretSync JSON: {{json .VaultSecretSync}}
`

	vaultSecretSyncJSON, _ := json.Marshal(notificationMessage.VaultSecretSync)
	expectedOutput := fmt.Sprintf(`
Event: success
VaultSecretSync JSON: %s
`, vaultSecretSyncJSON)

	output, err := renderTemplate(templateString, notificationMessage)
	assert.NoError(t, err)
	assert.Equal(t, expectedOutput, output)
}

func TestRenderTemplate_EmptyValues(t *testing.T) {
	vaultSecretSync := v1alpha1.VaultSecretSync{
		TypeMeta: metav1.TypeMeta{
			Kind:       "VaultSecretSync",
			APIVersion: "vaultsecretsync.lestak.sh/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "",
			Namespace: "",
		},
		Spec: v1alpha1.VaultSecretSyncSpec{
			Source: nil,
			Dest:   nil,
		},
		Status: v1alpha1.VaultSecretSyncStatus{
			Status: "",
		},
	}

	notificationMessage := v1alpha1.NotificationMessage{
		Event:           v1alpha1.NotificationEventSyncFailure,
		Message:         "Sync failed",
		VaultSecretSync: vaultSecretSync,
	}

	templateString := `
Event: {{.Event}}
Message: {{.Message}}
VaultSecretSync:
  Name: {{.VaultSecretSync.ObjectMeta.Name}}
  Namespace: {{.VaultSecretSync.ObjectMeta.Namespace}}
  Source Address: {{if .VaultSecretSync.Spec.Source}}{{.VaultSecretSync.Spec.Source.Address}}{{else}}<no value>{{end}}
  Destination: {{if .VaultSecretSync.Spec.Dest}}{{range .VaultSecretSync.Spec.Dest}}{{.AWS.Name}} ({{.AWS.Region}}){{end}}{{else}}<no value>{{end}}
  Status: {{.VaultSecretSync.Status.Status}}
`

	expectedOutput := `
Event: failure
Message: Sync failed
VaultSecretSync:
  Name: 
  Namespace: 
  Source Address: <no value>
  Destination: <no value>
  Status: 
`

	output, err := renderTemplate(templateString, notificationMessage)
	assert.NoError(t, err)
	assert.Equal(t, expectedOutput, output)
}
