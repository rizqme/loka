package main

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"github.com/spf13/cobra"
)

func newStartCmd() *cobra.Command {
	var (
		configPath string
		foreground bool
	)

	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the LOKA daemon locally",
		Long:  "Starts lokad with an embedded worker. This is the simplest way to run LOKA.",
		RunE: func(cmd *cobra.Command, args []string) error {
			lokadPath, err := findLokad()
			if err != nil {
				return err
			}

			env := os.Environ()
			if configPath != "" {
				env = append(env, "LOKA_CONFIG="+configPath)
			}

			if foreground {
				// Run in foreground — replace this process with lokad.
				return syscall.Exec(lokadPath, []string{"lokad"}, env)
			}

			// Run in background.
			proc := exec.Command(lokadPath)
			proc.Env = env
			proc.Stdout = os.Stdout
			proc.Stderr = os.Stderr

			if err := proc.Start(); err != nil {
				return fmt.Errorf("failed to start lokad: %w", err)
			}

			fmt.Printf("LOKA started (pid %d)\n", proc.Process.Pid)
			fmt.Printf("  API: http://localhost:8080\n")
			fmt.Printf("  Stop: loka stop\n")
			return nil
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", "", "Config file path")
	cmd.Flags().BoolVarP(&foreground, "foreground", "f", false, "Run in foreground (don't detach)")

	return cmd
}

func newStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the LOKA daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Find and kill lokad process.
			out, err := exec.Command("pgrep", "-f", "lokad").Output()
			if err != nil {
				fmt.Println("LOKA is not running")
				return nil
			}

			pid := string(out[:len(out)-1]) // trim newline
			if err := exec.Command("kill", pid).Run(); err != nil {
				return fmt.Errorf("failed to stop lokad (pid %s): %w", pid, err)
			}

			fmt.Printf("LOKA stopped (pid %s)\n", pid)
			return nil
		},
	}
}

func findLokad() (string, error) {
	// Check common locations.
	candidates := []string{
		"lokad",
		"/usr/local/bin/lokad",
		"./bin/lokad",
	}
	for _, c := range candidates {
		if path, err := exec.LookPath(c); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("lokad not found — run 'curl -fsSL https://rizqme.github.io/loka/install.sh | bash' to install")
}
