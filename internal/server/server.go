package server

import (
	"cmp"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/hashicorp/vault/audit"
	"github.com/hashicorp/vault/sdk/logical"
	"github.com/robertlestak/vault-secret-sync/internal/config"
	"github.com/robertlestak/vault-secret-sync/internal/event"
	"github.com/robertlestak/vault-secret-sync/internal/metrics"
	"github.com/robertlestak/vault-secret-sync/internal/queue"
	"github.com/robertlestak/vault-secret-sync/internal/srvutils"
	"github.com/robertlestak/vault-secret-sync/internal/sync"
	log "github.com/sirupsen/logrus"
)

var (
	ignoreEvents = []logical.Operation{
		logical.ReadOperation,
	}
	monitoredEvents = []logical.Operation{
		logical.CreateOperation,
		logical.UpdateOperation,
		logical.DeleteOperation,
	}
)

func shouldFilterVaultEvent(event event.AuditEvent) bool {
	var shouldFilter bool
	l := log.WithFields(log.Fields{
		"action": "shouldFilterVaultEvent",
	})
	l.Trace("start")
	for _, ie := range ignoreEvents {
		if event.Event.Request.Operation == logical.Operation(ie) {
			l.Tracef("ignoring event: %s", ie)
			return true
		}
	}
	if len(monitoredEvents) > 0 {
		var found bool
		for _, me := range monitoredEvents {
			if event.Event.Request.Operation == logical.Operation(me) {
				found = true
				break
			}
		}
		if !found {
			l.Trace("event not in monitored events")
			return true
		}
	}
	if queue.Q.EventSeen(event.Event.Request.ID) {
		l.Trace("event already seen")
		return true
	}
	queue.Q.SeenEvent(event.Event.Request.ID)
	jd, jerr := json.Marshal(event)
	if jerr != nil {
		l.Error(jerr)
		return true
	}
	l.Tracef("handling event: %s", string(jd))
	l.Tracef("headers_received=%+v", event.Event.Request.Headers)
	if h, ok := event.Event.Request.Headers["x-vault-sync"]; ok {
		l.Debugf("x-vault-sync header found: %s", h)
		for _, v := range h {
			l.Debugf("x-vault-sync header value: %s", v)
			if v == "true" {
				l.Debug("x-vault-sync header value is true, skipping event")
				return true
			}
		}
	}
	l.Trace("end")
	return shouldFilter
}

// processVaultEvent accepts a single Vault audit event, determines if it needs to be synced,
// and if so, will sync the secret from source to destination
func processVaultEvent(ctx context.Context, event event.AuditEvent) error {
	l := log.WithFields(log.Fields{
		"action": "processVaultEvent",
	})
	l.Trace("start")
	if shouldFilterVaultEvent(event) {
		l.Trace("filtering event")
		return nil
	}
	l = l.WithFields(log.Fields{
		"tenant":  event.VaultTenant,
		"eventId": event.Event.Request.ID,
		"op":      event.Event.Request.Operation,
		"path":    event.Event.Request.Path,
	})
	l.WithFields(log.Fields{
		"data":  event.Event.Request.Data,
		"event": event.Event,
	}).Trace("handle event")
	if config.Config.Log.Events {
		l.Infof("event: %s", event.Event.Request.Operation)
	}
	evt := sync.NewVaultEventFromAuditEvent(event)
	err := sync.ScheduleSync(ctx, evt)
	if err != nil {
		l.Error(err)
		return err
	}
	l.Trace("end")
	return nil
}
func eventAuthValid(r *http.Request) bool {
	l := log.WithFields(log.Fields{
		"action": "eventAuthValid",
	})
	l.Trace("start")
	if config.Config.Events.Security.Enabled == nil {
		l.Warn("security not configured. failing safe, denying all requests. To disable security, explicitly set event.security.enabled: false")
		return false
	}
	if config.Config.Events.Security.Enabled != nil && !*config.Config.Events.Security.Enabled {
		// don't flood the logs with this if security is handled by the service layer
		l.Debug("security disabled, all requests allowed")
		return true
	}
	// security is enabled
	if config.Config.Events.Security.Token == "" && (config.Config.Events.Security.TLS == nil || config.Config.Events.Security.TLS.ClientAuth == nil) {
		l.Warn("security enabled but no token or client cert provided")
		return false
	}
	token := r.Header.Get("X-Vault-Secret-Sync-Token")
	tlsEnabled := config.Config.Events.Security.TLS != nil
	clientAuthEnabled := tlsEnabled && config.Config.Events.Security.TLS.ClientAuth != nil
	if clientAuthEnabled && (*config.Config.Events.Security.TLS.ClientAuth == "require" || *config.Config.Events.Security.TLS.ClientAuth == "verify") {
		if r.TLS == nil || len(r.TLS.VerifiedChains) == 0 {
			l.Debug("client cert required but not provided")
			return false
		}
		// If client cert is provided and valid, no need to check for token
		l.Trace("end")
		return true
	} else if config.Config.Events.Security.Token != "" {
		if token == "" {
			l.Debug("no token provided")
			return false
		}
		if config.Config.Events.Security.Token != token {
			l.Debug("invalid token provided")
			return false
		}
		// If token is provided and valid, no need to check for client cert
		l.Trace("end")
		return true
	}
	l.Warn("Neither token nor client cert provided")
	return false
}

