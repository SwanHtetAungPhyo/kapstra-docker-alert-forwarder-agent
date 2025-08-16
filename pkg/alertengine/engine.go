package alertengine

import (
	"awesomeProject/internal/api"
	"awesomeProject/internal/docker"
	"awesomeProject/internal/interfaces"
	"awesomeProject/internal/processor"
	"awesomeProject/internal/types"
	"awesomeProject/internal/webhook"
	"context"
	"log"
	"strings"
	"sync"
	"time"
)

type Engine struct {
	config        *interfaces.Config
	dockerClient  *docker.Client
	processor     *processor.LogProcessor
	webhookSender *webhook.Sender
	webServer     *api.Server

	monitoredContainers map[string]*types.ContainerMonitor
	mu                  sync.RWMutex
	alertChan           chan types.Alert

	startTime time.Time
	ctx       context.Context
	cancel    context.CancelFunc
}

func New(config *interfaces.Config) (*Engine, error) {
	ctx, cancel := context.WithCancel(context.Background())

	dockerClient, err := docker.NewClient(&config.Docker)
	if err != nil {
		cancel()
		return nil, err
	}

	alertChan := make(chan types.Alert, config.Monitoring.BufferSize)

	engine := &Engine{
		config:              config,
		dockerClient:        dockerClient,
		processor:           processor.New(config.Monitoring.BufferSize, alertChan),
		webhookSender:       webhook.NewSender(),
		monitoredContainers: make(map[string]*types.ContainerMonitor),
		alertChan:           alertChan,
		startTime:           time.Now(),
		ctx:                 ctx,
		cancel:              cancel,
	}

	engine.webServer = api.NewServer(config, engine)

	return engine, nil
}

func (e *Engine) GetLogStream(ctx context.Context, containerID string) (*types.LogStream, error) {
	return e.dockerClient.GetContainerLogs(ctx, containerID)
}
func (e *Engine) Start(ctx context.Context) error {
	log.Println("Starting alert engine")

	go e.handleAlerts()
	go func() {
		err := e.webServer.Start(ctx)
		if err != nil {
			log.Println(err.Error())
		}
	}()
	go e.handleContainerEvents()

	if err := e.scanExistingContainers(); err != nil {
		log.Printf("Error scanning existing containers: %v", err)
	}

	ticker := time.NewTicker(e.config.Monitoring.CheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return e.shutdown()
		case <-ticker.C:
			if err := e.scanExistingContainers(); err != nil {
				log.Printf("Error during periodic scan: %v", err)
			}
		}
	}
}

func (e *Engine) handleAlerts() {
	for alert := range e.alertChan {
		log.Printf("🚨 ALERT DETECTED: %s in container %s", alert.Severity.String(), alert.Source.ContainerName)
		log.Printf("   Message: %s", alert.Message)

		e.webServer.AddAlert(alert)

		if !e.shouldAlert(alert) {
			log.Printf("   Alert filtered out by log level configuration")
			continue
		}

		for _, dest := range e.config.Alerts.Destinations {
			if dest.Type == "webhook" {
				if url, ok := dest.Config["url"].(string); ok {
					log.Printf("   Attempting to send webhook to: %s", url)
					if err := e.webhookSender.Send(e.ctx, url, alert); err != nil {
						log.Printf("   ❌ Failed to send webhook: %v", err)
					} else {
						log.Printf("   ✅ Alert sent successfully via webhook")
					}
				}
			}
		}
	}
}

func (e *Engine) handleContainerEvents() {
	events, err := e.dockerClient.WatchEvents(e.ctx)
	if err != nil {
		log.Printf("Failed to watch Docker events: %v", err)
		return
	}

	for event := range events {
		switch event.Type {
		case "start":
			if e.matchesFilter(event.Name, event.ContainerID, event.Labels) {
				e.startContainerMonitoring(event.ContainerID, event.Name, event.Labels)
			}
		case "stop", "die", "kill":
			e.stopContainerMonitoring(event.ContainerID)
		case "restart":
			e.stopContainerMonitoring(event.ContainerID)
			time.Sleep(2 * time.Second)
			if e.matchesFilter(event.Name, event.ContainerID, event.Labels) {
				e.startContainerMonitoring(event.ContainerID, event.Name, event.Labels)
			}
		}
	}
}

