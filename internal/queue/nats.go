package queue

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
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

	nc      *nats.Conn
	eventCh chan event.VaultEvent
}

func NewNATSQueue() *NATSQueue {
	return &NATSQueue{
		seenEvents: make(map[string]time.Time),
		eventCh:    make(chan event.VaultEvent, 100), // Adjust buffer size as needed
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
	close(q.eventCh)
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
	out := make(chan event.VaultEvent)

	_, err := q.nc.Subscribe(q.Subject, func(m *nats.Msg) {
		var e event.VaultEvent
		if err := json.Unmarshal(m.Data, &e); err != nil {
			return
		}

		select {
		case q.eventCh <- e:
		default:
			log.Warn("Event channel is full, dropping event")
		}
	})

	go func() {
		for evt := range q.eventCh {
			select {
			case out <- evt:
			case <-ctx.Done():
				return
			}
		}
	}()

	return out, err
}

func (q *NATSQueue) Push(evt event.VaultEvent) error {
	select {
	case q.eventCh <- evt:
		return nil
	default:
		return errors.New("failed to push event to local channel")
	}
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
	// Assuming q.conn is your NATS connection object
	err := q.nc.Flush()
	if err != nil {
		return err // If err is not nil, there was a problem reaching the NATS server
	}
	// Optionally, check for last error reported by the connection.
	if lastErr := q.nc.LastError(); lastErr != nil {
		return lastErr
	}
	return nil // If no error, the NATS server is reachable
}
