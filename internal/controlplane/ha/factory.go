package ha

import "fmt"

// Config holds coordinator configuration.
type Config struct {
	Type           string // "local" or "redis"
	Address        string
	Password       string
	SentinelMaster string
}

// Factory function type.
type FactoryFunc func(cfg Config) (Coordinator, error)

var factories = map[string]FactoryFunc{}

// RegisterFactory registers a coordinator factory.
func RegisterFactory(typeName string, fn FactoryFunc) {
	factories[typeName] = fn
}

// Open creates a coordinator based on config.
func Open(cfg Config) (Coordinator, error) {
	fn, ok := factories[cfg.Type]
	if !ok {
		return nil, fmt.Errorf("unknown coordinator type: %s (available: local, redis)", cfg.Type)
	}
	return fn(cfg)
}
