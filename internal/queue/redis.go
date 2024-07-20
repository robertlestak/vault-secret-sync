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

	log "github.com/sirupsen/logrus"

	"github.com/go-redis/redis"
	"github.com/robertlestak/vault-secret-sync/internal/event"
)

type TLSConfig struct {
	CA   string `json:"ca" yaml:"ca"`
	Cert string `json:"cert" yaml:"cert"`
	Key  string `json:"key" yaml:"key"`
}

type RedisQueue struct {
	Host     string     `yaml:"host" json:"host"`
	Port     int        `yaml:"port" json:"port"`
	Database int        `yaml:"database" json:"database"`
	Password string     `yaml:"password" json:"password"`
	TLS      *TLSConfig `yaml:"tls" json:"tls"`

	seenEvents  map[string]time.Time
	eventsMutex gosync.Mutex

	client  *redis.Client
	eventCh chan event.VaultEvent
}

func NewRedisQueue() *RedisQueue {
	return &RedisQueue{
		seenEvents: make(map[string]time.Time),
		eventCh:    make(chan event.VaultEvent, 100), // Adjust buffer size as needed
	}
}

func (q *RedisQueue) Start(params map[string]any) error {
	jd, err := json.Marshal(params)
	if err != nil {
		return err
	}
	err = json.Unmarshal(jd, q)
	if err != nil {
		return err
	}
	opts := &redis.Options{
		Addr:        fmt.Sprintf("%s:%d", q.Host, q.Port),
		Password:    q.Password, // no password set
		DB:          q.Database, // use default DB
		DialTimeout: 30 * time.Second,
		ReadTimeout: 30 * time.Second,
	}
	if q.TLS != nil {
		opts.TLSConfig = &tls.Config{
			RootCAs:    x509.NewCertPool(),
			ServerName: q.Host,
		}
		if q.TLS.CA != "" {
			caCert, err := os.ReadFile(q.TLS.CA)
			if err != nil {
				return err
			}
			opts.TLSConfig.RootCAs.AppendCertsFromPEM(caCert)
		}
		if q.TLS.Cert != "" && q.TLS.Key != "" {
			cert, err := tls.LoadX509KeyPair(q.TLS.Cert, q.TLS.Key)
			if err != nil {
				return err
			}
			opts.TLSConfig.Certificates = append(opts.TLSConfig.Certificates, cert)
		}
	}
	q.client = redis.NewClient(opts)
	cmd := q.client.Ping()
	if cmd.Err() != nil {
		return cmd.Err()
	}
	go q.eventClearer()
	return nil
}

func (q *RedisQueue) Stop() error {
	err := q.client.Close()
	if err != nil {
		return err
	}
	close(q.eventCh)
	return nil
}

func (q *RedisQueue) Publish(ctx context.Context, item event.VaultEvent) error {
	l := log.WithFields(log.Fields{
		"action": "Publish",
		"driver": "redis",
	})
	l.Trace("start")
	defer l.Trace("end")
	jd, err := json.Marshal(item)
	if err != nil {
		l.Errorf("error: %v", err)
		return err
	}
	cmd := q.client.RPush("queue", jd)
	if cmd.Err() != nil {
		l.Errorf("error: %v", cmd.Err())
		return cmd.Err()
	}
	return nil
}

func (q *RedisQueue) Subscribe(ctx context.Context) (chan event.VaultEvent, error) {
	l := log.WithFields(log.Fields{
		"action": "Subscribe",
		"driver": "redis",
	})
	l.Trace("start")
	ch := make(chan event.VaultEvent)

	go func() {
		for {
			cmd := q.client.BLPop(0, "queue")
			if cmd.Err() != nil {
				break
			}
			var item event.VaultEvent
			err := json.Unmarshal([]byte(cmd.Val()[1]), &item)
			if err != nil {
				l.Errorf("error unmarshalling event: %v", err)
				break
			}
			select {
			case q.eventCh <- item:
			default:
				log.Warn("Event channel is full, dropping event")
			}
		}
	}()

	go func() {
		for evt := range q.eventCh {
			select {
			case ch <- evt:
			case <-ctx.Done():
				return
			}
		}
	}()

	return ch, nil
}

func (q *RedisQueue) Push(evt event.VaultEvent) error {
	select {
	case q.eventCh <- evt:
		return nil
	default:
		return fmt.Errorf("failed to push event to local channel")
	}
}

func (q *RedisQueue) eventClearer() {
	l := log.WithFields(log.Fields{
		"action": "eventClearer",
		"driver": "redis",
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

func (q *RedisQueue) SeenEvent(id string) {
	l := log.WithFields(log.Fields{
		"action": "logEventSeen",
		"driver": "redis",
	})
	l.Trace("start")
	q.eventsMutex.Lock()
	q.seenEvents[id] = time.Now()
	q.eventsMutex.Unlock()
	l.Trace("end")
}

func (q *RedisQueue) EventSeen(id string) bool {
	l := log.WithFields(log.Fields{
		"action": "eventSeen",
		"driver": "redis",
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

func (q *RedisQueue) Ping() error {
	return q.client.Ping().Err()
}
