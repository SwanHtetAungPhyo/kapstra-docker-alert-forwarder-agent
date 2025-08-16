package monitor

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
)

type Config struct {
	WebhookURL      string
	LogLevels       []string
	ContainerFilter []string
	CheckInterval   int
	RestartDelay    int
}

type Alert struct {
	Timestamp     string `json:"timestamp"`
	ContainerID   string `json:"container_id"`
	ContainerName string `json:"container_name"`
	Level         string `json:"level"`
	Message       string `json:"message"`
	LogLine       string `json:"log_line"`
}

type LogMonitor struct {
	client              *client.Client
	config              Config
	errorPattern        *regexp.Regexp
	warnPattern         *regexp.Regexp
	fatalPattern        *regexp.Regexp
	panicPattern        *regexp.Regexp
	monitoredContainers map[string]context.CancelFunc
}

func NewLogMonitor(config Config) (*LogMonitor, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("failed to create Docker client: %w", err)
	}
	return &LogMonitor{
		client:              cli,
		config:              config,
		errorPattern:        regexp.MustCompile(`(?i)\b(error|err|exception|fail|failed)\b`),
		warnPattern:         regexp.MustCompile(`(?i)\b(warn|warning)\b`),
		fatalPattern:        regexp.MustCompile(`(?i)\b(fatal|critical)\b`),
		panicPattern:        regexp.MustCompile(`(?i)\b(panic|emergency)\b`),
		monitoredContainers: make(map[string]context.CancelFunc),
	}, nil
}

func (lm *LogMonitor) matchesFilter(containerName, containerID string) bool {
	if len(lm.config.ContainerFilter) == 0 {
		return true
	}
	for _, filter := range lm.config.ContainerFilter {
		if strings.Contains(containerName, filter) || strings.Contains(containerID, filter) {
			return true
		}
	}
	return false
}
func logMatchesLevel(logLine string, levels []string) bool {
	logLineLower := strings.ToLower(logLine)
	for _, level := range levels {
		if strings.Contains(logLineLower, strings.ToLower(level)) {
			return true
		}
	}
	if strings.Contains(logLineLower, "404") ||
		strings.Contains(logLineLower, "500") ||
		strings.Contains(logLineLower, "502") ||
		strings.Contains(logLineLower, "503") {
		return true
	}
	return false
}

func (lm *LogMonitor) detectLogLevel(logLine string) string {
	line := strings.ToLower(logLine)
	if lm.panicPattern.MatchString(line) {
		return "PANIC"
	}
	if lm.fatalPattern.MatchString(line) {
		return "FATAL"
	}
	if lm.errorPattern.MatchString(line) {
		return "ERROR"
	}
	if lm.warnPattern.MatchString(line) {
		return "WARN"
	}
	return ""
}

func (lm *LogMonitor) shouldAlert(level string) bool {
	if len(lm.config.LogLevels) == 0 {
		return level == "ERROR" || level == "FATAL" || level == "PANIC"
	}
	for _, configLevel := range lm.config.LogLevels {
		if strings.ToUpper(configLevel) == level {
			return true
		}
	}
	return false
}

func (lm *LogMonitor) sendWebhook(alert Alert) error {
	payload, err := json.Marshal(alert)
	if err != nil {
		return fmt.Errorf("failed to marshal alert: %w", err)
	}
	resp, err := http.Post(lm.config.WebhookURL, "application/json", bytes.NewBuffer(payload))
	if err != nil {
		return fmt.Errorf("failed to send webhook: %w", err)
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)
	if resp.StatusCode >= 400 {
		return fmt.Errorf("webhook returned status %d", resp.StatusCode)
	}
	return nil
}

func (lm *LogMonitor) monitorContainerLogs(ctx context.Context, containerID, containerName string) {
	log.Printf("Starting log monitoring for container: %s (%s)", containerName, containerID[:12])

	options := container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
		Timestamps: true,
		Since:      time.Now().Format(time.RFC3339),
	}

	logs, err := lm.client.ContainerLogs(ctx, containerID, options)
	if err != nil {
		log.Printf("Failed to get logs for container %s: %v", containerID[:12], err)
		return
	}
	defer func(logs io.ReadCloser) {
		_ = logs.Close()
	}(logs)

	scanner := bufio.NewScanner(logs)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			log.Printf("Stopping log monitoring for container: %s", containerName)
			return
		default:
		}
		line := scanner.Text()
		if len(line) > 8 {
			line = line[8:]
		}
		level := lm.detectLogLevel(line)
		if level == "" {
			continue
		}
		if !lm.shouldAlert(level) {
			continue
		}
		alert := Alert{
			Timestamp:     time.Now().Format(time.RFC3339),
			ContainerID:   containerID,
			ContainerName: containerName,
			Level:         level,
			Message:       strings.TrimSpace(line),
			LogLine:       line,
		}
		log.Printf("🚨 %s detected in %s: %s", level, containerName, strings.TrimSpace(line))
		if err := lm.sendWebhook(alert); err != nil {
			log.Printf("Failed to send webhook: %v", err)
		} else {
			log.Printf("✅ Alert sent to webhook")
		}
	}
	if err := scanner.Err(); err != nil && err != io.EOF {
		log.Printf("Error reading logs for container %s: %v", containerID[:12], err)
	}
}

