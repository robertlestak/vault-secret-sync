package queue

import (
	"context"
	"errors"
	gosync "sync"
	"time"

	"github.com/robertlestak/vault-secret-sync/internal/event"
	log "github.com/sirupsen/logrus"
)

type MemoryQueue struct {
	seenEvents  map[string]time.Time
	newEvents   chan event.VaultEvent
	eventsMutex gosync.Mutex
}

func NewMemoryQueue() *MemoryQueue {
	return &MemoryQueue{
		seenEvents: make(map[string]time.Time),
		newEvents:  make(chan event.VaultEvent),
	}
}

func (q *MemoryQueue) Push(evt event.VaultEvent) error {
	select {
	case q.newEvents <- evt:
		return nil
	default:
		return errors.New("failed to push event to local channel")
	}
}

func (q *MemoryQueue) Start(params map[string]any) error {
	go q.eventClearer()
	return nil
}

func (q *MemoryQueue) Stop() error {
	return nil
}

func (q *MemoryQueue) Publish(ctx context.Context, item event.VaultEvent) error {
	l := log.WithFields(log.Fields{
		"action": "Publish",
		"driver": "memory",
	})
	l.Trace("start")
	q.newEvents <- item
	return nil
}

func (q *MemoryQueue) Subscribe(ctx context.Context) (chan event.VaultEvent, error) {
	l := log.WithFields(log.Fields{
		"action": "Subscribe",
		"driver": "memory",
	})
	l.Trace("start")
	ch := make(chan event.VaultEvent)
	go func() {
		for v := range q.newEvents {
			ch <- v
		}
	}()
	return ch, nil
}

func (q *MemoryQueue) eventClearer() {
	l := log.WithFields(log.Fields{
		"action": "eventClearer",
		"driver": "memory",
	})
	l.Trace("start")
	expireTime := 5 * time.Minute
	for {
		time.Sleep(1 * time.Minute)
		q.eventsMutex.Lock()
		for k, v := range q.seenEvents {
			if time.Since(v) > expireTime {
				delete(q.seenEvents, k)
			}
		}
		q.eventsMutex.Unlock()
	}
}
func (q *MemoryQueue) SeenEvent(id string) {
	l := log.WithFields(log.Fields{
		"action": "logEventSeen",
		"driver": "memory",
	})
	l.Trace("start")
	q.eventsMutex.Lock()
	q.seenEvents[id] = time.Now()
	q.eventsMutex.Unlock()
	l.Trace("end")
}

func (q *MemoryQueue) EventSeen(id string) bool {
	l := log.WithFields(log.Fields{
		"action": "eventSeen",
		"driver": "memory",
	})
	l.Trace("start")
	if !Dedupe {
		return false
	}
	q.eventsMutex.Lock()
	defer q.eventsMutex.Unlock()
	if _, ok := q.seenEvents[id]; ok {
		l.Trace("event seen")
		return true
	}
	l.Trace("event not seen")
	return false
}

func (q *MemoryQueue) Ping() error {
	return nil
}
