package types

import (
	"context"
	"io"
	"time"
)

type LogLevel int

const (
	LogLevelUnknown LogLevel = iota
	LogLevelInfo
	LogLevelWarn
	LogLevelError
	LogLevelFatal
	LogLevelPanic
)

func (l LogLevel) String() string {
	switch l {
	case LogLevelInfo:
		return "INFO"
	case LogLevelWarn:
		return "WARN"
	case LogLevelError:
		return "ERROR"
	case LogLevelFatal:
		return "FATAL"
	case LogLevelPanic:
		return "PANIC"
	default:
		return "UNKNOWN"
	}
}

type Severity int

const (
	SeverityLow Severity = iota
	SeverityMedium
	SeverityHigh
	SeverityCritical
)

func (s Severity) String() string {
	switch s {
	case SeverityLow:
		return "LOW"
	case SeverityMedium:
		return "MEDIUM"
	case SeverityHigh:
		return "HIGH"
	case SeverityCritical:
		return "CRITICAL"
	default:
		return "UNKNOWN"
	}
}

type LogEntry struct {
	Timestamp     time.Time
	ContainerID   string
	ContainerName string
	Level         LogLevel
	Message       string
	Raw           string
	Labels        map[string]string
}

type Alert struct {
	ID           string
	Timestamp    time.Time
	Severity     Severity
	Source       Source
	Message      string
	Metadata     map[string]interface{}
	Destinations []string
}

type Source struct {
	ContainerID   string
	ContainerName string
	Labels        map[string]string
}

type ContainerMonitor struct {
	ID       string
	Name     string
	Labels   map[string]string
	Cancel   context.CancelFunc
	LastSeen time.Time
	Stats    MonitorStats
}

type MonitorStats struct {
	LogsProcessed uint64
	AlertsSent    uint64
	Errors        uint64
	LastActivity  time.Time
}

type State int

const (
	StateClosed State = iota
	StateOpen
	StateHalfOpen
)

type CircuitBreaker struct {
	State        State
	FailureCount int
	LastFailure  time.Time
	NextAttempt  time.Time
	SuccessCount int
}

type ComponentHealth struct {
	Status    string    `json:"status"`
	Message   string    `json:"message,omitempty"`
	LastCheck time.Time `json:"last_check"`
}

type HealthStatus struct {
	Overall    string                     `json:"overall"`
	Components map[string]ComponentHealth `json:"components"`
	Uptime     time.Duration              `json:"uptime"`
	Version    string                     `json:"version"`
}

type ContainerInfo struct {
	ID     string
	Name   string
	Labels map[string]string
	State  string
}

type LogStream struct {
	Reader io.ReadCloser
}

type ContainerEvent struct {
	Type        string
	ContainerID string
	Name        string
	Labels      map[string]string
	Timestamp   time.Time
}
