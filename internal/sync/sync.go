package sync

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/robertlestak/vault-secret-sync/api/v1alpha1"
	"github.com/robertlestak/vault-secret-sync/internal/event"
	"github.com/robertlestak/vault-secret-sync/internal/metrics"
	"github.com/robertlestak/vault-secret-sync/pkg/driver"
	log "github.com/sirupsen/logrus"
)

type StoresConfig map[driver.DriverName]*v1alpha1.StoreConfig

func Sync(ctx context.Context, evt event.VaultEvent) error {
	l := log.WithFields(log.Fields{"action": "Sync"})
	l.Trace("start")
	defer l.Trace("end")

	if evt.ID == "" {
		evt.ID = uuid.New().String()
	}
	if evt.Manual {
		evt.EventId = fmt.Sprintf("manual-%s", evt.ID)
	}
	l = l.WithFields(log.Fields{
		"id":      evt.ID,
		"eventId": evt.EventId,
		"path":    evt.Path,
		"op":      evt.Operation,
	})
	if evt.Address != "" {
		l = l.WithFields(log.Fields{"address": evt.Address})
	}
	if evt.Namespace != "" {
		l = l.WithFields(log.Fields{"namespace": evt.Namespace})
	}
	metrics.EventsProcessed.Inc()

	startTime := time.Now()
	defer func() {
		endTime := time.Now()
		metrics.EventProcessingDuration.Observe(endTime.Sub(startTime).Seconds())
	}()
	TrackSyncStart(evt.ID)
	defer TrackSyncEnd(evt.ID)

	jobHolder, affectedConfigs, destinationStores := buildSyncJobs(evt)
	if len(jobHolder) == 0 || len(affectedConfigs) == 0 {
		l.Trace("no configs need sync")
		return nil
	}

	l = l.WithFields(log.Fields{
		"affectedConfigs":   affectedConfigs,
		"destinationStores": len(destinationStores),
	})
	l.Info("syncing configs")

	if err := processSyncJobs(ctx, jobHolder); err != nil {
		l.WithError(err).Error("sync failed")
		return err
	}
	l.Info("sync complete")
	return nil
}

func syncJobWorker(ctx context.Context, jobHolder <-chan SyncJob, errChan chan error) {
	l := log.WithFields(log.Fields{"action": "syncJobWorker"})
	l.Trace("start")
	defer l.Trace("end")

	for job := range jobHolder {
		if err := doSync(ctx, job); err != nil {
			errChan <- err
		} else {
			errChan <- nil
		}
	}
}

func processSyncJobs(ctx context.Context, jobHolder []SyncJob) error {
	l := log.WithFields(log.Fields{"action": "processSyncJobs"})
	l.Trace("start")
	defer l.Trace("end")
	jobs := make(chan SyncJob, len(jobHolder))
	errChan := make(chan error, len(jobHolder))
	workers := 10
	if len(jobHolder) < workers {
		workers = len(jobHolder)
	}
	for i := 0; i < workers; i++ {
		go syncJobWorker(context.Background(), jobs, errChan)
	}
	for _, job := range jobHolder {
		jobs <- job
	}
	close(jobs)
	var errors []error
	for range jobHolder {
		if err := <-errChan; err != nil {
			errors = append(errors, err)
		}
	}
	if len(errors) > 0 {
		return fmt.Errorf("errors: %v", errors)
	}
	return nil
}

type SyncJob struct {
	VaultEvent event.VaultEvent
	SyncConfig v1alpha1.VaultSecretSync
	Error      error
}

func singleSyncWorker(ctx context.Context, sc *SyncClients, j SyncJob, dest chan SyncClient, errChan chan error) {
	l := log.WithFields(log.Fields{"action": "singleSyncWorker"})
	l.Trace("start")
	defer l.Trace("end")

	for d := range dest {
		if err := CreateOne(ctx, j, sc.Source, d, sc.Source.GetPath(), d.GetPath()); err != nil {
			errChan <- err
		} else {
			errChan <- nil
		}
	}
}

func handleSingleSync(sc *SyncClients, j SyncJob) error {
	l := log.WithFields(log.Fields{"action": "handleSingleSync"})
	l.Trace("single sync")
	var errors []error
	dest := make(chan SyncClient, len(sc.Dest))
	errChan := make(chan error, len(sc.Dest))
	workers := 10
	if len(sc.Dest) < workers {
		workers = len(sc.Dest)
	}
	for i := 0; i < workers; i++ {
		go singleSyncWorker(context.Background(), sc, j, dest, errChan)
	}
	for _, d := range sc.Dest {
		dest <- d
	}
	close(dest)
	for range sc.Dest {
		if err := <-errChan; err != nil {
			errors = append(errors, err)
		}
	}
	if len(errors) > 0 {
		return fmt.Errorf("errors: %v", errors)
	}
	return nil
}

func syncDeleteWorker(ctx context.Context, sc *SyncClients, j SyncJob, dest chan SyncClient, errChan chan error) {
	l := log.WithFields(log.Fields{"action": "syncDeleteWorker"})
	l.Trace("start")
	defer l.Trace("end")

	for d := range dest {
		if shouldFilterSecret(j, sc.Source.GetPath(), d.GetPath()) {
			errChan <- nil
			continue
		}
		if shouldDryRun(j, d, sc.Source.GetPath(), d.GetPath()) {
			errChan <- nil
			continue
		}
		if err := d.DeleteSecret(ctx, d.GetPath()); err != nil {
			errChan <- err
		} else {
			errChan <- nil
		}
	}
}

func handleSingleDelete(sc *SyncClients, j SyncJob) error {
	l := log.WithFields(log.Fields{"action": "handleSingleDelete"})
	l.Debug("single delete")
	var errors []error
	dest := make(chan SyncClient, len(sc.Dest))
	errChan := make(chan error, len(sc.Dest))
	workers := 10
	if len(sc.Dest) < workers {
		workers = len(sc.Dest)
	}
	for i := 0; i < workers; i++ {
		go syncDeleteWorker(context.Background(), sc, j, dest, errChan)
	}
	for _, d := range sc.Dest {
		dest <- d
	}
	close(dest)
	for range sc.Dest {
		if err := <-errChan; err != nil {
			errors = append(errors, err)
		}
	}
	if len(errors) > 0 {
		return fmt.Errorf("errors: %v", errors)
	}
	return nil
}
