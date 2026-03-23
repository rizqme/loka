package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/rizqme/loka/internal/loka"
)

// CPClient communicates with the control plane's HTTP API.
// In production, this will be replaced with gRPC.
type CPClient struct {
	baseURL string
	token   string
	http    *http.Client
	logger  *slog.Logger
}

// NewCPClient creates a new control plane client.
func NewCPClient(baseURL, token string, logger *slog.Logger) *CPClient {
	return &CPClient{
		baseURL: baseURL,
		token:   token,
		http:    &http.Client{Timeout: 10 * time.Second},
		logger:  logger,
	}
}

func (c *CPClient) post(ctx context.Context, path string, body any, result any) error {
	var bodyReader io.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		bodyReader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+path, bodyReader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		var errResp map[string]string
		json.NewDecoder(resp.Body).Decode(&errResp)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, errResp["error"])
	}
	if result != nil {
		return json.NewDecoder(resp.Body).Decode(result)
	}
	return nil
}

// Register registers this worker with the control plane.
func (c *CPClient) Register(ctx context.Context, hostname, provider string, capacity loka.ResourceCapacity, labels map[string]string) (string, error) {
	req := map[string]any{
		"hostname": hostname,
		"provider": provider,
		"capacity": capacity,
		"labels":   labels,
	}
	var resp struct {
		WorkerID string `json:"worker_id"`
	}
	// For MVP, we'll use the internal registration endpoint.
	// In production, this is a gRPC call.
	err := c.post(ctx, "/api/internal/workers/register", req, &resp)
	return resp.WorkerID, err
}

// ReportExecComplete reports execution results to the control plane.
func (c *CPClient) ReportExecComplete(ctx context.Context, sessionID, execID string, status loka.ExecStatus, results []loka.CommandResult, errMsg string) error {
	return c.post(ctx, "/api/internal/exec/complete", map[string]any{
		"session_id": sessionID,
		"exec_id":    execID,
		"status":     string(status),
		"results":    results,
		"error":      errMsg,
	}, nil)
}

// ReportSessionStatus reports session status to the control plane.
func (c *CPClient) ReportSessionStatus(ctx context.Context, sessionID string, status loka.SessionStatus) error {
	return c.post(ctx, "/api/internal/sessions/status", map[string]any{
		"session_id": sessionID,
		"status":     string(status),
	}, nil)
}
