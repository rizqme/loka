package api

import (
	"fmt"
	"io"
	"log/slog"

	pb "github.com/vyprai/loka/api/lokav1"
	"github.com/vyprai/loka/internal/loka"
)

// FileTunnel handles the bidirectional stream for mounting local files into a session.
// The CLI sends an init message, then the CP relays filesystem operations between
// the worker's VM and the CLI's local filesystem.
func (s *GRPCServer) FileTunnel(stream pb.ControlService_FileTunnelServer) error {
	// First message must be TunnelInit.
	msg, err := stream.Recv()
	if err != nil {
		return fmt.Errorf("receive init: %w", err)
	}

	init := msg.GetInit()
	if init == nil {
		return fmt.Errorf("first message must be TunnelInit")
	}

	sessionID := msg.SessionId
	s.logger.Info("file tunnel opened",
		"session", sessionID,
		"local_path", init.LocalPath,
		"mount_path", init.MountPath,
		"read_only", init.ReadOnly)

	// Verify session exists and is running.
	sess, err := s.sm.Get(stream.Context(), sessionID)
	if err != nil {
		return fmt.Errorf("session not found: %w", err)
	}
	if sess.Status != loka.SessionStatusRunning {
		return fmt.Errorf("session is %s, must be running", sess.Status)
	}

	// Register this tunnel so the worker can route filesystem requests through it.
	tunnel := &activeTunnel{
		sessionID: sessionID,
		mountPath: init.MountPath,
		localPath: init.LocalPath,
		readOnly:  init.ReadOnly,
		stream:    stream,
		logger:    s.logger,
	}

	// Relay messages between the worker and CLI until the stream closes.
	return tunnel.relay()
}

// activeTunnel manages a single file tunnel session.
type activeTunnel struct {
	sessionID string
	mountPath string
	localPath string
	readOnly  bool
	stream    pb.ControlService_FileTunnelServer
	logger    *slog.Logger
}

// relay reads messages from the stream and handles them.
// In the full implementation, this would relay between the CLI and the worker's VM.
// For now, it keeps the stream open and logs activity.
func (t *activeTunnel) relay() error {
	t.logger.Info("tunnel relay started",
		"session", t.sessionID,
		"mount", t.mountPath)

	for {
		msg, err := t.stream.Recv()
		if err == io.EOF {
			t.logger.Info("tunnel closed by client", "session", t.sessionID)
			return nil
		}
		if err != nil {
			return fmt.Errorf("tunnel recv: %w", err)
		}

		// Handle messages from the CLI side.
		switch p := msg.Payload.(type) {
		case *pb.FileTunnelMessage_ReadResp:
			t.logger.Debug("tunnel: read response", "eof", p.ReadResp.Eof, "bytes", len(p.ReadResp.Data))
		case *pb.FileTunnelMessage_WriteResp:
			t.logger.Debug("tunnel: write response", "bytes", p.WriteResp.BytesWritten)
		case *pb.FileTunnelMessage_ListResp:
			t.logger.Debug("tunnel: list response", "entries", len(p.ListResp.Entries))
		case *pb.FileTunnelMessage_StatResp:
			t.logger.Debug("tunnel: stat response", "exists", p.StatResp.Exists)
		case *pb.FileTunnelMessage_Error:
			t.logger.Warn("tunnel: error from client", "message", p.Error.Message, "path", p.Error.Path)
		default:
			t.logger.Debug("tunnel: unknown message type")
		}
	}
}