func (e *Engine) scanExistingContainers() error {
	containers, err := e.dockerClient.ListContainers(e.ctx)
	if err != nil {
		return err
	}

	for _, container := range containers {
		if container.State != "running" {
			continue
		}

		if !e.matchesFilter(container.Name, container.ID, container.Labels) {
			continue
		}

		e.mu.RLock()
		_, exists := e.monitoredContainers[container.ID]
		e.mu.RUnlock()

		if !exists {
			e.startContainerMonitoring(container.ID, container.Name, container.Labels)
		}
	}

	return nil
}

func (e *Engine) startContainerMonitoring(containerID, name string, labels map[string]string) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if _, exists := e.monitoredContainers[containerID]; exists {
		return
	}

	ctx, cancel := context.WithCancel(e.ctx)
	monitor := &types.ContainerMonitor{
		ID:       containerID,
		Name:     name,
		Labels:   labels,
		Cancel:   cancel,
		LastSeen: time.Now(),
		Stats:    types.MonitorStats{},
	}

	e.monitoredContainers[containerID] = monitor

	go func() {
		defer func() {
			e.mu.Lock()
			delete(e.monitoredContainers, containerID)
			e.mu.Unlock()
		}()

		logStream, err := e.dockerClient.GetContainerLogs(ctx, containerID)
		if err != nil {
			log.Printf("Failed to get logs for container %s: %v", name, err)
			return
		}

		if err := e.processor.ProcessStream(ctx, containerID, name, logStream.Reader); err != nil {
			log.Printf("Log processing stopped for container %s: %v", name, err)
		}
	}()

	log.Printf("Started monitoring container: %s (%s)", name, containerID[:12])
}

func (e *Engine) stopContainerMonitoring(containerID string) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if monitor, exists := e.monitoredContainers[containerID]; exists {
		monitor.Cancel()
		delete(e.monitoredContainers, containerID)
		log.Printf("Stopped monitoring container: %s", containerID[:12])
	}
}

func (e *Engine) matchesFilter(containerName, containerID string, labels map[string]string) bool {
	if len(e.config.Monitoring.ContainerFilters) == 0 {
		return true
	}

	for _, filter := range e.config.Monitoring.ContainerFilters {
		if strings.HasPrefix(filter, "label:") {
			labelFilter := strings.TrimPrefix(filter, "label:")
			parts := strings.SplitN(labelFilter, "=", 2)
			if len(parts) == 2 {
				if labels[parts[0]] == parts[1] {
					return true
				}
			} else {
				if _, exists := labels[parts[0]]; exists {
					return true
				}
			}
		} else if strings.HasPrefix(filter, "name:") {
			nameFilter := strings.TrimPrefix(filter, "name:")
			if strings.Contains(containerName, nameFilter) {
				return true
			}
		} else {
			if strings.Contains(containerName, filter) || strings.Contains(containerID, filter) {
				return true
			}
		}
	}

	return false
}

func (e *Engine) shouldAlert(alert types.Alert) bool {
	if len(e.config.Monitoring.LogLevels) == 0 {
		return true
	}

	alertLevel := e.mapSeverityToLogLevel(alert.Severity)
	for _, level := range e.config.Monitoring.LogLevels {
		if strings.ToUpper(level) == alertLevel {
			return true
		}
	}

	return false
}

func (e *Engine) mapSeverityToLogLevel(severity types.Severity) string {
	switch severity {
	case types.SeverityCritical:
		return "FATAL"
	case types.SeverityHigh:
		return "ERROR"
	case types.SeverityMedium:
		return "WARN"
	case types.SeverityLow:
		return "INFO"
	default:
		return "UNKNOWN"
	}
}

func (e *Engine) shutdown() error {
	log.Println("Shutting down alert engine")

	e.mu.Lock()
	for _, monitor := range e.monitoredContainers {
		monitor.Cancel()
	}
	e.monitoredContainers = make(map[string]*types.ContainerMonitor)
	e.mu.Unlock()

	close(e.alertChan)
	e.cancel()

	if e.dockerClient != nil {
		e.dockerClient.Close()
	}

	if e.webhookSender != nil {
		e.webhookSender.Close()
	}

	return nil
}

func (e *Engine) GetMonitoredContainers() map[string]*types.ContainerMonitor {
	e.mu.RLock()
	defer e.mu.RUnlock()

	result := make(map[string]*types.ContainerMonitor)
	for k, v := range e.monitoredContainers {
		result[k] = v
	}
	return result
}
