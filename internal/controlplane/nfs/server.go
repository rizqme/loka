// Package nfs provides a pure-Go NFSv3 server for "store" volumes.
// Store volumes are directories on the control plane shared via NFS to workers,
// which then expose them to guest VMs via virtiofs. This provides cross-worker
// shared storage with POSIX file locking (flock/fcntl).
package nfs

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sync"

	"github.com/go-git/go-billy/v5/osfs"
	nfs "github.com/willscott/go-nfs"
	nfshelper "github.com/willscott/go-nfs/helpers"
)

// StoreServer exports store volume directories via NFSv3.
type StoreServer struct {
	dataDir    string // Root directory for all stores ($dataDir/stores/).
	listenAddr string
	listener   net.Listener
	logger     *slog.Logger

	mu      sync.Mutex
	running bool
	cancel  context.CancelFunc
}

// NewStoreServer creates a new NFS server that exports store volumes.
// dataDir is the root stores directory (e.g. $dataDir/stores).
// listenAddr is the NFS listen address (e.g. ":2049" or "127.0.0.1:2049").
func NewStoreServer(dataDir string, listenAddr string, logger *slog.Logger) (*StoreServer, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("create stores dir: %w", err)
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &StoreServer{
		dataDir:    dataDir,
		listenAddr: listenAddr,
		logger:     logger,
	}, nil
}

// Start begins serving NFS requests. Blocks until ctx is cancelled.
func (s *StoreServer) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return nil
	}

	listener, err := net.Listen("tcp", s.listenAddr)
	if err != nil {
		s.mu.Unlock()
		return fmt.Errorf("listen %s: %w", s.listenAddr, err)
	}
	s.listener = listener
	s.running = true

	ctx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	s.mu.Unlock()

	s.logger.Info("NFS store server started", "addr", listener.Addr().String(), "dir", s.dataDir)

	// go-nfs serves from a billy.Filesystem rooted at our stores directory.
	fs := osfs.New(s.dataDir)
	handler := nfshelper.NewNullAuthHandler(fs)

	// Run NFS server (blocks until listener is closed).
	go func() {
		<-ctx.Done()
		listener.Close()
	}()

	err = nfs.Serve(listener, handler)
	if ctx.Err() != nil {
		return nil // Graceful shutdown.
	}
	return err
}

// Stop shuts down the NFS server.
func (s *StoreServer) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		s.cancel()
	}
	if s.listener != nil {
		s.listener.Close()
	}
	s.running = false
	s.logger.Info("NFS store server stopped")
}

// Addr returns the listen address (after Start).
func (s *StoreServer) Addr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listener != nil {
		return s.listener.Addr().String()
	}
	return s.listenAddr
}

// EnsureStore creates a store directory if it doesn't exist.
// Returns the absolute path to the store directory.
func (s *StoreServer) EnsureStore(name string) (string, error) {
	storePath := filepath.Join(s.dataDir, name)
	if err := os.MkdirAll(storePath, 0o755); err != nil {
		return "", fmt.Errorf("create store %q: %w", name, err)
	}
	return storePath, nil
}

// DeleteStore removes a store directory.
func (s *StoreServer) DeleteStore(name string) error {
	storePath := filepath.Join(s.dataDir, name)
	return os.RemoveAll(storePath)
}

// ListStores returns the names of all existing store directories.
func (s *StoreServer) ListStores() ([]string, error) {
	entries, err := os.ReadDir(s.dataDir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	return names, nil
}
