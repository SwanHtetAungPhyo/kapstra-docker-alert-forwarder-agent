package config

import (
	"awesomeProject/internal/interfaces"
	"fmt"
	"time"
)

type Validator struct{}

func NewValidator() *Validator {
	return &Validator{}
}

func (v *Validator) Validate(config *interfaces.Config) error {
	if err := v.validateServer(&config.Server); err != nil {
		return fmt.Errorf("server config: %w", err)
	}

	if err := v.validateDocker(&config.Docker); err != nil {
		return fmt.Errorf("docker config: %w", err)
	}

	if err := v.validateMonitoring(&config.Monitoring); err != nil {
		return fmt.Errorf("monitoring config: %w", err)
	}

	if err := v.validateAlerts(&config.Alerts); err != nil {
		return fmt.Errorf("alerts config: %w", err)
	}

	return nil
}

func (v *Validator) validateServer(config *interfaces.ServerConfig) error {
	if config.HealthPort <= 0 || config.HealthPort > 65535 {
		return fmt.Errorf("health_port must be between 1 and 65535")
	}

	validLevels := map[string]bool{
		"debug": true, "info": true, "warn": true, "error": true, "fatal": true,
	}
	if !validLevels[config.LogLevel] {
		return fmt.Errorf("log_level must be one of: debug, info, warn, error, fatal")
	}

	return nil
}

func (v *Validator) validateDocker(config *interfaces.DockerConfig) error {
	if config.Timeout <= 0 {
		return fmt.Errorf("timeout must be positive")
	}

	if config.RetryInterval <= 0 {
		return fmt.Errorf("retry_interval must be positive")
	}

	return nil
}

func (v *Validator) validateMonitoring(config *interfaces.MonitoringConfig) error {
	if config.CheckInterval <= 0 {
		return fmt.Errorf("check_interval must be positive")
	}

	if config.BufferSize <= 0 {
		return fmt.Errorf("buffer_size must be positive")
	}

	validLevels := map[string]bool{
		"INFO": true, "WARN": true, "ERROR": true, "FATAL": true, "PANIC": true,
	}
	for _, level := range config.LogLevels {
		if !validLevels[level] {
			return fmt.Errorf("invalid log level: %s", level)
		}
	}

	return nil
}

func (v *Validator) validateAlerts(config *interfaces.AlertConfig) error {
	if config.RateLimit.PerContainer <= 0 {
		return fmt.Errorf("rate_limit.per_container must be positive")
	}

	if config.RateLimit.Window <= 0 {
		return fmt.Errorf("rate_limit.window must be positive")
	}

	if config.RateLimit.Burst <= 0 {
		return fmt.Errorf("rate_limit.burst must be positive")
	}

	for i, dest := range config.Destinations {
		if err := v.validateDestination(&dest); err != nil {
			return fmt.Errorf("destination[%d]: %w", i, err)
		}
	}

	return nil
}

func (v *Validator) validateDestination(dest *interfaces.Destination) error {
	if dest.Name == "" {
		return fmt.Errorf("name is required")
	}

	validTypes := map[string]bool{
		"webhook": true, "slack": true, "email": true,
	}
	if !validTypes[dest.Type] {
		return fmt.Errorf("invalid type: %s", dest.Type)
	}

	switch dest.Type {
	case "webhook":
		return v.validateWebhookConfig(dest.Config)
	case "slack":
		return v.validateSlackConfig(dest.Config)
	case "email":
		return v.validateEmailConfig(dest.Config)
	}

	return nil
}

func (v *Validator) validateWebhookConfig(config map[string]interface{}) error {
	url, ok := config["url"].(string)
	if !ok || url == "" {
		return fmt.Errorf("webhook url is required")
	}

	if len(url) < 8 || (url[:7] != "http://" && url[:8] != "https://") {
		return fmt.Errorf("webhook url must be a valid HTTP/HTTPS URL")
	}

	if timeout, ok := config["timeout"]; ok {
		if timeoutStr, ok := timeout.(string); ok {
			if _, err := time.ParseDuration(timeoutStr); err != nil {
				return fmt.Errorf("invalid timeout format: %w", err)
			}
		}
	}

	return nil
}

func (v *Validator) validateSlackConfig(config map[string]interface{}) error {
	webhookURL, ok := config["webhook_url"].(string)
	if !ok || webhookURL == "" {
		return fmt.Errorf("slack webhook_url is required")
	}

	if len(webhookURL) < 8 || webhookURL[:8] != "https://" {
		return fmt.Errorf("slack webhook_url must be HTTPS")
	}

	return nil
}

func (v *Validator) validateEmailConfig(config map[string]interface{}) error {
	smtp, ok := config["smtp_host"].(string)
	if !ok || smtp == "" {
		return fmt.Errorf("email smtp_host is required")
	}

	from, ok := config["from"].(string)
	if !ok || from == "" {
		return fmt.Errorf("email from address is required")
	}

	to, ok := config["to"]
	if !ok {
		return fmt.Errorf("email to addresses are required")
	}

	switch toVal := to.(type) {
	case string:
		if toVal == "" {
			return fmt.Errorf("email to address cannot be empty")
		}
	case []interface{}:
		if len(toVal) == 0 {
			return fmt.Errorf("email to addresses list cannot be empty")
		}
	default:
		return fmt.Errorf("email to must be string or array")
	}

	return nil
}

func SetDefaults(config *interfaces.Config) {
	if config.Server.HealthPort == 0 {
		config.Server.HealthPort = 8080
	}
	if config.Server.LogLevel == "" {
		config.Server.LogLevel = "info"
	}

	if config.Docker.Host == "" {
		config.Docker.Host = "unix:///var/run/docker.sock"
	}
	if config.Docker.Timeout == 0 {
		config.Docker.Timeout = 30 * time.Second
	}
	if config.Docker.RetryInterval == 0 {
		config.Docker.RetryInterval = 30 * time.Second
	}

	if config.Monitoring.CheckInterval == 0 {
		config.Monitoring.CheckInterval = 30 * time.Second
	}
	if config.Monitoring.BufferSize == 0 {
		config.Monitoring.BufferSize = 1000
	}
	if len(config.Monitoring.LogLevels) == 0 {
		config.Monitoring.LogLevels = []string{"ERROR", "FATAL", "PANIC", "WARN"}
	}

	if config.Alerts.RateLimit.PerContainer == 0 {
		config.Alerts.RateLimit.PerContainer = 10
	}
	if config.Alerts.RateLimit.Window == 0 {
		config.Alerts.RateLimit.Window = 5 * time.Minute
	}
	if config.Alerts.RateLimit.Burst == 0 {
		config.Alerts.RateLimit.Burst = 3
	}

	if config.Logging.Level == "" {
		config.Logging.Level = "info"
	}
	if config.Logging.Format == "" {
		config.Logging.Format = "json"
	}
}
