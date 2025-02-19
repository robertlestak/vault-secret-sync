// +k8s:deepcopy-gen=package
// +groupName=vaultsecretsync.lestak.sh
package v1alpha1

import (
	"github.com/robertlestak/vault-secret-sync/stores/aws"
	"github.com/robertlestak/vault-secret-sync/stores/gcp"
	"github.com/robertlestak/vault-secret-sync/stores/github"
	"github.com/robertlestak/vault-secret-sync/stores/httpstore"
	"github.com/robertlestak/vault-secret-sync/stores/vault"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:resource:path=vaultsecretsyncs,scope=Namespaced,shortName=vss
// +kubebuilder:printcolumn:name="Status",type=string,JSONPath=`.status.status`,description="Current status of the VaultSecretSync"
// +kubebuilder:printcolumn:name="SyncDestinations",type=integer,JSONPath=`.status.syncDestinations`,description="Number of destinations synced"

// VaultSecretSync is the Schema for the vaultsecretsyncs API
type VaultSecretSync struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VaultSecretSyncSpec   `json:"spec,omitempty"`
	Status VaultSecretSyncStatus `json:"status,omitempty"`
}

type NotificationEvent string

const (
	NotificationEventSyncSuccess NotificationEvent = "success"
	NotificationEventSyncFailure NotificationEvent = "failure"
)

type StoreConfig struct {
	AWS    *aws.AwsClient        `json:"aws,omitempty" yaml:"aws,omitempty"`
	GCP    *gcp.GcpClient        `json:"gcp,omitempty" yaml:"gcp,omitempty"`
	GitHub *github.GitHubClient  `json:"github,omitempty" yaml:"github,omitempty"`
	Vault  *vault.VaultClient    `json:"vault,omitempty" yaml:"vault,omitempty"`
	HTTP   *httpstore.HTTPClient `json:"http,omitempty" yaml:"http,omitempty"`
}

type RegexpFilterConfig struct {
	Include []string `json:"include,omitempty" yaml:"include,omitempty"`
	Exclude []string `json:"exclude,omitempty" yaml:"exclude,omitempty"`
}

type PathFilterConfig struct {
	Include []string `json:"include,omitempty" yaml:"include,omitempty"`
	Exclude []string `json:"exclude,omitempty" yaml:"exclude,omitempty"`
}

type FilterConfig struct {
	Regex *RegexpFilterConfig `json:"regex,omitempty" yaml:"regex,omitempty"`
	Path  *PathFilterConfig   `json:"path,omitempty" yaml:"path,omitempty"`
}

type RenameTransform struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type TransformSpec struct {
	Include  []string          `yaml:"include,omitempty" json:"include,omitempty"`
	Exclude  []string          `yaml:"exclude,omitempty" json:"exclude,omitempty"`
	Rename   []RenameTransform `json:"rename,omitempty"`
	Template *string           `json:"template,omitempty"`
}

// Webhook represents the configuration for a webhook.
type WebhookNotification struct {
	Events       []NotificationEvent `json:"events"`
	URL          string              `yaml:"url,omitempty" json:"url,omitempty"`
	Method       string              `yaml:"method,omitempty" json:"method,omitempty"`
	Headers      map[string]string   `yaml:"headers,omitempty" json:"headers,omitempty"`
	HeaderSecret *string             `yaml:"headerSecret,omitempty" json:"headerSecret,omitempty"`
	Body         string              `yaml:"body,omitempty" json:"body,omitempty"`
	ExcludeBody  bool                `yaml:"excludeBody,omitempty" json:"excludeBody,omitempty"`
}

type EmailNotification struct {
	Events  []NotificationEvent `json:"events"`
	To      string              `yaml:"to,omitempty" json:"to,omitempty"`
	From    string              `yaml:"from,omitempty" json:"from,omitempty"`
	Subject string              `yaml:"subject,omitempty" json:"subject,omitempty"`
	Body    string              `yaml:"body,omitempty" json:"body,omitempty"`

	Host               string `yaml:"host,omitempty" json:"host,omitempty"`
	Port               int    `yaml:"port,omitempty" json:"port,omitempty"`
	Username           string `yaml:"username,omitempty" json:"username,omitempty"`
	Password           string `yaml:"password,omitempty" json:"password,omitempty"`
	InsecureSkipVerify bool   `yaml:"insecureSkipVerify,omitempty" json:"insecureSkipVerify,omitempty"`
}

type SlackNotification struct {
	Events       []NotificationEvent `json:"events"`
	URL          *string             `yaml:"url,omitempty" json:"url,omitempty"`
	URLSecret    *string             `yaml:"urlSecret,omitempty" json:"urlSecret,omitempty"`
	URLSecretKey *string             `yaml:"urlSecretKey,omitempty" json:"urlSecretKey,omitempty"`
	Body         string              `yaml:"body,omitempty" json:"body,omitempty"`
}

type NotificationMessage struct {
	Event           NotificationEvent `json:"event"`
	Message         string            `json:"message"`
	VaultSecretSync VaultSecretSync   `json:"vaultSecretSync"`
}

type NotificationSpec struct {
	Webhook *WebhookNotification `json:"webhook,omitempty"`
	Email   *EmailNotification   `json:"email,omitempty"`
	Slack   *SlackNotification   `json:"slack,omitempty"`
}

// +kubebuilder:object:generate=true

// VaultSecretSyncSpec defines the desired state of VaultSecretSync
type VaultSecretSyncSpec struct {
	Source                *vault.VaultClient  `yaml:"source" json:"source"`
	Dest                  []*StoreConfig      `yaml:"dest" json:"dest"`
	SyncDelete            *bool               `yaml:"syncDelete,omitempty" json:"syncDelete,omitempty"`
	DryRun                *bool               `yaml:"dryRun,omitempty" json:"dryRun,omitempty"`
	Suspend               *bool               `yaml:"suspend,omitempty" json:"suspend,omitempty"`
	Filters               *FilterConfig       `yaml:"filters,omitempty" json:"filters,omitempty"`
	Transforms            *TransformSpec      `json:"transforms,omitempty"`
	Notifications         []*NotificationSpec `json:"notifications,omitempty"`
	NotificationsTemplate *string             `json:"notificationsTemplate,omitempty"`
}

// +kubebuilder:object:generate=true

// VaultSecretSyncStatus defines the observed state of VaultSecretSync
type VaultSecretSyncStatus struct {
	Status           string      `json:"status,omitempty"`
	LastSyncTime     metav1.Time `json:"lastSyncTime,omitempty"`
	SyncDestinations int         `json:"syncDestinations,omitempty"`
	Hash             string      `json:"hash,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true

// VaultSecretSyncList contains a list of VaultSecretSync
type VaultSecretSyncList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VaultSecretSync `json:"items"`
}
