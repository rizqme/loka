package lokaapi

import (
	"context"
	"time"
)

// Worker represents a LOKA worker.
type Worker struct {
	ID           string            `json:"ID"`
	Hostname     string            `json:"Hostname"`
	IPAddress    string            `json:"IPAddress"`
	Provider     string            `json:"Provider"`
	Region       string            `json:"Region"`
	Zone         string            `json:"Zone"`
	Status       string            `json:"Status"`
	Labels       map[string]string `json:"Labels"`
	Capacity     ResourceCapacity  `json:"Capacity"`
	AgentVersion string            `json:"AgentVersion"`
	KVMAvailable bool              `json:"KVMAvailable"`
	CreatedAt    time.Time         `json:"CreatedAt"`
	LastSeen     time.Time         `json:"LastSeen"`
}

type ResourceCapacity struct {
	CPUCores int   `json:"CPUCores"`
	MemoryMB int64 `json:"MemoryMB"`
	DiskMB   int64 `json:"DiskMB"`
}

type ListWorkersResp struct {
	Workers []Worker `json:"workers"`
	Total   int      `json:"total"`
}

func (c *Client) ListWorkers(ctx context.Context) (*ListWorkersResp, error) {
	var resp ListWorkersResp
	err := c.do(ctx, "GET", "/api/v1/workers", nil, &resp)
	return &resp, err
}

func (c *Client) GetWorker(ctx context.Context, id string) (*Worker, error) {
	var w Worker
	err := c.do(ctx, "GET", "/api/v1/workers/"+id, nil, &w)
	return &w, err
}

func (c *Client) DrainWorker(ctx context.Context, id string, timeoutSeconds int) (*Worker, error) {
	var w Worker
	err := c.do(ctx, "POST", "/api/v1/workers/"+id+"/drain", map[string]int{"timeout_seconds": timeoutSeconds}, &w)
	return &w, err
}

func (c *Client) UndrainWorker(ctx context.Context, id string) (*Worker, error) {
	var w Worker
	err := c.do(ctx, "POST", "/api/v1/workers/"+id+"/undrain", nil, &w)
	return &w, err
}

func (c *Client) RemoveWorker(ctx context.Context, id string, force bool) error {
	path := "/api/v1/workers/" + id
	if force {
		path += "?force=true"
	}
	return c.do(ctx, "DELETE", path, nil, nil)
}

func (c *Client) LabelWorker(ctx context.Context, id string, labels map[string]string) (*Worker, error) {
	var w Worker
	err := c.do(ctx, "PUT", "/api/v1/workers/"+id+"/labels", map[string]any{"labels": labels}, &w)
	return &w, err
}

func (c *Client) MigrateSession(ctx context.Context, sessionID, targetWorkerID string) (*Session, error) {
	var s Session
	err := c.do(ctx, "POST", "/api/v1/sessions/"+sessionID+"/migrate", map[string]string{"target_worker_id": targetWorkerID}, &s)
	return &s, err
}
