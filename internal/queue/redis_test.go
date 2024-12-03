package queue

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/hashicorp/vault/sdk/logical"
	"github.com/robertlestak/vault-secret-sync/internal/event"
)

func setupTestRedis(t *testing.T) (*miniredis.Miniredis, *RedisQueue) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("Failed to create miniredis: %v", err)
	}

	q := NewRedisQueue()
	intPort, err := strconv.Atoi(mr.Port())
	if err != nil {
		t.Fatalf("Failed to convert port to int: %v", err)
	}

	err = q.Start(map[string]any{
		"host":     mr.Host(),
		"port":     intPort,
		"database": 0,
	})
	if err != nil {
		t.Fatalf("Failed to start Redis queue: %v", err)
	}

	return mr, q
}

func TestRedisQueue_SimplePublish(t *testing.T) {
	mr, q := setupTestRedis(t)
	defer mr.Close()
	defer q.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Create and publish test event
	testEvent := event.VaultEvent{
		ID:        "test-id",
		Path:      "test/path",
		Operation: logical.UpdateOperation,
	}

	err := q.Publish(ctx, testEvent)
	if err != nil {
		t.Fatalf("Failed to publish: %v", err)
	}

	// Verify the message is in Redis
	result, err := mr.List("queue")
	if err != nil {
		t.Fatalf("Failed to get list from Redis: %v", err)
	}
	if len(result) != 1 {
		t.Errorf("Expected 1 message in queue, got %d", len(result))
	}
}

func TestRedisQueue_Ping(t *testing.T) {
	mr, q := setupTestRedis(t)
	defer mr.Close()
	defer q.Stop()

	err := q.Ping()
	if err != nil {
		t.Errorf("Ping failed: %v", err)
	}

	mr.Close()
	err = q.Ping()
	if err == nil {
		t.Error("Expected ping to fail after closing connection")
	}
}

func TestRedisQueue_EventDeduplication(t *testing.T) {
	oldDedupe := Dedupe
	Dedupe = true
	defer func() { Dedupe = oldDedupe }()

	mr, q := setupTestRedis(t)
	defer mr.Close()
	defer q.Stop()

	eventID := "test-event-id"
	if q.EventSeen(eventID) {
		t.Error("Event should not be seen before marking it as seen")
	}

	q.SeenEvent(eventID)
	if !q.EventSeen(eventID) {
		t.Error("Event should be seen after marking it as seen")
	}
}

func TestRedisQueue_HighLoad(t *testing.T) {
	mr, q := setupTestRedis(t)

	// Create a shorter-lived context for publishing and receiving
	ctx, cancel := context.WithCancel(context.Background())

	// Number of events to test with
	const numEvents = 10000

	// Track received events
	receivedEvents := make(map[string]struct{})
	var receivedMu sync.Mutex
	var wg sync.WaitGroup
	wg.Add(numEvents)

	// Subscribe first
	ch, err := q.Subscribe(ctx)
	if err != nil {
		t.Fatalf("Failed to subscribe: %v", err)
	}

	// Start consumer
	go func() {
		for evt := range ch {
			receivedMu.Lock()
			receivedEvents[evt.ID] = struct{}{}
			receivedMu.Unlock()
			wg.Done()
		}
	}()

	// Small delay to ensure subscriber is ready
	time.Sleep(100 * time.Millisecond)

	// Publish events
	for i := 0; i < numEvents; i++ {
		evt := event.VaultEvent{
			ID:        fmt.Sprintf("event-%d", i),
			Path:      "test/path",
			Operation: logical.UpdateOperation,
		}
		if err := q.Publish(ctx, evt); err != nil {
			t.Errorf("Failed to publish event %d: %v", i, err)
		}
	}

	// Wait for completion or timeout
	allReceived := make(chan struct{})
	go func() {
		wg.Wait()
		close(allReceived)
	}()

	select {
	case <-allReceived:
		// Success case - verify all events were received
		receivedMu.Lock()
		received := len(receivedEvents)
		receivedMu.Unlock()

		if received != numEvents {
			t.Errorf("Expected %d events, got %d", numEvents, received)
			// Check which events are missing
			for i := 0; i < numEvents; i++ {
				eventID := fmt.Sprintf("event-%d", i)
				receivedMu.Lock()
				_, exists := receivedEvents[eventID]
				receivedMu.Unlock()
				if !exists {
					t.Logf("Missing event: %s", eventID)
				}
			}
		}

	case <-time.After(10 * time.Second):
		receivedMu.Lock()
		t.Errorf("Timeout: received only %d of %d events", len(receivedEvents), numEvents)
		receivedMu.Unlock()
	}

	// Cleanup in correct order
	cancel()                           // First cancel context to stop new operations
	time.Sleep(100 * time.Millisecond) // Give a moment for operations to stop
	q.Stop()                           // Then stop queue
	mr.Close()                         // Finally close miniredis
}
