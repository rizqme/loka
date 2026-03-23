package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	pb "github.com/vyprai/loka/api/lokav1"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func newShellCmd() *cobra.Command {
	var (
		shell   string
		workdir string
	)

	cmd := &cobra.Command{
		Use:   "shell <session-id>",
		Short: "Open an interactive shell in a session",
		Long: `Opens an interactive terminal inside a running session VM.
Supports full PTY: tab completion, arrow keys, Ctrl-C, resize, etc.

Examples:
  loka shell <id>                  # Default: /bin/bash
  loka shell <id> --shell /bin/sh
  loka shell <id> --workdir /workspace`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			sessionID := args[0]
			if shell == "" {
				shell = "/bin/bash"
			}

			grpcClient := newGRPCClient()
			if grpcClient == nil {
				return fmt.Errorf("cannot connect via gRPC — interactive shell requires gRPC")
			}
			defer grpcClient.Close()

			return runShell(cmd.Context(), grpcClient.Proto(), sessionID, shell, workdir)
		},
	}

	cmd.Flags().StringVar(&shell, "shell", "/bin/bash", "Shell to run")
	cmd.Flags().StringVar(&workdir, "workdir", "", "Working directory")
	return cmd
}

func runShell(ctx context.Context, client pb.ControlServiceClient, sessionID, shell, workdir string) error {
	// Put terminal in raw mode.
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return fmt.Errorf("stdin is not a terminal — interactive shell requires a TTY")
	}

	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return fmt.Errorf("set raw mode: %w", err)
	}
	defer term.Restore(fd, oldState)

	// Get initial terminal size.
	cols, rows, err := term.GetSize(fd)
	if err != nil {
		cols, rows = 80, 24
	}

	// Open gRPC stream.
	stream, err := client.Shell(ctx)
	if err != nil {
		term.Restore(fd, oldState)
		return fmt.Errorf("open shell stream: %w", err)
	}

	// Send init.
	if err := stream.Send(&pb.ShellMessage{
		SessionId: sessionID,
		Payload: &pb.ShellMessage_Init{
			Init: &pb.ShellInit{
				Command: shell,
				Rows:    uint32(rows),
				Cols:    uint32(cols),
				Workdir: workdir,
			},
		},
	}); err != nil {
		term.Restore(fd, oldState)
		return fmt.Errorf("send shell init: %w", err)
	}

	// Handle terminal resize (SIGWINCH).
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGWINCH)
	go func() {
		for range sigCh {
			newCols, newRows, err := term.GetSize(fd)
			if err != nil {
				continue
			}
			stream.Send(&pb.ShellMessage{
				SessionId: sessionID,
				Payload: &pb.ShellMessage_Resize{
					Resize: &pb.ShellResize{
						Rows: uint32(newRows),
						Cols: uint32(newCols),
					},
				},
			})
		}
	}()

	// Goroutine: read from gRPC stream → write to terminal stdout.
	exitCode := 0
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			msg, err := stream.Recv()
			if err != nil {
				return
			}
			switch p := msg.Payload.(type) {
			case *pb.ShellMessage_Output:
				os.Stdout.Write(p.Output.Data)
			case *pb.ShellMessage_Exit:
				exitCode = int(p.Exit.ExitCode)
				return
			}
		}
	}()

	// Goroutine: read from terminal stdin → send to gRPC stream.
	go func() {
		buf := make([]byte, 1024)
		for {
			n, err := os.Stdin.Read(buf)
			if n > 0 {
				stream.Send(&pb.ShellMessage{
					SessionId: sessionID,
					Payload: &pb.ShellMessage_Input{
						Input: &pb.ShellInput{
							Data: buf[:n],
						},
					},
				})
			}
			if err == io.EOF {
				stream.CloseSend()
				return
			}
			if err != nil {
				return
			}
		}
	}()

	// Wait for the shell to exit.
	<-done

	// Restore terminal.
	term.Restore(fd, oldState)
	signal.Stop(sigCh)

	if exitCode != 0 {
		os.Exit(exitCode)
	}
	return nil
}
