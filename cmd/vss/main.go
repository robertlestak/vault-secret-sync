package main

import (
	"cmp"
	"context"
	"errors"
	"flag"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/robertlestak/vault-secret-sync/api/v1alpha1"
	"github.com/robertlestak/vault-secret-sync/internal/backend"
	"github.com/robertlestak/vault-secret-sync/internal/config"
	"github.com/robertlestak/vault-secret-sync/internal/metrics"
	"github.com/robertlestak/vault-secret-sync/internal/queue"
	"github.com/robertlestak/vault-secret-sync/internal/server"
	"github.com/robertlestak/vault-secret-sync/internal/sync"
	"github.com/robertlestak/vault-secret-sync/pkg/driver"
	log "github.com/sirupsen/logrus"
)

func setLogLevelStr(level string) {
	ll, err := log.ParseLevel(level)
	if err != nil {
		ll = log.InfoLevel
	}
	log.SetLevel(ll)
}

func init() {
	setLogLevelStr(os.Getenv("LOG_LEVEL"))
	// set the log format
	//log.SetFormatter(&log.JSONFormatter{})
	backend.ManualTrigger = sync.ManualTrigger
}

func initQueue() error {
	l := log.WithFields(log.Fields{
		"action": "initQueue",
	})
	l.Trace("start")
	defer l.Trace("end")
	if config.Config.Queue == nil {
		config.Config.Queue = &config.QueueConfig{
			Type: queue.QueueTypeMemory,
		}
	}
	queueType := config.Config.Queue.Type
	queueParams := config.Config.Queue.Params
	queueType = cmp.Or(queueType, queue.QueueTypeMemory)
	dedupe := config.Config.Events.Dedupe
	if dedupe == nil || *dedupe {
		queue.Dedupe = true
	}
	// if queue type is memory and we don't also see both the operator
	// and event server running, throw an error
	// since the memory queue is not shared between processes
	// it can only be used in single-binary mode
	if queueType == queue.QueueTypeMemory {
		if (config.Config.Operator == nil || (config.Config.Operator.Enabled == nil || !*config.Config.Operator.Enabled)) &&
			(config.Config.Events == nil || config.Config.Events.Enabled == nil || !*config.Config.Events.Enabled) {
			return errors.New("memory queue can only be used in single-binary mode")
		}
	}
	if err := queue.Init(queueType, queueParams); err != nil {
		l.Error(err)
		return err
	}
	return nil
}

func cleanup() {
	l := log.WithFields(log.Fields{
		"action": "cleanup",
	})
	l.Trace("start")
	defer l.Trace("end")
}

func main() {
	l := log.WithFields(log.Fields{
		"action": "main",
	})
	l.Trace("start")
	configFile := flag.String("config", "config.yaml", "config file")
	startOperator := flag.Bool("operator", false, "start operator")
	startEvent := flag.Bool("events", false, "start event server")
	metricsPort := flag.Int("metrics-port", 9090, "The port the metric endpoint binds to.")
	kubeMetricsAddr := flag.String("kube-metrics-addr", ":9080", "The address the kubernetes operator metric endpoint binds to.")
	enableLeaderElection := flag.Bool("enable-leader-election", false, "Enable leader election for controller manager.")
	leaderElectionNamespace := flag.String("leader-election-namespace", "", "Namespace used for leader election")
	leaderElectionId := flag.String("leader-election-id", "vault-secret-sync-leader-election", "ID used for leader election")
	flag.Parse()
	if err := config.LoadFile(*configFile); err != nil {
		l.Fatal(err)
	}
	if config.Config.LogLevel != "" {
		setLogLevelStr(config.Config.LogLevel)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel() // Make sure all resources are cleaned up

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
	startServers := []string{}

	if metricsPort != nil {
		config.Config.Metrics.Port = *metricsPort
	}

	cliFlagProvided := *startOperator || *startEvent
	enabledTrue := true
	go metrics.Start(config.Config.Metrics.Port, config.Config.Metrics.Security.TLS)
	if (!cliFlagProvided && config.Config.Operator != nil && config.Config.Operator.Enabled != nil && *config.Config.Operator.Enabled) || *startOperator {
		config.Config.Operator.Enabled = &enabledTrue
		if config.Config.Operator.Backend.Type == backend.BackendTypeKubernetes {
			if config.Config.Operator.Backend.Params == nil {
				config.Config.Operator.Backend.Params = make(map[string]any)
			}
			if config.Config.Operator.Backend.Params["metricsAddr"] == nil {
				config.Config.Operator.Backend.Params["metricsAddr"] = *kubeMetricsAddr
			}
			if config.Config.Operator.Backend.Params["enableLeaderElection"] == nil {
				config.Config.Operator.Backend.Params["enableLeaderElection"] = *enableLeaderElection
			}
			if config.Config.Operator.Backend.Params["leaderElectionNamespace"] == nil {
				config.Config.Operator.Backend.Params["leaderElectionNamespace"] = *leaderElectionNamespace
			}
			if config.Config.Operator.Backend.Params["leaderElectionId"] == nil {
				config.Config.Operator.Backend.Params["leaderElectionId"] = *leaderElectionId
			}
		}
		if config.Config.Stores != nil {
			if sync.DefaultConfigs == nil {
				sync.DefaultConfigs = make(map[driver.DriverName]*v1alpha1.StoreConfig)
			}
			sync.SetStoreDefaults(config.Config.Stores)
		}
		startServers = append(startServers, "operator")
	}
	if (!cliFlagProvided && config.Config.Events != nil && config.Config.Events.Enabled != nil && *config.Config.Events.Enabled) || *startEvent {
		config.Config.Events.Enabled = &enabledTrue
		startServers = append(startServers, "event")
	}

	// now that we have all the config loaded, we can initialize the services
	// first we need to initialize the queue
	if err := initQueue(); err != nil {
		l.Fatal(err)
	}

	// start the servers
	if strings.Contains(strings.Join(startServers, ","), "operator") {
		config.Config.Operator.WorkerPoolSize = cmp.Or(config.Config.Operator.WorkerPoolSize, 10)
		config.Config.Operator.NumSubscriptions = cmp.Or(config.Config.Operator.NumSubscriptions, 10)
		go sync.Operator(
			ctx,
			config.Config.Operator.Backend.Params,
			config.Config.Operator.WorkerPoolSize,
			config.Config.Operator.NumSubscriptions,
		)
	}

	if strings.Contains(strings.Join(startServers, ","), "event") {
		go server.EventServer(config.Config.Events.Port, config.Config.Events.Security.TLS)
	}

	if len(startServers) == 0 {
		l.Fatal("no servers started")
	}

	// wait for a signal to stop
	select {
	case <-sigChan:
		cleanup()
		cancel()
	case <-ctx.Done():
	}
}
