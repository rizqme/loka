package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	pb "github.com/vyprai/loka/api/lokav1"
	"github.com/vyprai/loka/pkg/lokaapi"
	"github.com/spf13/cobra"
)

func newSessionMountLocalCmd() *cobra.Command {
	var readOnly bool

	cmd := &cobra.Command{
		Use:   "mount <session-id> <local-path> <vm-path>",
		Short: "Mount a local directory into a running session via tunnel",
		Long: `Opens a gRPC tunnel that makes a local directory available inside the session VM.
The tunnel stays open until you press Ctrl+C. Files are served on-demand —
only data that the VM reads is transferred over the network.

Examples:
  loka session mount <id> ./project /workspace
  loka session mount <id> ~/data /data --read-only
  loka session mount <id> . /workspace`,
		Args: cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			sessionID := args[0]
			localPath, err := filepath.Abs(args[1])
			if err != nil {
				return fmt.Errorf("resolve path: %w", err)
			}
			vmPath := args[2]

			// Verify local path exists.
			info, err := os.Stat(localPath)
			if err != nil {
				return fmt.Errorf("local path %s: %w", localPath, err)
			}
			if !info.IsDir() {
				return fmt.Errorf("%s is not a directory", localPath)
			}

			grpcClient := newGRPCClient()
			if grpcClient == nil {
				return fmt.Errorf("cannot connect via gRPC — tunnel requires gRPC")
			}
			defer grpcClient.Close()

			fmt.Printf("Mounting %s → %s (session %s)\n", localPath, vmPath, shortID(sessionID))
			if readOnly {
				fmt.Println("  Mode: read-only")
			}
			fmt.Println("  Press Ctrl+C to unmount")
			fmt.Println()

			return runTunnel(cmd.Context(), grpcClient, sessionID, localPath, vmPath, readOnly)
		},
	}

	cmd.Flags().BoolVar(&readOnly, "read-only", false, "Mount as read-only")
	return cmd
}

func runTunnel(ctx context.Context, client *lokaapi.GRPCClient, sessionID, localPath, vmPath string, readOnly bool) error {
	stream, err := client.Proto().FileTunnel(ctx)
	if err != nil {
		return fmt.Errorf("open tunnel: %w", err)
	}

	// Send init message.
	if err := stream.Send(&pb.FileTunnelMessage{
		SessionId: sessionID,
		Payload: &pb.FileTunnelMessage_Init{
			Init: &pb.TunnelInit{
				LocalPath: localPath,
				MountPath: vmPath,
				ReadOnly:  readOnly,
			},
		},
	}); err != nil {
		return fmt.Errorf("send init: %w", err)
	}

	fmt.Println("Tunnel open. Serving local files...")

	// Handle requests from the CP/worker.
	for {
		msg, err := stream.Recv()
		if err == io.EOF {
			fmt.Println("Tunnel closed by server.")
			return nil
		}
		if err != nil {
			return fmt.Errorf("tunnel recv: %w", err)
		}

		var resp *pb.FileTunnelMessage

		switch p := msg.Payload.(type) {
		case *pb.FileTunnelMessage_ReadReq:
			resp = handleReadReq(sessionID, localPath, p.ReadReq)
		case *pb.FileTunnelMessage_WriteReq:
			if readOnly {
				resp = tunnelErr(sessionID, "read-only mount", p.WriteReq.Path)
			} else {
				resp = handleWriteReq(sessionID, localPath, p.WriteReq)
			}
		case *pb.FileTunnelMessage_ListReq:
			resp = handleListReq(sessionID, localPath, p.ListReq)
		case *pb.FileTunnelMessage_StatReq:
			resp = handleStatReq(sessionID, localPath, p.StatReq)
		default:
			continue
		}

		if resp != nil {
			if err := stream.Send(resp); err != nil {
				return fmt.Errorf("tunnel send: %w", err)
			}
		}
	}
}

