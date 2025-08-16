package docker

import (
	"awesomeProject/internal/interfaces"
	"awesomeProject/internal/types"
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
)

type Client struct {
	client        *client.Client
	config        *interfaces.DockerConfig
	eventChan     chan types.ContainerEvent
	reconnectChan chan struct{}
}

func NewClient(config *interfaces.DockerConfig) (*Client, error) {
	opts := []client.Opt{
		client.FromEnv,
		client.WithAPIVersionNegotiation(),
	}

	if config.Host != "" {
		opts = append(opts, client.WithHost(config.Host))
	}

	cli, err := client.NewClientWithOpts(opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create Docker client: %w", err)
	}

	return &Client{
		client:        cli,
		config:        config,
		eventChan:     make(chan types.ContainerEvent, 100),
		reconnectChan: make(chan struct{}, 1),
	}, nil
}

func (c *Client) ListContainers(ctx context.Context) ([]types.ContainerInfo, error) {
	containers, err := c.client.ContainerList(ctx, container.ListOptions{
		All: true,
	})
	if err != nil {
		return nil, c.handleError(err)
	}

	var result []types.ContainerInfo
	for _, cont := range containers {
		if len(cont.Names) == 0 {
			continue
		}

		name := strings.TrimPrefix(cont.Names[0], "/")
		result = append(result, types.ContainerInfo{
			ID:     cont.ID,
			Name:   name,
			Labels: cont.Labels,
			State:  cont.State,
		})
	}

	return result, nil
}

func (c *Client) GetContainerLogs(ctx context.Context, containerID string) (*types.LogStream, error) {
	options := container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
		Timestamps: true,
		Since:      time.Now().Format(time.RFC3339),
	}

	logs, err := c.client.ContainerLogs(ctx, containerID, options)
	if err != nil {
		return nil, c.handleError(err)
	}

	return &types.LogStream{
		Reader: logs,
	}, nil
}

func (c *Client) WatchEvents(ctx context.Context) (<-chan types.ContainerEvent, error) {
	go c.eventWatcher(ctx)
	return c.eventChan, nil
}

func (c *Client) eventWatcher(ctx context.Context) {
	defer close(c.eventChan)

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.reconnectChan:
			time.Sleep(c.config.RetryInterval)
		default:
		}

		if err := c.watchEventsOnce(ctx); err != nil {
			select {
			case c.reconnectChan <- struct{}{}:
			default:
			}
		}
	}
}

func (c *Client) watchEventsOnce(ctx context.Context) error {
	filterArgs := filters.NewArgs()
	filterArgs.Add("type", "container")

	eventOptions := events.ListOptions{
		Filters: filterArgs,
	}

	eventChan, errChan := c.client.Events(ctx, eventOptions)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case event := <-eventChan:
			containerEvent := types.ContainerEvent{
				Type:        string(event.Action),
				ContainerID: event.Actor.ID,
				Name:        event.Actor.Attributes["name"],
				Labels:      make(map[string]string),
				Timestamp:   time.Unix(event.Time, 0),
			}

			for k, v := range event.Actor.Attributes {
				if strings.HasPrefix(k, "label.") {
					containerEvent.Labels[strings.TrimPrefix(k, "label.")] = v
				}
			}

			select {
			case c.eventChan <- containerEvent:
			case <-ctx.Done():
				return ctx.Err()
			default:
			}

		case err := <-errChan:
			if err != nil {
				return err
			}
		}
	}
}

func (c *Client) handleError(err error) error {
	if client.IsErrConnectionFailed(err) {
		select {
		case c.reconnectChan <- struct{}{}:
		default:
		}
	}
	return err
}

func (c *Client) Close() error {
	if c.client != nil {
		return c.client.Close()
	}
	return nil
}

func (c *Client) Ping(ctx context.Context) error {
	_, err := c.client.Ping(ctx)
	return c.handleError(err)
}
