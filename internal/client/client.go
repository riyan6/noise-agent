package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"noise-agent/internal/model"
)

var (
	ErrUnauthorized = errors.New("unauthorized agent token")
	ErrConflict     = errors.New("report sequence or bootId conflict")
	ErrBadRequest   = errors.New("invalid agent request")
)

type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

func New(endpoint, token string) *Client {
	return &Client{
		baseURL: strings.TrimRight(strings.TrimSpace(endpoint), "/"),
		token:   strings.TrimSpace(token),
		http: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

type envelope[T any] struct {
	Status  string `json:"status"`
	Data    T      `json:"data"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+"/api/v1/agent"+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}

	if resp.StatusCode == http.StatusUnauthorized {
		return ErrUnauthorized
	}
	if resp.StatusCode == http.StatusConflict {
		return ErrConflict
	}
	if resp.StatusCode == http.StatusBadRequest || resp.StatusCode == http.StatusNotFound {
		var errEnv envelope[any]
		_ = json.Unmarshal(raw, &errEnv)
		return fmt.Errorf("%w: %s", ErrBadRequest, errEnv.Message)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("unexpected HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	if out == nil {
		return nil
	}
	var env struct {
		Status string          `json:"status"`
		Data   json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return err
	}
	if env.Status != "success" {
		return fmt.Errorf("API returned status %q", env.Status)
	}
	if len(env.Data) == 0 || string(env.Data) == "null" {
		return nil
	}
	return json.Unmarshal(env.Data, out)
}

func (c *Client) GetConfig(ctx context.Context) (model.AgentConfig, error) {
	var cfg model.AgentConfig
	err := c.do(ctx, http.MethodGet, "/config", nil, &cfg)
	return cfg, err
}

func (c *Client) PostInfo(ctx context.Context, info model.AgentInfo) error {
	return c.do(ctx, http.MethodPost, "/info", info, nil)
}

func (c *Client) PostReport(ctx context.Context, report model.Report) (model.ReportAck, error) {
	var ack model.ReportAck
	err := c.do(ctx, http.MethodPost, "/report", report, &ack)
	return ack, err
}

func (c *Client) PostLatencyResults(ctx context.Context, results []model.LatencyResult) error {
	payload := struct {
		Results []model.LatencyResult `json:"results"`
	}{Results: results}
	return c.do(ctx, http.MethodPost, "/latency-results", payload, nil)
}
