package api

import (
	"awesomeProject/internal/interfaces"
	"awesomeProject/internal/types"
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type Server struct {
	engine     EngineInterface
	config     *interfaces.Config
	httpServer *http.Server
	alerts     []AlertResponse
	maxAlerts  int
}

type EngineInterface interface {
	GetMonitoredContainers() map[string]*types.ContainerMonitor
	GetLogStream(ctx context.Context, containerID string) (*types.LogStream, error)
}

type ContainerResponse struct {
	ID     string            `json:"id"`
	Name   string            `json:"name"`
	State  string            `json:"state"`
	Labels map[string]string `json:"labels"`
}

type LogResponse struct {
	Timestamp time.Time `json:"timestamp"`
	Message   string    `json:"message"`
	Level     string    `json:"level"`
}

type AlertResponse struct {
	Timestamp     time.Time `json:"timestamp"`
	ContainerID   string    `json:"container_id"`
	ContainerName string    `json:"container_name"`
	Level         string    `json:"level"`
	Severity      string    `json:"severity"`
	Message       string    `json:"message"`
}

type StatusResponse struct {
	Overall    string                     `json:"overall"`
	Components map[string]ComponentHealth `json:"components"`
	Uptime     string                     `json:"uptime"`
	Version    string                     `json:"version"`
}

type ComponentHealth struct {
	Status    string    `json:"status"`
	Message   string    `json:"message,omitempty"`
	LastCheck time.Time `json:"last_check"`
}

func NewServer(config *interfaces.Config, engine EngineInterface) *Server {
	return &Server{
		engine:    engine,
		config:    config,
		alerts:    make([]AlertResponse, 0),
		maxAlerts: 1000,
	}
}

func (s *Server) Start(ctx context.Context) error {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/containers", s.handleContainers)
	mux.HandleFunc("/api/containers/", s.handleContainerLogs)
	mux.HandleFunc("/api/alerts", s.handleAlerts)
	mux.HandleFunc("/api/status", s.handleStatus)
	mux.HandleFunc("/health", s.handleHealth)

	mux.Handle("/", http.FileServer(http.Dir("./web/build/")))

	s.httpServer = &http.Server{
		Addr:         fmt.Sprintf(":%d", s.config.Server.HealthPort),
		Handler:      s.corsMiddleware(mux),
		ReadTimeout:  30 * time.Second, // Add read timeout
		WriteTimeout: 30 * time.Second, // Add write timeout
		IdleTimeout:  60 * time.Second, // Add idle timeout
	}

	log.Printf("Starting web server on port %d", s.config.Server.HealthPort)

	go func() {
		if err := s.httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("HTTP server error: %v", err)
		}
	}()

	<-ctx.Done()
	return s.httpServer.Shutdown(context.Background())
}

func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleContainers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	containers := s.engine.GetMonitoredContainers()
	response := make([]ContainerResponse, 0, len(containers))

	for _, container := range containers {
		response = append(response, ContainerResponse{
			ID:     container.ID,
			Name:   container.Name,
			State:  "running",
			Labels: container.Labels,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

func (s *Server) handleContainerLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/api/containers/")
	parts := strings.Split(path, "/")
	if len(parts) < 2 || parts[1] != "logs" {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	containerID := parts[0]
	tailStr := r.URL.Query().Get("tail")
	tail := 100
	if tailStr != "" {
		if t, err := strconv.Atoi(tailStr); err == nil {
			tail = t
		}
	}

	// Create timeout context - 20 seconds should be enough for log fetching
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	logs := s.getContainerLogsWithTimeout(ctx, containerID, tail)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(logs)
}

func (s *Server) handleAlerts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(s.alerts)
}

func (s *Server) getContainerLogsWithTimeout(ctx context.Context, containerID string, tail int) []LogResponse {
	log.Printf("Fetching logs for container: %s, tail: %d", containerID, tail)

	// Create a channel to receive results
	resultChan := make(chan []LogResponse, 1)
	errorChan := make(chan error, 1)

	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("Panic in getContainerLogs: %v", r)
				errorChan <- fmt.Errorf("panic: %v", r)
			}
		}()

		logs, err := s.fetchContainerLogsInternal(ctx, containerID, tail)
		if err != nil {
			errorChan <- err
			return
		}
		resultChan <- logs
	}()

	// Wait for either result, error, or timeout
	select {
	case logs := <-resultChan:
		log.Printf("Successfully fetched %d logs for container %s", len(logs), containerID)
		return logs
	case err := <-errorChan:
		log.Printf("Error fetching logs for container %s: %v", containerID, err)
		return make([]LogResponse, 0)
	case <-ctx.Done():
		log.Printf("Timeout fetching logs for container %s", containerID)
		return make([]LogResponse, 0)
	}
}

