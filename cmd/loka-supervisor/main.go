package main

import (
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"github.com/vyprai/loka/internal/loka"
	"github.com/vyprai/loka/internal/supervisor"
	"github.com/vyprai/loka/pkg/version"
)

// loka-supervisor runs inside the VM as the init process (PID 1).
// It is a minimal exec agent: listens on vsock, executes commands,
// manages services, enforces security policy.
//
// Filesystem (virtiofs, overlay) is handled by the VMM — not here.
func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	logger.Info("loka-supervisor starting", "version", version.Version)

	// As PID 1, mount /proc for process management.
	_ = exec.Command("mount", "-t", "proc", "proc", "/proc").Run()

	// Default policy — updated by the host via set_policy RPC.
	policy := loka.DefaultExecPolicy()
	mode := loka.ModeExplore
	if m := os.Getenv("LOKA_MODE"); m != "" {
		mode = loka.ExecMode(m)
	}

	// Listen on vsock (inside VM) or unix socket (local testing).
	listenAddr := os.Getenv("LOKA_SUPERVISOR_SOCK")
	if listenAddr == "" {
		if _, err := os.Stat("/dev/vsock"); err == nil {
			listenAddr = "vsock:52"
		} else {
			listenAddr = "/tmp/loka-supervisor.sock"
		}
	}

	server := supervisor.NewServer(policy, mode, logger)

	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		logger.Info("supervisor shutting down")
		server.Stop()
	}()

	if err := server.ListenAndServe(listenAddr); err != nil {
		logger.Error("supervisor error", "error", err)
		os.Exit(1)
	}
}
