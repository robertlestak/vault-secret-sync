package queue

import (
	"container/list"
	"context"
	gosync "sync"
)

// UnboundedChannel implements an unbounded FIFO queue backed by a linked list
type UnboundedChannel struct {
	mu    gosync.Mutex
	list  *list.List
	ready chan struct{} // signals that data is available
}

func NewUnboundedChannel() *UnboundedChannel {
	return &UnboundedChannel{
		list:  list.New(),
		ready: make(chan struct{}, 1),
	}
}

// Send adds an item to the queue
func (u *UnboundedChannel) Send(v interface{}) {
	u.mu.Lock()
	u.list.PushBack(v)
	u.mu.Unlock()
	// Non-blocking send to signal data is available
	select {
	case u.ready <- struct{}{}:
	default:
	}
}

// Receive removes and returns the first item from the queue
// It blocks if the queue is empty
func (u *UnboundedChannel) Receive(ctx context.Context) (interface{}, error) {
	for {
		u.mu.Lock()
		if u.list.Len() > 0 {
			v := u.list.Remove(u.list.Front())
			u.mu.Unlock()
			return v, nil
		}
		u.mu.Unlock()

		// Wait for data or context cancellation
		select {
		case <-u.ready:
			continue
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

// Len returns the current length of the queue
func (u *UnboundedChannel) Len() int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.list.Len()
}