func (s *Server) fetchContainerLogsInternal(ctx context.Context, containerID string, tail int) ([]LogResponse, error) {
	// Validate container exists first
	containers := s.engine.GetMonitoredContainers()
	if _, exists := containers[containerID]; !exists {
		return nil, fmt.Errorf("container %s not found in monitored containers", containerID)
	}

	logStream, err := s.engine.GetLogStream(ctx, containerID)
	if err != nil {
		return nil, fmt.Errorf("failed to get log stream: %w", err)
	}

	allLogs := make([]LogResponse, 0)
	scanner := bufio.NewScanner(logStream.Reader)

	const maxCapacity = 1024 * 1024 // 1MB buffer
	buf := make([]byte, maxCapacity)
	scanner.Buffer(buf, maxCapacity)

	lineCount := 0
	maxLines := 1000

	readTimeout := time.NewTimer(15 * time.Second)
	defer readTimeout.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Printf("Context cancelled while reading logs for container %s", containerID)
			return allLogs, ctx.Err()
		case <-readTimeout.C:
			log.Printf("Read timeout reached for container %s, returning partial logs", containerID)
			return allLogs, nil
		default:
		}

		if !scanner.Scan() {
			break // End of stream or error
		}

		lineCount++
		if lineCount > maxLines {
			log.Printf("Reached max lines (%d) for container %s, stopping", maxLines, containerID)
			break
		}

		line := scanner.Text()
		if line == "" {
			continue
		}

		timestamp, message := s.parseDockerLogLine(line)
		logEntry := LogResponse{
			Timestamp: timestamp,
			Message:   message,
			Level:     s.extractLogLevel(message),
		}

		allLogs = append(allLogs, logEntry)
	}

	if err := scanner.Err(); err != nil {
		log.Printf("Scanner error for container %s: %v", containerID, err)
		return allLogs, fmt.Errorf("scanner error: %w", err)
	}

	log.Printf("Successfully read %d logs for container %s", len(allLogs), containerID)

	// Return last 'tail' logs
	if tail <= 0 || tail >= len(allLogs) {
		return allLogs, nil
	}

	return allLogs[len(allLogs)-tail:], nil
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	containers := s.engine.GetMonitoredContainers()

	status := StatusResponse{
		Overall: "healthy",
		Components: map[string]ComponentHealth{
			"docker": {
				Status:    "healthy",
				Message:   fmt.Sprintf("Monitoring %d containers", len(containers)),
				LastCheck: time.Now(),
			},
		},
		Uptime:  time.Since(time.Now().Add(-time.Hour)).String(),
		Version: "1.0.0",
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(status)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *Server) parseDockerLogLine(line string) (time.Time, string) {
	line = strings.TrimPrefix(line, "stdout: ")
	line = strings.TrimPrefix(line, "stderr: ")

	parts := strings.SplitN(line, " ", 2)
	if len(parts) < 2 {
		return time.Now(), line
	}

	timeFormats := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05.000000000Z",
		"2006-01-02T15:04:05Z",
	}

	for _, format := range timeFormats {
		if timestamp, err := time.Parse(format, parts[0]); err == nil {
			return timestamp, parts[1]
		}
	}

	return time.Now(), line
}

func (s *Server) extractLogLevel(message string) string {
	message = strings.ToLower(message)
	switch {
	case strings.Contains(message, "fatal") || strings.Contains(message, "panic"):
		return "fatal"
	case strings.Contains(message, "error") || strings.Contains(message, "err"):
		return "error"
	case strings.Contains(message, "warn"):
		return "warn"
	case strings.Contains(message, "debug"):
		return "debug"
	case strings.Contains(message, "info"):
		return "info"
	default:
		return "info"
	}
}

func (s *Server) AddAlert(alert types.Alert) {
	alertResponse := AlertResponse{
		Timestamp:     alert.Timestamp,
		ContainerID:   alert.Source.ContainerID,
		ContainerName: alert.Source.ContainerName,
		Level:         s.mapSeverityToLevel(alert.Severity),
		Severity:      alert.Severity.String(),
		Message:       alert.Message,
	}

	s.alerts = append([]AlertResponse{alertResponse}, s.alerts...)

	if len(s.alerts) > s.maxAlerts {
		s.alerts = s.alerts[:s.maxAlerts]
	}
}

func (s *Server) mapSeverityToLevel(severity types.Severity) string {
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
