package queue

import (
	"context"
	"errors"
	"time"

	"github.com/robertlestak/vault-secret-sync/internal/event"
	"github.com/robertlestak/vault-secret-sync/internal/metrics"
	log "github.com/sirupsen/logrus"
)

var (
	Q      Queue
	Dedupe bool
)

type QueueType string

const (
	QueueTypeMemory QueueType = "memory"
	QueueTypeNATS   QueueType = "nats"
	QueueTypeRedis  QueueType = "redis"
	QueueTypeSQS    QueueType = "sqs"
)

type Queue interface {
	Start(params map[string]any) error
	Stop() error
	Publish(context.Context, event.VaultEvent) error
	Push(event.VaultEvent) error
	Subscribe(ctx context.Context) (chan event.VaultEvent, error)
	SeenEvent(string)
	EventSeen(string) bool
	Ping() error
}

func NewQueue(t QueueType) (Queue, error) {
	l := log.WithFields(log.Fields{
		"action": "NewQueue",
		"type":   t,
		"pkg":    "queue",
	})
	l.Trace("start")
	defer l.Trace("end")
	switch t {
	case QueueTypeMemory:
		return NewMemoryQueue(), nil
	case QueueTypeNATS:
		return NewNATSQueue(), nil
	case QueueTypeRedis:
		return NewRedisQueue(), nil
	case QueueTypeSQS:
		return NewSQSQueue(), nil
	default:
		return nil, errors.New("unknown queue type")
	}
}

func Init(t QueueType, params map[string]any) error {
	l := log.WithFields(log.Fields{
		"action": "Init",
		"type":   t,
		"pkg":    "queue",
	})
	l.Trace("start")
	defer l.Trace("end")
	q, err := NewQueue(t)
	if err != nil {
		l.Errorf("error: %v", err)
		metrics.RegisterServiceHealth("queue", metrics.ServiceHealthStatusCritical)
		return err
	}
	err = q.Start(params)
	if err != nil {
		l.Errorf("error: %v", err)
		metrics.RegisterServiceHealth("queue", metrics.ServiceHealthStatusCritical)
		return err
	}
	Q = q
	metrics.RegisterServiceHealth("queue", metrics.ServiceHealthStatusOK)
	// start the queue pinger, if it fails, die
	go func() {
		for {
			err := Q.Ping()
			if err != nil {
				l.Errorf("error: %v", err)
				metrics.RegisterServiceHealth("queue", metrics.ServiceHealthStatusCritical)
				// goodbye, world!
				l.Fatal("queue ping failed")
			}
			// sleep for 10 seconds
			<-time.After(10 * time.Second)
		}
	}()
	return nil
}
