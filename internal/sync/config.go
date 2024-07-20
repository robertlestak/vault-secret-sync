package sync

import (
	"context"
	"sync"

	log "github.com/sirupsen/logrus"
)

// SyncConfig is a single sync configuration containing the source and destination

type SyncClients struct {
	Source SyncClient
	Dest   []SyncClient
}

// CreateClients will create a new Vault client for both the Source and Destination
// vaults from the SyncConfig. NOTE: This creates a unique client for each SyncConfig, on each sync execution.
// At face value this would seem inefficient and a prime opportunity to refactor by caching
// the clients. However we _intentionally_ instantiate a new client for each sync execution
// to ensure that the client is fresh and not reusing any state from previous syncs.
// This also enables us to scope down the ttl of the client to the duration of the sync.
func (sc *SyncClients) CreateClients(ctx context.Context) error {
	l := log.WithFields(log.Fields{
		"action": "sc.createClients",
	})
	l.Trace("start")
	cerr := sc.Source.Init(ctx)
	if cerr != nil {
		l.Error(cerr)
		return cerr
	}
	l.Trace("create client")
	for _, d := range sc.Dest {
		if cerr := d.Init(ctx); cerr != nil {
			l.Error(cerr)
			return cerr
		}
	}
	l.Trace("end")
	return nil
}

func (sc *SyncClients) CloseClients(ctx context.Context) {
	l := log.WithFields(log.Fields{
		"action": "sc.CloseClients",
	})
	l.Trace("start")
	l.Trace("close source client")
	wg := sync.WaitGroup{}
	syncJobs := 1 + len(sc.Dest)
	wg.Add(syncJobs)
	go func() {
		sc.Source.Close()
		wg.Done()
	}()
	go func() {
		for _, d := range sc.Dest {
			l.Debugf("close dest client: %s", d.Driver())
			d.Close()
			wg.Done()
		}
	}()
	wg.Wait()
	l.Trace("end")
}
