package config

import (
	"awesomeProject/internal/interfaces"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Loader struct {
	validator   *Validator
	configPaths []string
}

func NewLoader() *Loader {
	return &Loader{
		validator: NewValidator(),
		configPaths: []string{
			"config.yaml",
			"config.yml",
			"config.json",
			"./config/config.yaml",
			"./config/config.yml",
			"./config/config.json",
			"/etc/alert-agent/config.yaml",
			"/etc/alert-agent/config.yml",
			"/etc/alert-agent/config.json",
		},
	}
}

func (l *Loader) Load() (*interfaces.Config, error) {
	config := &interfaces.Config{}

	SetDefaults(config)

	if err := l.loadFromFile(config); err != nil {
		return nil, fmt.Errorf("loading from file: %w", err)
	}

	l.loadFromEnv(config)
	l.loadFromFlags(config)

	if err := l.validator.Validate(config); err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}

	return config, nil
}

func (l *Loader) Validate(config *interfaces.Config) error {
	return l.validator.Validate(config)
}

func (l *Loader) Watch(ctx context.Context) (<-chan *interfaces.Config, error) {
	ch := make(chan *interfaces.Config, 1)

	go func() {
		defer close(ch)
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		var lastModTime time.Time
		configFile := l.findConfigFile()
		if configFile != "" {
			if stat, err := os.Stat(configFile); err == nil {
				lastModTime = stat.ModTime()
			}
		}

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if configFile == "" {
					continue
				}

				stat, err := os.Stat(configFile)
				if err != nil {
					continue
				}

				if stat.ModTime().After(lastModTime) {
					lastModTime = stat.ModTime()
					if newConfig, err := l.Load(); err == nil {
						select {
						case ch <- newConfig:
						case <-ctx.Done():
							return
						}
					}
				}
			}
		}
	}()

	return ch, nil
}

func (l *Loader) loadFromFile(config *interfaces.Config) error {
	configFile := l.findConfigFile()
	if configFile == "" {
		return nil
	}

	data, err := os.ReadFile(configFile)
	if err != nil {
		return fmt.Errorf("reading config file %s: %w", configFile, err)
	}

	ext := filepath.Ext(configFile)
	switch ext {
	case ".yaml", ".yml":
		if err := yaml.Unmarshal(data, config); err != nil {
			return fmt.Errorf("parsing YAML config: %w", err)
		}
	case ".json":
		if err := json.Unmarshal(data, config); err != nil {
			return fmt.Errorf("parsing JSON config: %w", err)
		}
	default:
		return fmt.Errorf("unsupported config file format: %s", ext)
	}

	return nil
}

func (l *Loader) findConfigFile() string {
	for _, path := range l.configPaths {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}

func (l *Loader) loadFromEnv(config *interfaces.Config) {
	if val := os.Getenv("ALERT_AGENT_HEALTH_PORT"); val != "" {
		if port := parseInt(val); port > 0 {
			config.Server.HealthPort = port
		}
	}

	if val := os.Getenv("ALERT_AGENT_LOG_LEVEL"); val != "" {
		config.Server.LogLevel = val
	}

	if val := os.Getenv("ALERT_AGENT_DOCKER_HOST"); val != "" {
		config.Docker.Host = val
	}

	if val := os.Getenv("ALERT_AGENT_DOCKER_TIMEOUT"); val != "" {
		if duration, err := time.ParseDuration(val); err == nil {
			config.Docker.Timeout = duration
		}
	}

	if val := os.Getenv("ALERT_AGENT_DOCKER_RETRY_INTERVAL"); val != "" {
		if duration, err := time.ParseDuration(val); err == nil {
			config.Docker.RetryInterval = duration
		}
	}

	if val := os.Getenv("ALERT_AGENT_CONTAINER_FILTERS"); val != "" {
		config.Monitoring.ContainerFilters = strings.Split(val, ",")
		for i := range config.Monitoring.ContainerFilters {
			config.Monitoring.ContainerFilters[i] = strings.TrimSpace(config.Monitoring.ContainerFilters[i])
		}
	}

	if val := os.Getenv("ALERT_AGENT_LOG_LEVELS"); val != "" {
		config.Monitoring.LogLevels = strings.Split(val, ",")
		for i := range config.Monitoring.LogLevels {
			config.Monitoring.LogLevels[i] = strings.TrimSpace(config.Monitoring.LogLevels[i])
		}
	}

	if val := os.Getenv("ALERT_AGENT_CHECK_INTERVAL"); val != "" {
		if duration, err := time.ParseDuration(val); err == nil {
			config.Monitoring.CheckInterval = duration
		}
	}

	if val := os.Getenv("ALERT_AGENT_BUFFER_SIZE"); val != "" {
		if size := parseInt(val); size > 0 {
			config.Monitoring.BufferSize = size
		}
	}

	if val := os.Getenv("ALERT_AGENT_RATE_LIMIT_PER_CONTAINER"); val != "" {
		if limit := parseInt(val); limit > 0 {
			config.Alerts.RateLimit.PerContainer = limit
		}
	}

	if val := os.Getenv("ALERT_AGENT_RATE_LIMIT_WINDOW"); val != "" {
		if duration, err := time.ParseDuration(val); err == nil {
			config.Alerts.RateLimit.Window = duration
		}
	}

	if val := os.Getenv("ALERT_AGENT_RATE_LIMIT_BURST"); val != "" {
		if burst := parseInt(val); burst > 0 {
			config.Alerts.RateLimit.Burst = burst
		}
	}
}

func (l *Loader) loadFromFlags(config *interfaces.Config) {
	var (
		healthPort = flag.Int("health-port", config.Server.HealthPort, "Health check port")
		logLevel   = flag.String("log-level", config.Server.LogLevel, "Log level")
		dockerHost = flag.String("docker-host", config.Docker.Host, "Docker host")
		configFile = flag.String("config", "", "Config file path")
	)

	if !flag.Parsed() {
		flag.Parse()
	}

	if *healthPort != config.Server.HealthPort {
		config.Server.HealthPort = *healthPort
	}

	if *logLevel != config.Server.LogLevel {
		config.Server.LogLevel = *logLevel
	}

	if *dockerHost != config.Docker.Host {
		config.Docker.Host = *dockerHost
	}

	if *configFile != "" {
		l.configPaths = []string{*configFile}
	}
}

func parseInt(s string) int {
	var result int
	fmt.Sscanf(s, "%d", &result)
	return result
}
