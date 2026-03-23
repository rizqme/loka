package store

import "fmt"

// Config holds database configuration.
type Config struct {
	Driver string // "sqlite" or "postgres"
	DSN    string
}

// Factory function type — implementations register themselves.
type FactoryFunc func(dsn string) (Store, error)

var factories = map[string]FactoryFunc{}

// RegisterFactory registers a store factory for a driver name.
func RegisterFactory(driver string, fn FactoryFunc) {
	factories[driver] = fn
}

// Open creates a new store based on the driver config.
func Open(cfg Config) (Store, error) {
	fn, ok := factories[cfg.Driver]
	if !ok {
		return nil, fmt.Errorf("unknown database driver: %s (available: sqlite, postgres)", cfg.Driver)
	}
	return fn(cfg.DSN)
}
