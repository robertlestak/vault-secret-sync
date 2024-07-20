package metrics

import (
	"cmp"
	"encoding/json"
	"net/http"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/robertlestak/vault-secret-sync/internal/srvutils"
	log "github.com/sirupsen/logrus"
)

var (
	Health              *ServiceHealth
	healthMutex         sync.Mutex
	ServiceHealthMetric = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "vault_secret_sync_service_health",
		Help: "The health of the service",
	}, []string{"service"})
	ActiveSyncs = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "vault_secret_sync_active_syncs",
		Help: "The number of active syncs",
	}, []string{"namespace", "name"})
	SyncDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "vault_secret_sync_sync_duration",
		Help:    "The duration of a sync",
		Buckets: prometheus.ExponentialBuckets(1, 2, 10),
	}, []string{"namespace", "name"})
	SyncErrors = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "vault_secret_sync_sync_errors",
		Help: "The number of sync errors",
	}, []string{"namespace", "name"})
	SyncsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "vault_secret_sync_syncs_total",
		Help: "The total number of syncs",
	}, []string{"namespace", "name"})
	SyncStatus = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "vault_secret_sync_sync_status",
		Help: "The status of a sync",
	}, []string{"namespace", "name"})
	EventHandlerRequests = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "vault_secret_sync_event_handler_requests",
		Help: "The number of event handler requests",
	})
	EventHandlerRequestDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "vault_secret_sync_event_handler_request_duration",
		Help:    "The duration of an event handler request",
		Buckets: prometheus.ExponentialBuckets(1, 2, 10)})
	EventHandlerErrors = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "vault_secret_sync_event_handler_errors",
		Help: "The number of event handler errors",
	})
	EventProcessingDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "vault_secret_sync_event_processing_duration",
		Help:    "The duration of event processing",
		Buckets: prometheus.ExponentialBuckets(1, 2, 10)})
	EventProcessingErrors = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "vault_secret_sync_event_processing_errors",
		Help: "The number of event processing errors",
	})
	EventsProcessed = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "vault_secret_sync_events_processed",
		Help: "The number of events processed",
	})
	ManualSyncRequests = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "vault_secret_sync_manual_sync_requests",
		Help: "The number of manual sync requests",
	}, []string{"namespace", "name"})
	ManualSyncErrors = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "vault_secret_sync_manual_sync_errors",
		Help: "The number of manual sync errors",
	}, []string{"namespace", "name"})
	ManualSyncDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "vault_secret_sync_manual_sync_duration",
		Help:    "The duration of a manual sync",
		Buckets: prometheus.ExponentialBuckets(1, 2, 10),
	}, []string{"namespace", "name"})
)

type ServiceHealthStatus string

const (
	ServiceHealthStatusOK       ServiceHealthStatus = "ok"
	ServiceHealthStatusWarning  ServiceHealthStatus = "warning"
	ServiceHealthStatusCritical ServiceHealthStatus = "critical"
)

type ServiceHealth struct {
	Services map[string]ServiceHealthStatus
	Status   ServiceHealthStatus
}

func init() {
	prometheus.MustRegister(ActiveSyncs)
	prometheus.MustRegister(SyncDuration)
	prometheus.MustRegister(SyncErrors)
	prometheus.MustRegister(SyncsTotal)
	prometheus.MustRegister(SyncStatus)
}

func NewServiceHealth() *ServiceHealth {
	Health = &ServiceHealth{
		Services: make(map[string]ServiceHealthStatus),
		Status:   ServiceHealthStatusOK,
	}
	return Health
}

func RegisterServiceHealth(name string, status ServiceHealthStatus) {
	healthMutex.Lock()
	defer healthMutex.Unlock()
	if Health == nil {
		NewServiceHealth()
	}
	Health.Services[name] = status
}

func DetermineOverallHealth() ServiceHealthStatus {
	healthMutex.Lock()
	defer healthMutex.Unlock()
	if Health == nil {
		NewServiceHealth()
	}
	for _, v := range Health.Services {
		if v == ServiceHealthStatusCritical {
			Health.Status = ServiceHealthStatusCritical
			return ServiceHealthStatusCritical
		}
		if v == ServiceHealthStatusWarning {
			Health.Status = ServiceHealthStatusWarning
			return ServiceHealthStatusWarning
		}
	}
	Health.Status = ServiceHealthStatusOK
	return ServiceHealthStatusOK
}

func Start(port int, tls *srvutils.TLSConfig) {
	l := log.WithFields(log.Fields{
		"pkg": "metrics",
		"fn":  "Start",
	})
	port = cmp.Or(port, 9090)
	l.Infof("starting metrics server on port %d", port)
	r := http.NewServeMux()
	r.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		h := DetermineOverallHealth()
		switch h {
		case ServiceHealthStatusOK:
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(Health)
		case ServiceHealthStatusWarning:
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(Health)
		case ServiceHealthStatusCritical:
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(Health)
		}
	})
	r.Handle("/metrics", promhttp.Handler())
	s, err := srvutils.SetupServer(r, port, tls)
	if err != nil {
		l.Fatal(err)
	}
	if err := s.ListenAndServe(); err != nil {
		l.Fatal(err)
	}
}
