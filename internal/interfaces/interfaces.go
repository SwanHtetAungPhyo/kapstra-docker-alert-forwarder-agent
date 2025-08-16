package interfaces

import (
	"awesomeProject/internal/types"
	"context"
	"time"
)

type AlertProcessor interface {
	Process(ctx context.Context, logEntry types.LogEntry) error
}

type NotificationHandler interface {
	Send(ctx context.Context, alert types.Alert) error
	Name() string
	Validate(config map[string]interface{}) error
}

type ConfigManager interface {
	Load() (*Config, error)
	Validate(config *Config) error
	Watch(ctx context.Context) (<-chan *Config, error)
}

type LogProcessor interface {
	ProcessStream(ctx context.Context, containerID, containerName string, stream <-chan string) error
	SetPatterns(patterns map[types.LogLevel]string) error
}

type EventMonitor interface {
	Start(ctx context.Context) error
	Subscribe() <-chan ContainerEvent
}

type NotificationRouter interface {
	Route(ctx context.Context, alert types.Alert) error
	RegisterHandler(name string, handler NotificationHandler) error
}

type HealthChecker interface {
	Check(ctx context.Context) types.HealthStatus
	RegisterComponent(name string, checker func() types.ComponentHealth)
}

type MetricsCollector interface {
	IncrementCounter(name string, labels map[string]string)
	RecordDuration(name string, duration time.Duration, labels map[string]string)
	SetGauge(name string, value float64, labels map[string]string)
}

type ContainerEvent struct {
	Type        string
	ContainerID string
	Name        string
	Labels      map[string]string
	Timestamp   time.Time
}

type Config struct {
	Server     ServerConfig     `yaml:"server"`
	Docker     DockerConfig     `yaml:"docker"`
	Monitoring MonitoringConfig `yaml:"monitoring"`
	Alerts     AlertConfig      `yaml:"alerts"`
	Logging    LoggingConfig    `yaml:"logging"`
	Token      string           `yaml:"token"`
}

type ServerConfig struct {
	HealthPort int    `yaml:"health_port" default:"8080"`
	LogLevel   string `yaml:"log_level" default:"info"`
}

type DockerConfig struct {
	Host          string        `yaml:"host"`
	APIVersion    string        `yaml:"api_version"`
	Timeout       time.Duration `yaml:"timeout" default:"30s"`
	RetryInterval time.Duration `yaml:"retry_interval" default:"30s"`
}

type MonitoringConfig struct {
	ContainerFilters []string      `yaml:"container_filters"`
	LogLevels        []string      `yaml:"log_levels"`
	CheckInterval    time.Duration `yaml:"check_interval" default:"30s"`
	BufferSize       int           `yaml:"buffer_size" default:"1000"`
}

type AlertConfig struct {
	Destinations []Destination `yaml:"destinations"`
	RateLimit    RateLimit     `yaml:"rate_limit"`
	Templates    Templates     `yaml:"templates"`
}

type LoggingConfig struct {
	Level  string `yaml:"level" default:"info"`
	Format string `yaml:"format" default:"json"`
}

type Destination struct {
	Name    string                 `yaml:"name"`
	Type    string                 `yaml:"type"`
	Config  map[string]interface{} `yaml:"config"`
	Filters []Filter               `yaml:"filters"`
}

type Filter struct {
	Severity []string `yaml:"severity"`
}

type RateLimit struct {
	PerContainer int           `yaml:"per_container" default:"10"`
	Window       time.Duration `yaml:"window" default:"5m"`
	Burst        int           `yaml:"burst" default:"3"`
}

type Templates struct {
	Default string            `yaml:"default"`
	Custom  map[string]string `yaml:"custom"`
}

type RetryPolicy struct {
	MaxAttempts   int           `yaml:"max_attempts" default:"3"`
	InitialDelay  time.Duration `yaml:"initial_delay" default:"1s"`
	MaxDelay      time.Duration `yaml:"max_delay" default:"60s"`
	BackoffFactor float64       `yaml:"backoff_factor" default:"2.0"`
}
