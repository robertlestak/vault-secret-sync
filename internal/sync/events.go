package sync

import (
	"context"
	"sync"
	"time"

	"github.com/robertlestak/vault-secret-sync/api/v1alpha1"
	"github.com/robertlestak/vault-secret-sync/internal/event"
	"github.com/robertlestak/vault-secret-sync/internal/queue"
	"github.com/robertlestak/vault-secret-sync/pkg/driver"
	log "github.com/sirupsen/logrus"
)

var (
	DefaultConfigs  map[driver.DriverName]*v1alpha1.StoreConfig
	ActiveSyncs     = make(map[string]time.Time)
	ActiveSyncMutex = sync.Mutex{}
)

// Worker function that processes events
func eventWorker(ctx context.Context, workerID int, events <-chan event.VaultEvent) {
	l := log.WithFields(log.Fields{
		"worker": workerID,
		"action": "eventWorker",
	})
	l.Trace("Worker started")
	for event := range events {
		// Process the event here
		if err := Sync(ctx, event); err != nil {
			l.Error(err)
		}
	}
	l.Trace("Worker stopped")
}

func EventProcessor(ctx context.Context, workerPoolSize, numSubscriptions int) error {
	l := log.WithFields(log.Fields{
		"action": "eventProcessor",
	})
	l.Trace("Starting eventProcessor")

	// Function to start a subscription and its workers
	startSubscription := func(subID int) {
		l := log.WithFields(log.Fields{
			"subscription": subID,
		})
		ch, err := queue.Q.Subscribe(ctx)
		if err != nil {
			l.Error("Failed to subscribe to queue:", err)
			return
		}
		l.Trace("Subscribed to queue")

		eventChannel := make(chan event.VaultEvent)

		// Start workers for this subscription
		for i := 0; i < workerPoolSize; i++ {
			go eventWorker(ctx, subID*workerPoolSize+i, eventChannel)
		}

		// Distribute events to workers
		go func() {
			for event := range ch {
				eventChannel <- event
			}
			close(eventChannel) // Close channel to stop workers after all events are processed
		}()
	}

	// Start multiple subscriptions
	for i := 0; i < numSubscriptions; i++ {
		startSubscription(i)
	}

	<-ctx.Done()
	l.Trace("Stopping eventProcessor")
	return nil
}

func Drain(ctx context.Context) {
	l := log.WithFields(log.Fields{
		"action": "Drain",
	})
	l.Trace("start")
	go l.Trace("end")
	// wait for all events to finish processing
	WaitForSyncs(ctx)
}

// TrackSyncStart logs the start time of a sync
func TrackSyncStart(id string) {
	startTime := time.Now()
	ActiveSyncMutex.Lock()
	ActiveSyncs[id] = startTime
	ActiveSyncMutex.Unlock()
}

// TrackSyncEnd logs the end time of a sync and removes it from ActiveSyncs
func TrackSyncEnd(id string) {
	ActiveSyncMutex.Lock()
	defer ActiveSyncMutex.Unlock()
	delete(ActiveSyncs, id)
}

// WaitForSyncs waits until all active syncs are complete
func WaitForSyncs(ctx context.Context) {
	l := log.WithFields(log.Fields{"action": "WaitForSyncs"})
	l.Trace("start")

	for {
		select {
		case <-ctx.Done():
			l.Trace("context cancelled")
			return
		default:
			ActiveSyncMutex.Lock()
			if len(ActiveSyncs) == 0 {
				ActiveSyncMutex.Unlock()
				l.Trace("syncs complete")
				return
			}
			l.WithFields(log.Fields{"syncs": len(ActiveSyncs)}).Trace("waiting for syncs")
			ActiveSyncMutex.Unlock()
			time.Sleep(1 * time.Second)
		}
	}
}

func SyncDelete(ctx context.Context, sc *SyncClients, j SyncJob) error {
	l := log.WithFields(log.Fields{"action": "SyncDelete"})
	l.Trace("start")
	defer l.Trace("end")

	sourcePath := sc.Source.GetPath()
	if j.VaultEvent.Manual && isRegexPath(sourcePath) {
		l.Debug("manual regex delete")
		if err := handleManualRegexDelete(ctx, sc, j); err != nil {
			return err
		}
		l.Debug("manual regex delete complete")
	} else if isRegexPath(sourcePath) {
		l.Debug("regex delete")
		if err := handleRegexDelete(ctx, sc, j); err != nil {
			return err
		}
	} else {
		l.Debug("single delete")
		if err := handleSingleDelete(sc, j); err != nil {
			return err
		}
	}
	l.Debug("delete complete")
	return nil
}

func SyncCreate(ctx context.Context, sc *SyncClients, j SyncJob) error {
	l := log.WithFields(log.Fields{"action": "SyncCreate"})
	l.Trace("start")

	sourcePath := sc.Source.GetPath()
	if j.VaultEvent.Manual && isRegexPath(sourcePath) {
		l.Debug("manual regex sync")
		if err := handleManualRegexSync(ctx, sc, j); err != nil {
			return err
		}
		l.Debug("manual regex sync complete")
	} else if isRegexPath(sourcePath) {
		if err := handleRegexSync(ctx, sc, j); err != nil {
			return err
		}
	} else {
		if err := handleSingleSync(sc, j); err != nil {
			return err
		}
	}
	l.Trace("end")
	return nil
}
