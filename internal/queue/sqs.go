package queue

import (
	"context"
	"encoding/json"
	"fmt"
	gosync "sync"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/robertlestak/vault-secret-sync/internal/event"
)

type SQSQueue struct {
	Url     string `json:"url" yaml:"url"`
	Region  string `json:"region" yaml:"region"`
	RoleArn string `json:"roleArn" yaml:"roleArn"`

	client *sqs.Client

	seenEvents  map[string]time.Time
	eventsMutex gosync.Mutex

	eventCh chan event.VaultEvent
}

func NewSQSQueue() *SQSQueue {
	return &SQSQueue{
		seenEvents: make(map[string]time.Time),
		eventCh:    make(chan event.VaultEvent, 1000), // Adjust buffer size as needed
	}
}

func (q *SQSQueue) Start(params map[string]any) error {
	l := log.WithFields(log.Fields{
		"action": "Start",
		"driver": "sqs",
	})
	l.Trace("start")
	defer l.Trace("end")
	go q.eventClearer()
	awscfg, err := config.LoadDefaultConfig(context.TODO())
	if err != nil {
		l.Debugf("error: %v", err)
		return err
	}
	var provider aws.CredentialsProvider
	if q.RoleArn != "" {
		stsclient := sts.NewFromConfig(awscfg)
		provider = stscreds.NewAssumeRoleProvider(stsclient, q.RoleArn)
		awscfg.Credentials = provider
	}
	svc := sqs.New(sqs.Options{
		Credentials: awscfg.Credentials,
		Region:      q.Region,
	})
	q.client = svc
	return nil
}

func (q *SQSQueue) Stop() error {
	close(q.eventCh)
	return nil
}

func (q *SQSQueue) Publish(ctx context.Context, e event.VaultEvent) error {
	l := log.WithFields(log.Fields{
		"action": "Publish",
		"driver": "sqs",
	})
	l.Trace("start")
	defer l.Trace("end")
	body, err := json.Marshal(e)
	if err != nil {
		l.Errorf("error: %v", err)
		return err
	}

	_, err = q.client.SendMessage(ctx, &sqs.SendMessageInput{
		QueueUrl:    &q.Url,
		MessageBody: aws.String(string(body)),
	})

	return err
}

func (q *SQSQueue) Subscribe(ctx context.Context) (chan event.VaultEvent, error) {
	l := log.WithFields(log.Fields{
		"action": "Subscribe",
		"driver": "sqs",
	})
	l.Trace("start")
	defer l.Trace("end")
	out := make(chan event.VaultEvent)

	go func() {
		for {
			result, err := q.client.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
				QueueUrl:            &q.Url,
				MaxNumberOfMessages: 1,
				VisibilityTimeout:   20,
				WaitTimeSeconds:     0,
			})

			if err != nil {
				continue
			}

			if len(result.Messages) == 0 {
				continue
			}

			var e event.VaultEvent
			if err := json.Unmarshal([]byte(*result.Messages[0].Body), &e); err != nil {
				l.Debugf("error: %v", err)
				continue
			}

			select {
			case q.eventCh <- e:
			default:
				log.Warn("Event channel is full, dropping event")
			}
		}
	}()

	go func() {
		for evt := range q.eventCh {
			select {
			case out <- evt:
			case <-ctx.Done():
				return
			}
		}
	}()

	return out, nil
}

func (q *SQSQueue) Push(evt event.VaultEvent) error {
	select {
	case q.eventCh <- evt:
		return nil
	default:
		return fmt.Errorf("failed to push event to local channel")
	}
}

func (q *SQSQueue) eventClearer() {
	l := log.WithFields(log.Fields{
		"action": "eventClearer",
		"driver": "sqs",
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

func (q *SQSQueue) SeenEvent(id string) {
	l := log.WithFields(log.Fields{
		"action": "logEventSeen",
		"driver": "sqs",
	})
	l.Trace("start")
	q.eventsMutex.Lock()
	q.seenEvents[id] = time.Now()
	q.eventsMutex.Unlock()
	l.Trace("end")
}

func (q *SQSQueue) EventSeen(id string) bool {
	l := log.WithFields(log.Fields{
		"action": "eventSeen",
		"driver": "sqs",
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

func (q *SQSQueue) Ping() error {
	// ping sqs endpoint
	_, err := q.client.GetQueueAttributes(context.TODO(), &sqs.GetQueueAttributesInput{
		QueueUrl: &q.Url,
		AttributeNames: []types.QueueAttributeName{
			types.QueueAttributeNameQueueArn, // Minimal attribute
		},
	})
	return err
}
