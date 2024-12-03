package queue

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"os"
	gosync "sync"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/robertlestak/vault-secret-sync/internal/event"
	log "github.com/sirupsen/logrus"
)

type NATSQueue struct {
	Url         string     `json:"url" yaml:"url"`
	Subject     string     `json:"subject" yaml:"subject"`
	TLS         *TLSConfig `json:"tls" yaml:"tls"`
	seenEvents  map[string]time.Time
	eventsMutex gosync.Mutex
	nc          *nats.Conn
	eventQueue  *UnboundedChannel
}

func NewNATSQueue() *NATSQueue {
	return &NATSQueue{
		seenEvents: make(map[string]time.Time),
		eventQueue: NewUnboundedChannel(),
	}
}

func (q *NATSQueue) Start(params map[string]any) error {
	var err error
	url := params["url"].(string)
	if url == "" {
		url = nats.DefaultURL
	}
	opts := nats.GetDefaultOptions()
	if q.TLS != nil {
		if q.TLS.CA != "" {
			// Load CA cert
			caCert, err := os.ReadFile(q.TLS.CA)
			if err != nil {
				return err
			}
			caCertPool := x509.NewCertPool()
			caCertPool.AppendCertsFromPEM(caCert)
			opts.TLSConfig = &tls.Config{
				RootCAs: caCertPool,
			}
		}
		if q.TLS.Cert != "" && q.TLS.Key != "" {
			cert, err := tls.LoadX509KeyPair(q.TLS.Cert, q.TLS.Key)
			if err != nil {
				return err
			}
			opts.TLSConfig.Certificates = []tls.Certificate{cert}
		}
		q.nc, err = nats.Connect(url, nats.Secure(opts.TLSConfig))
	} else {
		q.nc, err = nats.Connect(url)
	}
	if err != nil {
		return err
	}
	q.Subject = params["subject"].(string)
	if q.Subject == "" {
		q.Subject = "vault-secret-sync"
	}
	go q.eventClearer()
	return nil
}

func (q *NATSQueue) Stop() error {
	q.nc.Close()
	return nil
}

func (q *NATSQueue) Publish(ctx context.Context, e event.VaultEvent) error {
	body, err := json.Marshal(e)
	if err != nil {
		return err
	}
	return q.nc.Publish(q.Subject, body)
}

func (q *NATSQueue) Subscribe(ctx context.Context) (chan event.VaultEvent, error) {
	l := log.WithFields(log.Fields{
		"action": "Subscribe",
		"driver": "nats",
	})
	l.Trace("start")

	out := make(chan event.VaultEvent)

	// Subscribe to NATS and send events to the unbounded queue
	_, err := q.nc.Subscribe(q.Subject, func(m *nats.Msg) {
		var e event.VaultEvent
		if err := json.Unmarshal(m.Data, &e); err != nil {
			l.Errorf("error unmarshalling event: %v", err)
			return
		}
		q.eventQueue.Send(e)
	})

	if err != nil {
		return nil, fmt.Errorf("failed to subscribe to NATS: %v", err)
	}

	// Start the event distributor
	go func() {
		defer close(out)
		for {
			evt, err := q.eventQueue.Receive(ctx)
			if err != nil {
				if err == context.Canceled {
					return
				}
				l.Errorf("error receiving from queue: %v", err)
				continue
			}

			select {
			case out <- evt.(event.VaultEvent):
			case <-ctx.Done():
				return
			}
		}
	}()

	return out, nil
}

func (q *NATSQueue) Push(evt event.VaultEvent) error {
	q.eventQueue.Send(evt)
	return nil
}

func (q *NATSQueue) eventClearer() {
	l := log.WithFields(log.Fields{
		"action": "eventClearer",
		"driver": "nats",
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

func (q *NATSQueue) SeenEvent(id string) {
	l := log.WithFields(log.Fields{
		"action": "logEventSeen",
		"driver": "nats",
	})
	l.Trace("start")
	q.eventsMutex.Lock()
	q.seenEvents[id] = time.Now()
	q.eventsMutex.Unlock()
	l.Trace("end")
}

func (q *NATSQueue) EventSeen(id string) bool {
	l := log.WithFields(log.Fields{
		"action": "eventSeen",
		"driver": "nats",
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

func (q *NATSQueue) Ping() error {
	err := q.nc.Flush()
	if err != nil {
		return err
	}
	if lastErr := q.nc.LastError(); lastErr != nil {
		return lastErr
	}
	return nil
}
