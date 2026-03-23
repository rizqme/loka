package config

// WorkerConfig is the configuration for the loka-worker agent.
type WorkerConfig struct {
	ControlPlane WorkerCPConfig `yaml:"control_plane"`
	DataDir      string         `yaml:"data_dir"`   // Local data directory for overlays, caches.
	Provider     string         `yaml:"provider"`    // Provider name (e.g., "aws", "local", "selfmanaged").
	Token        string         `yaml:"token"`       // Registration token.
	Labels       map[string]string `yaml:"labels"`
	TLS          TLSConfig      `yaml:"tls"`
}

// WorkerCPConfig specifies how the worker connects to the control plane.
type WorkerCPConfig struct {
	Address string `yaml:"address"` // Control plane gRPC address.
}

// Defaults fills in default values for unset fields.
func (c *WorkerConfig) Defaults() {
	if c.ControlPlane.Address == "" {
		c.ControlPlane.Address = "localhost:9090"
	}
	if c.DataDir == "" {
		c.DataDir = "/var/loka/worker"
	}
	if c.Provider == "" {
		c.Provider = "local"
	}
}
