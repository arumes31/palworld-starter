// Package control provides the narrow client used to control allowlisted game
// containers through the private Docker broker.
package control

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const maxResponseBytes = 1 << 20

var ErrNotFound = errors.New("container not found")

type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

type request struct {
	Container string `json:"container"`
}

type response struct {
	Status   string `json:"status,omitempty"`
	Logs     string `json:"logs,omitempty"`
	ExitCode int    `json:"exit_code,omitempty"`
	Output   string `json:"output,omitempty"`
}

func NewClient(baseURL, token string) (*Client, error) {
	if len(token) < 32 {
		return nil, fmt.Errorf("BROKER_TOKEN must contain at least 32 characters")
	}
	parsed, err := url.Parse(strings.TrimRight(baseURL, "/"))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return nil, fmt.Errorf("invalid broker URL")
	}
	return &Client{
		baseURL: parsed.String(),
		token:   token,
		http: &http.Client{
			Timeout: 40 * time.Second,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}, nil
}

func (c *Client) Status(ctx context.Context, container string) (string, error) {
	var result response
	err := c.call(ctx, "status", container, &result)
	return result.Status, err
}

func (c *Client) Logs(ctx context.Context, container string) (string, error) {
	var result response
	err := c.call(ctx, "logs", container, &result)
	return result.Logs, err
}

func (c *Client) Start(ctx context.Context, container string) error {
	return c.call(ctx, "start", container, nil)
}

func (c *Client) Stop(ctx context.Context, container string) error {
	return c.call(ctx, "stop", container, nil)
}

func (c *Client) Backup(ctx context.Context, container string) (int, string, error) {
	var result response
	err := c.call(ctx, "backup", container, &result)
	return result.ExitCode, result.Output, err
}

func (c *Client) call(ctx context.Context, action, container string, result *response) error {
	if strings.TrimSpace(container) == "" {
		return fmt.Errorf("container name is required")
	}
	body, err := json.Marshal(request{Container: container})
	if err != nil {
		return fmt.Errorf("encode broker request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/"+action, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create broker request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("call broker: %w", err)
	}
	defer resp.Body.Close()
	limited := io.LimitReader(resp.Body, maxResponseBytes+1)
	responseBody, err := io.ReadAll(limited)
	if err != nil {
		return fmt.Errorf("read broker response: %w", err)
	}
	if len(responseBody) > maxResponseBytes {
		return fmt.Errorf("broker response exceeds size limit")
	}
	if resp.StatusCode == http.StatusNotFound {
		return ErrNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("broker returned HTTP %d", resp.StatusCode)
	}
	if result != nil && len(responseBody) > 0 {
		if err := json.Unmarshal(responseBody, result); err != nil {
			return fmt.Errorf("decode broker response: %w", err)
		}
	}
	return nil
}
