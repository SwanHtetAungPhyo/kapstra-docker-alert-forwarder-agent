package processor

import (
	"awesomeProject/internal/types"
	"bufio"
	"context"
	"io"
	"regexp"
	"strings"
	"time"
)

type LogProcessor struct {
	patterns   map[types.LogLevel]*regexp.Regexp
	bufferSize int
	alertChan  chan types.Alert
}

func New(bufferSize int, alertChan chan types.Alert) *LogProcessor {
	return &LogProcessor{
		patterns: map[types.LogLevel]*regexp.Regexp{
			types.LogLevelError: regexp.MustCompile(`(?i)\b(error|err|exception|fail|failed)\b`),
			types.LogLevelWarn:  regexp.MustCompile(`(?i)\b(warn|warning)\b`),
			types.LogLevelFatal: regexp.MustCompile(`(?i)\b(fatal|critical)\b`),
			types.LogLevelPanic: regexp.MustCompile(`(?i)\b(panic|emergency)\b`),
		},
		bufferSize: bufferSize,
		alertChan:  alertChan,
	}
}

func (p *LogProcessor) ProcessStream(ctx context.Context, containerID, containerName string, logStream io.ReadCloser) error {
	defer logStream.Close()

	scanner := bufio.NewScanner(logStream)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line := scanner.Text()
		if len(line) > 8 {
			line = line[8:]
		}

		level := p.detectLogLevel(line)
		if level == types.LogLevelUnknown {
			continue
		}

		alert := types.Alert{
			ID:        generateAlertID(),
			Timestamp: time.Now(),
			Severity:  p.mapLogLevelToSeverity(level),
			Source: types.Source{
				ContainerID:   containerID,
				ContainerName: containerName,
			},
			Message: strings.TrimSpace(line),
		}

		select {
		case p.alertChan <- alert:
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}

	return scanner.Err()
}

func (p *LogProcessor) detectLogLevel(logLine string) types.LogLevel {
	line := strings.ToLower(logLine)

	if p.patterns[types.LogLevelPanic].MatchString(line) {
		return types.LogLevelPanic
	}
	if p.patterns[types.LogLevelFatal].MatchString(line) {
		return types.LogLevelFatal
	}
	if p.patterns[types.LogLevelError].MatchString(line) {
		return types.LogLevelError
	}
	if p.patterns[types.LogLevelWarn].MatchString(line) {
		return types.LogLevelWarn
	}

	if strings.Contains(line, "404") || strings.Contains(line, "500") ||
		strings.Contains(line, "502") || strings.Contains(line, "503") {
		return types.LogLevelError
	}

	return types.LogLevelUnknown
}

func (p *LogProcessor) mapLogLevelToSeverity(level types.LogLevel) types.Severity {
	switch level {
	case types.LogLevelPanic:
		return types.SeverityCritical
	case types.LogLevelFatal:
		return types.SeverityCritical
	case types.LogLevelError:
		return types.SeverityHigh
	case types.LogLevelWarn:
		return types.SeverityMedium
	default:
		return types.SeverityLow
	}
}

func generateAlertID() string {
	return time.Now().Format("20060102150405") + "-" + randomString(8)
}

func randomString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[time.Now().UnixNano()%int64(len(charset))]
	}
	return string(b)
}
