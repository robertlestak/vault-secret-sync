package event

import (
	"github.com/hashicorp/vault/audit"
	"github.com/hashicorp/vault/sdk/logical"
)

type VaultEvent struct {
	ID        string            `json:"id"`
	EventId   string            `json:"eventId"`
	SyncName  string            `json:"syncName"`
	Address   string            `json:"address"`
	Namespace string            `json:"namespace"`
	Path      string            `json:"path"`
	Operation logical.Operation `json:"operation"`
	Manual    bool              `json:"manual"`
}

// AuditEvent contains a single AuditEvent as received by the operator
// the operator receives the event as-is, and supplements this with contextual http information
type AuditEvent struct {
	Event       audit.ResponseEntry
	VaultTenant string
	RemoteAddr  string
}
