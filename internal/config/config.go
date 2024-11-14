package config

import (
	"encoding/json"
	"os"

	"github.com/kelseyhightower/envconfig"
	"github.com/robertlestak/vault-secret-sync/api/v1alpha1"
	"github.com/robertlestak/vault-secret-sync/internal/backend"
	"github.com/robertlestak/vault-secret-sync/internal/queue"
	"github.com/robertlestak/vault-secret-sync/internal/srvutils"
	log "github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"
)

var (
	Config ConfigFile
)

type EventServer struct {
	Enabled  *bool           `json:"enabled" yaml:"enabled"`
	Port     int             `json:"port" yaml:"port"`
	Security *ServerSecurity `json:"security" yaml:"security"`
	Dedupe   *bool           `json:"dedupe" yaml:"dedupe"`
}

type QueueConfig struct {
	Type   queue.QueueType `json:"type" yaml:"type"`
	Params map[string]any  `json:"params" yaml:"params"`
}

type BackendConfig struct {
	Type   backend.BackendType `json:"type" yaml:"type"`
	Params map[string]any      `json:"params" yaml:"params"`
}

type OperatorConfig struct {
	Enabled          *bool          `json:"enabled" yaml:"enabled"`
	Backend          *BackendConfig `json:"backend" yaml:"backend"`
	WorkerPoolSize   int            `json:"workerPoolSize" yaml:"workerPoolSize"`
	NumSubscriptions int            `json:"numSubscriptions" yaml:"numSubscriptions"`
}

type EmailNotificationConfig struct {
	Host               string `json:"host" yaml:"host"`
	Port               int    `json:"port" yaml:"port"`
	Username           string `json:"username" yaml:"username"`
	Password           string `json:"password" yaml:"password"`
	From               string `json:"from" yaml:"from"`
	To                 string `json:"to" yaml:"to"`
	Subject            string `json:"subject" yaml:"subject"`
	Body               string `json:"body" yaml:"body"`
	InsecureSkipVerify bool   `json:"insecureSkipVerify" yaml:"insecureSkipVerify"`
}

type SlackNotificationConfig struct {
	URL     string `json:"url" yaml:"url"`
	Message string `json:"message" yaml:"message"`
}

type WebhookNotificationConfig struct {
	URL         string            `json:"url" yaml:"url"`
	Method      string            `json:"method" yaml:"method"`
	Headers     map[string]string `json:"headers" yaml:"headers"`
	Body        string            `json:"body" yaml:"body"`
	ExcludeBody bool              `json:"excludeBody" yaml:"excludeBody"`
}

type NotificationsConfig struct {
	Email   *EmailNotificationConfig   `json:"email" yaml:"email"`
	Slack   *SlackNotificationConfig   `json:"slack" yaml:"slack"`
	Webhook *WebhookNotificationConfig `json:"webhook" yaml:"webhook"`
}

type ServerSecurity struct {
	Enabled *bool               `json:"enabled" yaml:"enabled"`
	Token   string              `json:"token" yaml:"token"`
	TLS     *srvutils.TLSConfig `json:"tls" yaml:"tls"`
}

type MetricsServer struct {
	Port     int             `json:"port" yaml:"port"`
	Security *ServerSecurity `json:"security" yaml:"security"`
}

type LogConfig struct {
	Level  string `json:"level" yaml:"level"`
	Format string `json:"format" yaml:"format"`
	Events bool   `json:"events" yaml:"events"`
}

type ConfigFile struct {
	Log           *LogConfig            `json:"log" yaml:"log"`
	Events        *EventServer          `json:"events" yaml:"events"`
	Operator      *OperatorConfig       `json:"operator" yaml:"operator"`
	Stores        *v1alpha1.StoreConfig `json:"stores" yaml:"stores"`
	Queue         *QueueConfig          `json:"queue" yaml:"queue"`
	Metrics       *MetricsServer        `json:"metrics" yaml:"metrics"`
	Notifications *NotificationsConfig  `json:"notifications" yaml:"notifications"`
}

func LoadFile(f string) error {
	l := log.WithFields(log.Fields{
		"action": "LoadFile",
		"file":   f,
		"pkg":    "config",
	})
	l.Trace("start")
	defer l.Trace("end")
	cfp := f
	// if the file path doesn't exist, try default at /config/config.yaml
	if _, err := os.Stat(cfp); os.IsNotExist(err) {
		cfp = "/config/config.yaml"
	}
	// if file doesn't exit, fall back to other methods
	if _, err := os.Stat(f); os.IsNotExist(err) {
		l.Debugf("config file not found: %s", f)
		if err := Config.SetFromEnv(); err != nil {
			return err
		}
		if err := Config.SetDefaults(); err != nil {
			return err
		}
		return nil
	}
	fd, err := os.Open(f)
	if err != nil {
		return err
	}
	defer fd.Close()
	// try to unmarshal the file as yaml, if that fails, try as json
	yd := yaml.NewDecoder(fd)
	if err := yd.Decode(&Config); err != nil {
		jd := json.NewDecoder(fd)
		if err := jd.Decode(&Config); err != nil {
			return err
		}
	}
	if err := Config.SetFromEnv(); err != nil {
		return err
	}
	if err := Config.SetDefaults(); err != nil {
		return err
	}
	// marshall it back to yaml for logging
	jd, err := yaml.Marshal(Config)
	if err != nil {
		return err
	}
	l.Debugf("config from file: %s", string(jd))
	return nil
}

func (c *ConfigFile) SetFromEnv() error {
	l := log.WithFields(log.Fields{
		"action": "SetFromEnv",
		"pkg":    "config",
	})
	l.Trace("start")
	defer l.Trace("end")
	err := envconfig.Process("vss", c)
	if err != nil {
		l.Error(err)
		return err
	}
	l.Debugf("config from env: %+v", c)
	return nil
}

func (c *ConfigFile) SetDefaults() error {
	l := log.WithFields(log.Fields{
		"action": "SetDefaults",
		"pkg":    "config",
	})
	l.Trace("start")
	defer l.Trace("end")
	if c.Log == nil {
		c.Log = &LogConfig{}
	}
	if c.Log.Level == "" {
		c.Log.Level = "info"
	}
	if c.Log.Format == "" {
		c.Log.Format = "text"
	}
	if c.Operator == nil || c.Operator.Backend == nil {
		c.Operator = &OperatorConfig{
			Backend: &BackendConfig{
				Type: backend.BackendTypeKubernetes,
			},
		}
	}
	if c.Operator.Backend == nil || c.Operator.Backend.Type == "" {
		c.Operator.Backend = &BackendConfig{
			Type: backend.BackendTypeKubernetes,
		}
	}
	if c.Queue == nil || c.Queue.Type == "" {
		c.Queue = &QueueConfig{
			Type: queue.QueueTypeMemory,
		}
	}
	return nil
}
