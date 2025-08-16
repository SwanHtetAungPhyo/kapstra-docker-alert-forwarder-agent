package webhook

import (
	"awesomeProject/internal/types"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

type Sender struct {
	client      *http.Client
	retryPolicy RetryPolicy
}

type RetryPolicy struct {
	MaxAttempts   int
	InitialDelay  time.Duration
	MaxDelay      time.Duration
	BackoffFactor float64
}

type Payload struct {
	SourceName    string                 `json:"sourceName"`
	Timestamp     string                 `json:"timestamp"`
	ContainerID   string                 `json:"container_id"`
	ContainerName string                 `json:"container_name"`
	Level         string                 `json:"level"`
	Severity      string                 `json:"severity"`
	Message       string                 `json:"message"`
	Metadata      map[string]interface{} `json:"metadata,omitempty"`
}

func NewSender() *Sender {
	return &Sender{
		client: &http.Client{
			Timeout: 10 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     90 * time.Second,
			},
		},
		retryPolicy: RetryPolicy{
			MaxAttempts:   3,
			InitialDelay:  1 * time.Second,
			MaxDelay:      60 * time.Second,
			BackoffFactor: 2.0,
		},
	}
}

func (s *Sender) Send(ctx context.Context, webhookURL string, alert types.Alert) error {
	payload := Payload{
		Timestamp:     alert.Timestamp.Format(time.RFC3339),
		ContainerID:   alert.Source.ContainerID,
		ContainerName: alert.Source.ContainerName,
		Level:         s.mapSeverityToLevel(alert.Severity),
		Severity:      alert.Severity.String(),
		Message:       alert.Message,
		Metadata:      alert.Metadata,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	return s.sendWithRetry(ctx, webhookURL, jsonData)
}

func (s *Sender) sendWithRetry(ctx context.Context, url string, data []byte) error {
	var lastErr error
	delay := s.retryPolicy.InitialDelay

	for attempt := 1; attempt <= s.retryPolicy.MaxAttempts; attempt++ {
		if attempt > 1 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}

			delay = time.Duration(float64(delay) * s.retryPolicy.BackoffFactor)
			if delay > s.retryPolicy.MaxDelay {
				delay = s.retryPolicy.MaxDelay
			}
		}

		if err := s.sendOnce(ctx, url, data); err != nil {
			lastErr = err
			if !s.isRetryableError(err) {
				return err
			}
			continue
		}

		return nil
	}

	return fmt.Errorf("webhook failed after %d attempts: %w", s.retryPolicy.MaxAttempts, lastErr)
}

func (s *Sender) sendOnce(ctx context.Context, url string, data []byte) error {
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(data))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "alert-agent/1.0")
	// req.Header.Set("Authorization", "Bearer "+s.token)
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			log.Printf("failed to close response body: %v", err)
		}
	}(resp.Body)

	if resp.StatusCode >= 400 {
		return fmt.Errorf("webhook returned status %d", resp.StatusCode)
	}

	return nil
}

func (s *Sender) isRetryableError(err error) bool {
	if err == nil {
		return false
	}

	errStr := err.Error()
	return bytes.Contains([]byte(errStr), []byte("timeout")) ||
		bytes.Contains([]byte(errStr), []byte("connection")) ||
		bytes.Contains([]byte(errStr), []byte("502")) ||
		bytes.Contains([]byte(errStr), []byte("503")) ||
		bytes.Contains([]byte(errStr), []byte("504"))
}

func (s *Sender) mapSeverityToLevel(severity types.Severity) string {
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

func (s *Sender) Close() error {
	s.client.CloseIdleConnections()
	return nil
}