// handleVaultEvents receives raw vault audit log events as jsonl format
// splits each event into a struct and processes each event independently
func handleVaultEvents(w http.ResponseWriter, r *http.Request) {
	l := log.WithFields(log.Fields{
		"action": "handleVaultEvents",
	})
	l.Trace("start")
	metrics.EventHandlerRequests.Inc()
	startTime := time.Now()
	defer func() {
		endTime := time.Now()
		metrics.EventHandlerRequestDuration.Observe(endTime.Sub(startTime).Seconds())
	}()
	callerIP := r.RemoteAddr
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		callerIP = xff
	}
	vaultTenant := r.Header.Get("X-Vault-Tenant")
	if !eventAuthValid(r) {
		l.Error("invalid auth")
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	for {
		var ev audit.ResponseEntry
		if err := dec.Decode(&ev); err == io.EOF {
			l.Trace("EOF")
			w.WriteHeader(http.StatusAccepted)
			break
		} else if err != nil {
			l.Error("error decoding event")
			metrics.EventHandlerErrors.Inc()
			l.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			break
		}
		if ev == (audit.ResponseEntry{}) {
			l.Trace("empty or invalid event")
			continue
		}
		l.Tracef("event=%+v", ev)
		ve := event.AuditEvent{
			RemoteAddr:  callerIP,
			VaultTenant: vaultTenant,
			Event:       ev,
		}
		// using a background context to ensure the event is processed even if the request is cancelled
		ctx := context.Background()
		go processVaultEvent(ctx, ve)
	}
	l.Trace("end")
}

func EventServer(port int, tlsConfig *srvutils.TLSConfig) {
	l := log.WithFields(log.Fields{
		"action": "EventServer",
		"pkg":    "server",
	})
	l.Trace("start")
	r := mux.NewRouter()
	r.HandleFunc("/events", handleVaultEvents)
	port = cmp.Or(port, 8080)
	if tlsConfig != nil && tlsConfig.Cert != "" && tlsConfig.Key != "" {
		l.Infof("starting server on port %d with tls", port)
	} else {
		l.Infof("starting server on port %d", port)
	}
	srv, err := srvutils.SetupServer(r, port, tlsConfig)
	if err != nil {
		l.Fatal(err)
	}
	metrics.RegisterServiceHealth("events", metrics.ServiceHealthStatusOK)
	if tlsConfig != nil && tlsConfig.Cert != "" && tlsConfig.Key != "" {
		l.Fatal(srv.ListenAndServeTLS(tlsConfig.Cert, tlsConfig.Key))
	} else {
		l.Fatal(srv.ListenAndServe())
	}
	metrics.RegisterServiceHealth("events", metrics.ServiceHealthStatusCritical)
}
