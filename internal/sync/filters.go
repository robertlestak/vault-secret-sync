package sync

import (
	"context"
	"fmt"

	"github.com/robertlestak/vault-secret-sync/internal/backend"
	"github.com/robertlestak/vault-secret-sync/internal/transforms"
	log "github.com/sirupsen/logrus"
)

// shouldFilterSecret checks if the secret should be filtered based on configuration
func shouldFilterSecret(j SyncJob, sourcePath, destPath string) bool {
	l := log.WithFields(log.Fields{
		"action":     "shouldFilterSecret",
		"sourcePath": sourcePath,
		"destPath":   destPath,
	})
	if j.SyncConfig.Spec.Filters != nil && transforms.ShouldFilterString(j.SyncConfig, sourcePath) {
		l.Debug("filtering secret")
		return true
	}
	return false
}

// shouldDryRun checks if the sync should be a dry run
func shouldDryRun(j SyncJob, dest SyncClient, sourcePath, destPath string) bool {
	l := log.WithFields(log.Fields{
		"action":     "shouldDryRun",
		"sourcePath": sourcePath,
		"destPath":   destPath,
	})
	if j.SyncConfig.Spec.DryRun != nil && *j.SyncConfig.Spec.DryRun {
		l.Info("dry run")
		backend.SetSyncStatus(context.TODO(), j.SyncConfig, backend.SyncStatusDryRun)
		backend.WriteEvent(
			context.TODO(),
			j.SyncConfig.Namespace,
			j.SyncConfig.Name,
			"Normal",
			string(backend.SyncStatusDryRun),
			fmt.Sprintf("dry run: synced %s to %s: %s", sourcePath, dest.Driver(), destPath),
		)
		return true
	}
	return false
}
