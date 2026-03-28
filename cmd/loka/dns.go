package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

func newDNSCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dns",
		Short: "Manage DNS resolution for .loka domains",
		Long: `Configure local DNS so that *.loka resolves to 127.0.0.1.

  loka dns enable    # Set up resolver and start DNS server
  loka dns disable   # Remove resolver config and stop DNS server
  loka dns status    # Show current DNS status`,
	}
	cmd.AddCommand(
		newDNSEnableCmd(),
		newDNSDisableCmd(),
		newDNSStatusCmd(),
	)
	return cmd
}

func newDNSEnableCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "enable",
		Short: "Enable DNS resolution for .loka domains",
		RunE: func(cmd *cobra.Command, args []string) error {
			// 1. Create the OS resolver config.
			if err := createResolverConfig(); err != nil {
				return err
			}

			// 2. Set up port forwarding (80,443 → 6843) so .loka works without port.
			if runtime.GOOS == "darwin" {
				setupPortProxy()
			}

			// 3. Enable domain proxy + DNS on the server.
			client := newClient()
			var resp map[string]any
			if err := client.Raw(cmd.Context(), "POST", "/api/v1/admin/dns", map[string]any{"enabled": true}, &resp); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: could not enable DNS on server: %v\n", err)
				fmt.Fprintf(os.Stderr, "Ensure lokad is running.\n")
			} else {
				fmt.Println("DNS server enabled on lokad.")
			}

			// 4. Trust the CA certificate for HTTPS without warnings.
			if runtime.GOOS == "darwin" {
				trustCACert()
			}

			fmt.Println("DNS resolution for .loka domains is now enabled.")
			fmt.Println("  *.loka -> 127.0.0.1")
			fmt.Println("  http://*.loka  (port 80 → 6843)")
			fmt.Println("  https://*.loka (port 443 → 6843)")
			fmt.Println()
			fmt.Println("Test it:")
			fmt.Println("  curl http://my-app.loka/")
			return nil
		},
	}
}

func newDNSDisableCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "disable",
		Short: "Disable DNS resolution for .loka domains",
		RunE: func(cmd *cobra.Command, args []string) error {
			// 1. Remove the OS resolver config.
			if err := removeResolverConfig(); err != nil {
				return err
			}

			// 2. Disable DNS on the server.
			client := newClient()
			var resp map[string]any
			if err := client.Raw(cmd.Context(), "POST", "/api/v1/admin/dns", map[string]any{"enabled": false}, &resp); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: could not disable DNS on server: %v\n", err)
			} else {
				fmt.Println("DNS server disabled on lokad.")
			}

			fmt.Println("DNS resolution for .loka domains has been disabled.")
			return nil
		},
	}
}

func newDNSStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show DNS resolution status for .loka domains",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Check resolver file.
			resolverPath := resolverFilePath()
			resolverExists := false
			if _, err := os.Stat(resolverPath); err == nil {
				resolverExists = true
			}

			fmt.Printf("Resolver file:  ")
			if resolverExists {
				fmt.Printf("%s (exists)\n", resolverPath)
			} else {
				fmt.Printf("%s (not found)\n", resolverPath)
			}

			// Check DNS server.
			fmt.Printf("DNS server:     ")
			out, err := exec.Command("dig", "@127.0.0.1", "-p", "5453", "test.loka", "+short", "+time=2", "+tries=1").Output()
			if err != nil {
				fmt.Println("not responding")
			} else {
				answer := strings.TrimSpace(string(out))
				if answer == "" {
					fmt.Println("responding but no answer")
				} else {
					fmt.Printf("responding (%s)\n", answer)
				}
			}

			// Overall status.
			fmt.Printf("Status:         ")
			if resolverExists {
				fmt.Println("enabled")
			} else {
				fmt.Println("disabled")
			}

			return nil
		},
	}
}

// isDNSEnabled checks if .loka DNS resolution is configured (resolver file exists + server responding).
func isDNSEnabled() bool {
	if _, err := os.Stat(resolverFilePath()); err != nil {
		return false
	}
	out, err := exec.Command("dig", "@127.0.0.1", "-p", "5453", "test.loka", "+short", "+time=1", "+tries=1").Output()
	return err == nil && len(strings.TrimSpace(string(out))) > 0
}

// enableDNS sets up the resolver file, port forwarding, and enables DNS on the server.
func enableDNS() bool {
	if err := createResolverConfig(); err != nil {
		fmt.Fprintf(os.Stderr, "  DNS setup failed: %v\n", err)
		return false
	}
	if runtime.GOOS == "darwin" {
		setupPortProxy()
	}
	client := newClient()
	ctx := context.Background()
	client.Raw(ctx, "POST", "/api/v1/admin/dns", map[string]any{"enabled": true}, nil)
	// Verify it works.
	for i := 0; i < 10; i++ {
		if isDNSEnabled() {
			return true
		}
		time.Sleep(500 * time.Millisecond)
	}
	return false
}