func (lm *LogMonitor) Start(ctx context.Context) error {
	log.Println("🚀 Starting Docker Log Monitor")
	log.Printf("📡 Webhook URL: %s", lm.config.WebhookURL)
	log.Printf("📊 Monitoring log levels: %v", lm.config.LogLevels)
	go lm.monitorDockerEvents(ctx)
	if err := lm.scanAndMonitorContainers(ctx); err != nil {
		return err
	}
	ticker := time.NewTicker(time.Duration(lm.config.CheckInterval) * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if err := lm.scanAndMonitorContainers(ctx); err != nil {
				log.Printf("Error during periodic container scan: %v", err)
			}
		case <-ctx.Done():
			log.Println("Shutting down Docker Log Monitor...")
			lm.stopAllMonitoring()
			return nil
		}
	}
}

func (lm *LogMonitor) scanAndMonitorContainers(ctx context.Context) error {
	containers, err := lm.client.ContainerList(ctx, container.ListOptions{
		Size:    true,
		All:     true,
		Latest:  true,
		Limit:   0,
		Filters: filters.Args{},
	})
	if err != nil {
		return fmt.Errorf("failed to list containers: %w", err)
	}
	for _, c := range containers {
		if len(c.Names) == 0 {
			continue
		}
		containerName := strings.TrimPrefix(c.Names[0], "/")
		if !lm.matchesFilter(containerName, c.ID) {
			continue
		}
		if _, exists := lm.monitoredContainers[c.ID]; exists {
			continue
		}
		lm.startContainerMonitoring(ctx, c.ID, containerName)
	}
	return nil
}

func (lm *LogMonitor) startContainerMonitoring(ctx context.Context, containerID, containerName string) {
	containerCtx, cancel := context.WithCancel(ctx)
	lm.monitoredContainers[containerID] = cancel
	log.Printf("📦 Found new container to monitor: %s (%s)", containerName, containerID[:12])
	go func() {
		defer func() {
			delete(lm.monitoredContainers, containerID)
		}()
		lm.monitorContainerLogs(containerCtx, containerID, containerName)
	}()
}

func (lm *LogMonitor) stopAllMonitoring() {
	for containerID, cancel := range lm.monitoredContainers {
		log.Printf("Stopping monitoring for container: %s", containerID[:12])
		cancel()
	}
	lm.monitoredContainers = make(map[string]context.CancelFunc)
}

func (lm *LogMonitor) monitorDockerEvents(ctx context.Context) {
	log.Println("🔍 Starting Docker events monitoring...")
	filterArgs := filters.NewArgs()
	filterArgs.Add("type", "container")
	eventOptions := events.ListOptions{Filters: filterArgs}
	eventChan, errChan := lm.client.Events(ctx, eventOptions)
	for {
		select {
		case event := <-eventChan:
			lm.handleContainerEvent(ctx, event)
		case err := <-errChan:
			if err != nil {
				log.Printf("Error monitoring Docker events: %v", err)
				time.Sleep(5 * time.Second)
				go lm.monitorDockerEvents(ctx)
				return
			}
		case <-ctx.Done():
			return
		}
	}
}

func (lm *LogMonitor) handleContainerEvent(ctx context.Context, event events.Message) {
	containerID := event.Actor.ID
	containerName := event.Actor.Attributes["name"]
	switch event.Action {
	case "start":
		log.Printf("🆕 Container started: %s (%s)", containerName, containerID[:12])
		if !lm.matchesFilter(containerName, containerID) {
			return
		}
		if _, exists := lm.monitoredContainers[containerID]; !exists {
			time.Sleep(time.Duration(lm.config.RestartDelay) * time.Second)
			lm.startContainerMonitoring(ctx, containerID, containerName)
		}
	case "die", "stop", "kill":
		log.Printf("🛑 Container stopped: %s (%s)", containerName, containerID[:12])
		if cancel, exists := lm.monitoredContainers[containerID]; exists {
			cancel()
			delete(lm.monitoredContainers, containerID)
		}
	case "restart":
		log.Printf("🔄 Container restarted: %s (%s)", containerName, containerID[:12])
		if cancel, exists := lm.monitoredContainers[containerID]; exists {
			cancel()
			delete(lm.monitoredContainers, containerID)
		}
		if !lm.matchesFilter(containerName, containerID) {
			return
		}
		time.Sleep(time.Duration(lm.config.RestartDelay) * time.Second)
		lm.startContainerMonitoring(ctx, containerID, containerName)
	}
}