func handleReadReq(sessionID, localPath string, req *pb.TunnelReadReq) *pb.FileTunnelMessage {
	fullPath := filepath.Join(localPath, filepath.Clean(req.Path))
	if !strings.HasPrefix(fullPath, localPath) {
		return tunnelErr(sessionID, "path escape", req.Path)
	}

	f, err := os.Open(fullPath)
	if err != nil {
		return tunnelErr(sessionID, err.Error(), req.Path)
	}
	defer f.Close()

	if req.Offset > 0 {
		f.Seek(req.Offset, io.SeekStart)
	}

	size := req.Length
	if size <= 0 {
		size = 64 * 1024 // 64KB default chunk
	}
	buf := make([]byte, size)
	n, err := f.Read(buf)
	eof := err == io.EOF

	return &pb.FileTunnelMessage{
		SessionId: sessionID,
		Payload: &pb.FileTunnelMessage_ReadResp{
			ReadResp: &pb.TunnelReadResp{
				Data: buf[:n],
				Eof:  eof,
			},
		},
	}
}

func handleWriteReq(sessionID, localPath string, req *pb.TunnelWriteReq) *pb.FileTunnelMessage {
	fullPath := filepath.Join(localPath, filepath.Clean(req.Path))
	if !strings.HasPrefix(fullPath, localPath) {
		return tunnelErr(sessionID, "path escape", req.Path)
	}

	os.MkdirAll(filepath.Dir(fullPath), 0o755)

	flags := os.O_WRONLY | os.O_CREATE
	if req.Truncate {
		flags |= os.O_TRUNC
	}
	f, err := os.OpenFile(fullPath, flags, 0o644)
	if err != nil {
		return tunnelErr(sessionID, err.Error(), req.Path)
	}
	defer f.Close()

	if req.Offset > 0 {
		f.Seek(req.Offset, io.SeekStart)
	}

	n, err := f.Write(req.Data)
	if err != nil {
		return tunnelErr(sessionID, err.Error(), req.Path)
	}

	return &pb.FileTunnelMessage{
		SessionId: sessionID,
		Payload: &pb.FileTunnelMessage_WriteResp{
			WriteResp: &pb.TunnelWriteResp{
				BytesWritten: int64(n),
			},
		},
	}
}

func handleListReq(sessionID, localPath string, req *pb.TunnelListReq) *pb.FileTunnelMessage {
	fullPath := filepath.Join(localPath, filepath.Clean(req.Path))
	if !strings.HasPrefix(fullPath, localPath) {
		return tunnelErr(sessionID, "path escape", req.Path)
	}

	entries, err := os.ReadDir(fullPath)
	if err != nil {
		return tunnelErr(sessionID, err.Error(), req.Path)
	}

	var pbEntries []*pb.TunnelDirEntry
	for _, e := range entries {
		info, _ := e.Info()
		entry := &pb.TunnelDirEntry{
			Name:  e.Name(),
			IsDir: e.IsDir(),
		}
		if info != nil {
			entry.Size = info.Size()
			entry.Mode = int64(info.Mode())
			entry.ModTime = info.ModTime().Unix()
		}
		pbEntries = append(pbEntries, entry)
	}

	return &pb.FileTunnelMessage{
		SessionId: sessionID,
		Payload: &pb.FileTunnelMessage_ListResp{
			ListResp: &pb.TunnelListResp{
				Entries: pbEntries,
			},
		},
	}
}

func handleStatReq(sessionID, localPath string, req *pb.TunnelStatReq) *pb.FileTunnelMessage {
	fullPath := filepath.Join(localPath, filepath.Clean(req.Path))
	if !strings.HasPrefix(fullPath, localPath) {
		return tunnelErr(sessionID, "path escape", req.Path)
	}

	info, err := os.Stat(fullPath)
	if err != nil {
		return &pb.FileTunnelMessage{
			SessionId: sessionID,
			Payload: &pb.FileTunnelMessage_StatResp{
				StatResp: &pb.TunnelStatResp{Exists: false},
			},
		}
	}

	return &pb.FileTunnelMessage{
		SessionId: sessionID,
		Payload: &pb.FileTunnelMessage_StatResp{
			StatResp: &pb.TunnelStatResp{
				Name:    info.Name(),
				IsDir:   info.IsDir(),
				Size:    info.Size(),
				Mode:    int64(info.Mode()),
				ModTime: info.ModTime().Unix(),
				Exists:  true,
			},
		},
	}
}

func tunnelErr(sessionID, message, path string) *pb.FileTunnelMessage {
	return &pb.FileTunnelMessage{
		SessionId: sessionID,
		Payload: &pb.FileTunnelMessage_Error{
			Error: &pb.TunnelError{
				Message: message,
				Path:    path,
			},
		},
	}
}