// setupPortProxy starts loka-proxy on ports 80/443 as root (via osascript),
// forwarding to the domain proxy on port 6843.
func setupPortProxy() {
	// Check if already running.
	pidFile := proxyPidFile()
	if data, err := os.ReadFile(pidFile); err == nil {
		pid := strings.TrimSpace(string(data))
		if pid != "" {
			// Check if process is alive.
			if err := exec.Command("kill", "-0", pid).Run(); err == nil {
				return // Already running.
			}
		}
	}

	// Find loka-proxy binary.
	proxyBin := findLokaProxy()
	if proxyBin == "" {
		fmt.Fprintf(os.Stderr, "  loka-proxy not found, skipping port 80/443 setup\n")
		return
	}

	// Find TLS certs for HTTPS.
	home, _ := os.UserHomeDir()
	certFile := filepath.Join(home, ".loka", "tls", "server.crt")
	keyFile := filepath.Join(home, ".loka", "tls", "server.key")
	// Also check /tmp/loka-data
	if _, err := os.Stat(certFile); err != nil {
		certFile = "/tmp/loka-data/tls/server.crt"
		keyFile = "/tmp/loka-data/tls/server.key"
	}

	args := fmt.Sprintf("%s -target 127.0.0.1:6843 -pid %s", proxyBin, pidFile)
	if _, err := os.Stat(certFile); err == nil {
		args += fmt.Sprintf(" -cert %s -key %s", certFile, keyFile)
	}

	fmt.Println("Starting port proxy (80,443 → 6843, requires sudo)...")
	cmd := exec.Command("sudo", "sh", "-c", args+" &")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Run()

	// Wait briefly for it to start.
	time.Sleep(1 * time.Second)
}

// stopPortProxy stops the loka-proxy process.
func stopPortProxy() {
	pidFile := proxyPidFile()
	data, err := os.ReadFile(pidFile)
	if err != nil {
		return
	}
	pid := strings.TrimSpace(string(data))
	if pid != "" {
		exec.Command("kill", pid).Run()
	}
	os.Remove(pidFile)
}

func proxyPidFile() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".loka", "proxy.pid")
}

// trustCACert imports the LOKA CA certificate into the macOS login keychain
// as a trusted root CA, so HTTPS connections to *.loka don't show warnings.
func trustCACert() {
	home, _ := os.UserHomeDir()
	candidates := []string{
		filepath.Join(home, ".loka", "tls", "ca.crt"),
		"/tmp/loka-data/tls/ca.crt",
	}
	var caCert string
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			caCert = c
			break
		}
	}
	if caCert == "" {
		return
	}

	// Check if already trusted in System keychain.
	out, _ := exec.Command("security", "find-certificate", "-c", "LOKA", "-a",
		"/Library/Keychains/System.keychain").Output()
	if strings.Contains(string(out), "LOKA") {
		return // Already trusted.
	}

	fmt.Println("Trusting LOKA CA certificate for HTTPS (requires sudo)...")
	cmd := exec.Command("sudo", "security", "add-trusted-cert", "-d", "-r", "trustRoot",
		"-p", "ssl", "-k", "/Library/Keychains/System.keychain", caCert)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Run()
}

func findLokaProxy() string {
	// Check next to loka binary.
	if self, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(self), "loka-proxy")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	candidates := []string{
		filepath.Join(os.Getenv("HOME"), ".loka", "bin", "loka-proxy"),
		"loka-proxy",
	}
	for _, p := range candidates {
		if path, err := exec.LookPath(p); err == nil {
			return path
		}
	}
	return ""
}

// resolverFilePath returns the path to the OS-level resolver configuration.
func resolverFilePath() string {
	if runtime.GOOS == "linux" {
		// Check for systemd-resolved first.
		if _, err := os.Stat("/etc/systemd/resolved.conf.d"); err == nil {
			return "/etc/systemd/resolved.conf.d/loka.conf"
		}
	}
	// macOS uses /etc/resolver/<domain>, Linux fallback does the same.
	return "/etc/resolver/loka"
}

// createResolverConfig creates the OS-level DNS resolver config.
func createResolverConfig() error {
	path := resolverFilePath()

	if runtime.GOOS == "linux" && strings.Contains(path, "systemd") {
		// systemd-resolved config.
		content := "[Resolve]\nDNS=127.0.0.1\nDomains=~loka\n"
		return writeSudoFile(path, content)
	}

	// macOS /etc/resolver/loka or Linux fallback.
	content := "nameserver 127.0.0.1\nport 5453\n"

	// Ensure /etc/resolver directory exists.
	dir := "/etc/resolver"
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		fmt.Printf("Creating %s (requires sudo)...\n", dir)
		if err := exec.Command("sudo", "mkdir", "-p", dir).Run(); err != nil {
			return fmt.Errorf("failed to create %s: %w", dir, err)
		}
	}

	return writeSudoFile(path, content)
}

// removeResolverConfig removes the OS-level DNS resolver config and port forward.
func removeResolverConfig() error {
	path := resolverFilePath()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil // Already removed.
	}
	fmt.Printf("Removing %s (requires sudo)...\n", path)
	if err := exec.Command("sudo", "rm", "-f", path).Run(); err != nil {
		return fmt.Errorf("failed to remove %s: %w", path, err)
	}

	if runtime.GOOS == "darwin" {
		stopPortProxy()
	}

	// If systemd-resolved, restart it.
	if runtime.GOOS == "linux" && strings.Contains(path, "systemd") {
		exec.Command("sudo", "systemctl", "restart", "systemd-resolved").Run()
	}
	return nil
}

// writeSudoFile writes content to path using sudo tee.
func writeSudoFile(path, content string) error {
	fmt.Printf("Writing %s (requires sudo)...\n", path)
	cmd := exec.Command("sudo", "tee", path)
	cmd.Stdin = strings.NewReader(content)
	cmd.Stdout = nil // suppress tee's stdout
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to write %s: %w", path, err)
	}
	return nil
}
