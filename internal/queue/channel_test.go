package queue

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestUnboundedChannel_Basic(t *testing.T) {
	uc := NewUnboundedChannel()
	ctx := context.Background()

	// Test basic send/receive
	uc.Send("test1")
	if uc.Len() != 1 {
		t.Errorf("Expected length 1, got %d", uc.Len())
	}

	val, err := uc.Receive(ctx)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if val != "test1" {
		t.Errorf("Expected 'test1', got %v", val)
	}
	if uc.Len() != 0 {
		t.Errorf("Expected length 0, got %d", uc.Len())
	}
}

func TestUnboundedChannel_ConcurrentAccess(t *testing.T) {
	uc := NewUnboundedChannel()
	ctx := context.Background()
	const numItems = 1000
	var wg sync.WaitGroup

	// Test concurrent sends
	wg.Add(numItems)
	for i := 0; i < numItems; i++ {
		go func(val int) {
			defer wg.Done()
			uc.Send(val)
		}(i)
	}
	wg.Wait()

	if uc.Len() != numItems {
		t.Errorf("Expected length %d, got %d", numItems, uc.Len())
	}

	// Test concurrent receives
	receivedMap := make(map[int]bool)
	var mu sync.Mutex
	wg.Add(numItems)
	for i := 0; i < numItems; i++ {
		go func() {
			defer wg.Done()
			val, err := uc.Receive(ctx)
			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}
			mu.Lock()
			receivedMap[val.(int)] = true
			mu.Unlock()
		}()
	}
	wg.Wait()

	if len(receivedMap) != numItems {
		t.Errorf("Expected %d unique items, got %d", numItems, len(receivedMap))
	}
}

func TestUnboundedChannel_ContextCancellation(t *testing.T) {
	uc := NewUnboundedChannel()
	ctx, cancel := context.WithCancel(context.Background())

	// Start a goroutine that will try to receive
	errCh := make(chan error)
	go func() {
		_, err := uc.Receive(ctx)
		errCh <- err
	}()

	// Cancel the context after a short delay
	time.Sleep(100 * time.Millisecond)
	cancel()

	// Check if we get the expected context cancellation error
	select {
	case err := <-errCh:
		if err != context.Canceled {
			t.Errorf("Expected context.Canceled error, got %v", err)
		}
	case <-time.After(time.Second):
		t.Error("Timeout waiting for context cancellation")
	}
}

func TestUnboundedChannel_Order(t *testing.T) {
	uc := NewUnboundedChannel()
	ctx := context.Background()
	items := []string{"first", "second", "third", "fourth", "fifth"}

	// Send items
	for _, item := range items {
		uc.Send(item)
	}

	// Verify order
	for _, expected := range items {
		val, err := uc.Receive(ctx)
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}
		if val != expected {
			t.Errorf("Expected '%s', got '%s'", expected, val)
		}
	}
}

func TestUnboundedChannel_HighLoad(t *testing.T) {
	uc := NewUnboundedChannel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	const (
		numPublishers = 50
		numConsumers  = 10
		eventsPerPub  = 1000
		totalEvents   = numPublishers * eventsPerPub
	)

	// Track received messages
	received := make(map[string]int)
	var receivedMu sync.Mutex
	var wg sync.WaitGroup
	wg.Add(totalEvents)

	// Start consumers
	for i := 0; i < numConsumers; i++ {
		go func(consumerID int) {
			for {
				msg, err := uc.Receive(ctx)
				if err != nil {
					if err == context.Canceled {
						return
					}
					t.Errorf("Consumer %d receive error: %v", consumerID, err)
					return
				}

				// Record the received message
				receivedMu.Lock()
				received[msg.(string)]++
				wg.Done()
				receivedMu.Unlock()
			}
		}(i)
	}

	// Start publishers
	start := time.Now()
	var pubWg sync.WaitGroup
	pubWg.Add(numPublishers)

	for i := 0; i < numPublishers; i++ {
		go func(pubID int) {
			defer pubWg.Done()
			for j := 0; j < eventsPerPub; j++ {
				msg := fmt.Sprintf("pub-%d-msg-%d", pubID, j)
				uc.Send(msg)
			}
		}(i)
	}

	// Wait for publishers to finish
	pubWg.Wait()
	duration := time.Since(start)

	// Wait for all messages to be received or timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success case
	case <-time.After(10 * time.Second):
		t.Fatal("Timeout waiting for messages to be processed")
	}

	// Verify results
	receivedMu.Lock()
	defer receivedMu.Unlock()

	// Check total count
	totalReceived := 0
	duplicates := 0
	missing := 0

	for i := 0; i < numPublishers; i++ {
		for j := 0; j < eventsPerPub; j++ {
			msg := fmt.Sprintf("pub-%d-msg-%d", i, j)
			count := received[msg]
			totalReceived += count

			if count == 0 {
				missing++
				t.Errorf("Message not received: %s", msg)
			} else if count > 1 {
				duplicates++
				t.Errorf("Message received multiple times: %s (%d times)", msg, count)
			}
		}
	}

	// Report results
	t.Logf("Results:")
	t.Logf("- Time taken: %v", duration)
	t.Logf("- Messages per second: %v", float64(totalEvents)/duration.Seconds())
	t.Logf("- Total messages expected: %d", totalEvents)
	t.Logf("- Total messages received: %d", totalReceived)
	t.Logf("- Missing messages: %d", missing)
	t.Logf("- Duplicate messages: %d", duplicates)

	if missing > 0 {
		t.Errorf("Missing %d messages", missing)
	}
	if duplicates > 0 {
		t.Errorf("Found %d duplicate messages", duplicates)
	}
	if totalReceived != totalEvents {
		t.Errorf("Expected %d total messages, got %d", totalEvents, totalReceived)
	}
}
