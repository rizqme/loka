package main

import (
	crypto_tls "crypto/tls"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"github.com/vyprai/loka/pkg/lokaapi"
)

func newSetupCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Set up LOKA infrastructure",
		Long: `Set up LOKA on local or cloud infrastructure.

  loka setup local --name dev
  loka setup aws --name prod --region us-east-1 --workers 3
  loka setup vm --name staging --cp 10.0.0.1
  loka setup apply prod.yml         # Deploy from YAML file
  loka worker add 10.0.0.5          # Add a worker
  loka worker remove 10.0.0.5       # Remove a worker
  loka list                         # List all servers
  loka use prod                     # Switch active server`,
	}
	cmd.AddCommand(
		newDeployFileCmd(),
		newDeployExportCmd(),
		newDeployCloudCmd("aws", "Deploy to AWS (EC2)", deployAWS),
		newDeployCloudCmd("gcp", "Deploy to Google Cloud", deployGCP),
		newDeployCloudCmd("azure", "Deploy to Azure", deployAzure),
		newDeployCloudCmd("do", "Deploy to DigitalOcean", deployDigitalOcean),
		newDeployCloudCmd("ovh", "Deploy to OVH", deployOVH),
		newDeployVMCmd(),
		newDeployLocalCmd(),
		newDeployRenameCmd(),
		newDeployDownCmd(),
		newDeployStatusCmd(),
		newDeployDestroyCmd(),
	)
	return cmd
}

type deployFunc func(opts deployOpts) error
type deployOpts struct {
	Name, Provider, Region, Zone, Project, InstanceType, SSHKey string
	Workers                                                     int
}

func newDeployCloudCmd(provider, desc string, fn deployFunc) *cobra.Command {
	var opts deployOpts
	opts.Provider = provider
	cmd := &cobra.Command{
		Use:   provider,
		Short: desc,
		RunE: func(cmd *cobra.Command, args []string) error {
			if opts.Name == "" { opts.Name = provider }
			if err := fn(opts); err != nil { return err }
			store, _ := loadDeployments()
			store.Add(Deployment{Name: opts.Name, Provider: provider, Region: opts.Region, Endpoint: "http://<pending>:8080", Workers: opts.Workers, Status: "provisioning", CreatedAt: time.Now()})
			store.Active = opts.Name
			saveDeployments(store)
			fmt.Printf("\nServer %q created and set as active.\n", opts.Name)
			return nil
		},
	}
	cmd.Flags().StringVar(&opts.Name, "name", "", "Server name (default: provider)")
	cmd.Flags().StringVar(&opts.Region, "region", "", "Region")
	cmd.Flags().StringVar(&opts.Zone, "zone", "", "Zone")
	cmd.Flags().StringVar(&opts.Project, "project", "", "Project ID (GCP)")
	cmd.Flags().IntVar(&opts.Workers, "workers", 1, "Workers")
	cmd.Flags().StringVar(&opts.InstanceType, "instance-type", "", "Instance type")
	cmd.Flags().StringVar(&opts.SSHKey, "ssh-key", "", "SSH key")
	return cmd
}

func newDeployLocalCmd() *cobra.Command {
	var (name string; foreground bool)
	cmd := &cobra.Command{
		Use: "local", Short: "Start LOKA locally",
		Long: `Start LOKA on your local machine.

On Linux: runs lokad directly.
On macOS: runs lokad inside a VM (ports forwarded to localhost).

The VM is created automatically. If it doesn't exist yet, run the installer first:
  curl -fsSL https://vyprai.github.io/loka/install.sh | bash`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if name == "" { name = "local" }

			isMacOS := runtime.GOOS == "darwin"

			if isMacOS {
				return deployLocalMacOS(name, foreground)
			}
			return deployLocalLinux(name, foreground)
		},
	}
	cmd.Flags().StringVar(&name, "name", "local", "Server name")
	cmd.Flags().BoolVarP(&foreground, "foreground", "f", false, "Foreground")
	return cmd
}

func deployLocalLinux(name string, foreground bool) error {
	lokad, err := findBinary("lokad")
	if err != nil { return err }

	// Auto-TLS generates certs here.
	caCertPath := "/tmp/loka-data/artifacts/tls/ca.crt"
	store, _ := loadDeployments()
	store.Add(Deployment{
		Name: name, Provider: "local", Endpoint: "http://localhost:6840",
		Workers: 1, Status: "running", CreatedAt: time.Now(),
		Meta: map[string]string{"ca_cert": caCertPath},
	})
	store.Active = name
	saveDeployments(store)

	if foreground {
		fmt.Printf("Starting %q (foreground)...\n", name)
		p := exec.Command(lokad); p.Env = os.Environ(); p.Stdout = os.Stdout; p.Stderr = os.Stderr; p.Stdin = os.Stdin
		return p.Run()
	}
	p := exec.Command(lokad); p.Env = os.Environ()
	if err := p.Start(); err != nil { return err }
	fmt.Printf("LOKA %q started (pid %d)\n", name, p.Process.Pid)
	fmt.Printf("  Endpoint: http://localhost:6840\n")
	fmt.Printf("  Stop:     loka setup down\n")
	return nil
}

func deployLocalMacOS(name string, foreground bool) error {
	// Find lokad binary — it embeds the Apple VZ hypervisor (lokavm library).
	lokadPath := findLokad()
	if lokadPath == "" {
		return fmt.Errorf("lokad binary not found. Run: make build")
	}

	home, _ := os.UserHomeDir()
	dataDir := filepath.Join(home, ".loka")
	os.MkdirAll(dataDir, 0755)

	if foreground {
		cmd := exec.Command(lokadPath, "--data-dir", dataDir)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}

	// Start lokad in background.
	fmt.Print("Starting LOKA...")
	logPath := filepath.Join(dataDir, "lokad.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		return fmt.Errorf("create log file: %w", err)
	}
	cmd := exec.Command(lokadPath, "--data-dir", dataDir)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		logFile.Close()
		return fmt.Errorf("start lokad: %w", err)
	}
	logFile.Close()

	// Wait for health — try HTTPS first (auto-TLS), then HTTP.
	fmt.Print(" waiting...")
	ready := false
	insecureClient := &http.Client{Transport: &http.Transport{
		TLSClientConfig: &crypto_tls.Config{InsecureSkipVerify: true},
	}}
	for i := 0; i < 60; i++ {
		// Try HTTPS (lokad auto-generates TLS certs).
		resp, err := insecureClient.Get("https://localhost:6840/api/v1/health")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				ready = true
				break
			}
		}
		// Fall back to HTTP (if TLS disabled).
		resp, err = http.Get("http://localhost:6840/api/v1/health")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				ready = true
				break
			}
		}
		time.Sleep(1 * time.Second)
		fmt.Print(".")
	}
	if !ready {
		fmt.Println(" FAILED")
		logOut, _ := os.ReadFile(logPath)
		lines := strings.Split(string(logOut), "\n")
		if len(lines) > 10 {
			lines = lines[len(lines)-10:]
		}
		return fmt.Errorf("lokad did not become healthy:\n%s", strings.Join(lines, "\n"))
	}
	fmt.Println(" ready!")

	// Fetch CA cert from the server's /ca.crt endpoint.
	fmt.Print("  Fetching CA certificate...")
	caCertLocalPath := ""
	fetched, fetchErr := fetchCACert("https://localhost:6840")
	if fetchErr == nil && fetched != "" {
		caCertLocalPath = fetched
		fmt.Printf(" %s\n", caCertLocalPath)
	} else {
		fmt.Println(" not available")
	}

	// Save deployment.
	store, _ := loadDeployments()
	meta := map[string]string{
		"runtime": "lokad",
		"pid":     fmt.Sprint(cmd.Process.Pid),
	}
	if caCertLocalPath != "" {
		meta["ca_cert"] = caCertLocalPath
	}
	store.Add(Deployment{
		Name: name, Provider: "local", Endpoint: "https://localhost:6840",
		Workers: 1, Status: "running", CreatedAt: time.Now(),
		Meta: meta,
	})
	store.Active = name
	saveDeployments(store)

	fmt.Printf("LOKA started\n  Endpoint: https://localhost:6840\n  Stop: loka setup down\n")
	return nil
}

// findLokad searches common locations for the lokad binary.
// Checks sibling directory first (for ./bin/lokad next to ./bin/loka).
func findLokad() string {
	if self, err := os.Executable(); err == nil {
		sibling := filepath.Join(filepath.Dir(self), "lokad")
		if _, err := os.Stat(sibling); err == nil {
			return sibling
		}
	}
	candidates := []string{
		"lokad",
		"/usr/local/bin/lokad",
		filepath.Join(os.Getenv("HOME"), ".loka", "bin", "lokad"),
	}
	for _, p := range candidates {
		if path, err := exec.LookPath(p); err == nil {
			return path
		}
	}
	return ""
}

func newListCmd() *cobra.Command {
	return &cobra.Command{
		Use: "list", Short: "List servers", Aliases: []string{"ls"},
		RunE: func(cmd *cobra.Command, args []string) error {
			store, _ := loadDeployments()
			if len(store.Deployments) == 0 {
				fmt.Println("No servers. Set one up: loka setup local --name dev")
				return nil
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "  NAME\tPROVIDER\tENDPOINT\tWORKERS\tSTATUS\tCREATED")
			for _, d := range store.Deployments {
				a := " "; if d.Name == store.Active { a = "*" }
				fmt.Fprintf(w, "%s %s\t%s\t%s\t%d\t%s\t%s\n", a, d.Name, d.Provider, d.Endpoint, d.Workers, d.Status, d.CreatedAt.Format("2006-01-02"))
			}
			w.Flush()
			return nil
		},
	}
}


func newDeployRenameCmd() *cobra.Command {
	return &cobra.Command{
		Use: "rename <old> <new>", Short: "Rename a server", Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, _ := loadDeployments()
			d := store.Get(args[0])
			if d == nil { return fmt.Errorf("server %q not found", args[0]) }
			old := d.Name; d.Name = args[1]
			if store.Active == old { store.Active = args[1] }
			saveDeployments(store)
			fmt.Printf("Renamed %q -> %q\n", old, args[1])
			return nil
		},
	}
}

func newDeployDownCmd() *cobra.Command {
	return &cobra.Command{
		Use: "down [name]", Short: "Stop a server", Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, _ := loadDeployments()
			name := store.Active; if len(args) > 0 { name = args[0] }
			d := store.Get(name)
			if d == nil { return fmt.Errorf("server %q not found", name) }
			if d.Provider == "local" {
				rt := ""
				if d.Meta != nil {
					rt = d.Meta["runtime"]
				}
				if rt == "lokad" || rt == "lokavm" || rt == "vm" {
					// Kill lokad process.
					if pidStr, ok := d.Meta["pid"]; ok && pidStr != "" {
						exec.Command("kill", pidStr).Run()
					} else {
						exec.Command("pkill", "-f", "lokad").Run()
					}
					fmt.Printf("LOKA %q stopped\n", name)
				} else {
					out, _ := exec.Command("pgrep", "-f", "lokad").Output()
					if len(out) > 0 {
						exec.Command("kill", strings.TrimSpace(string(out))).Run()
						fmt.Printf("LOKA %q stopped\n", name)
					} else {
						fmt.Printf("%q is not running\n", name)
					}
				}
			}
			d.Status = "stopped"; saveDeployments(store)
			return nil
		},
	}
}

func newDeployStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use: "status [name]", Short: "Show server status", Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, _ := loadDeployments()
			name := store.Active; if len(args) > 0 { name = args[0] }
			d := store.Get(name)
			if d == nil { return fmt.Errorf("server %q not found", name) }
			fmt.Printf("Name:     %s\n", d.Name)
			fmt.Printf("Provider: %s\n", d.Provider)
			fmt.Printf("Endpoint: %s\n", d.Endpoint)
			fmt.Printf("Workers:  %d\n", d.Workers)
			fmt.Printf("Status:   %s\n", d.Status)
			c := lokaapi.NewClient(d.Endpoint, token)
			var h struct{ Status string `json:"status"`; WorkersReady int `json:"workers_ready"` }
			if err := c.Raw(cmd.Context(), "GET", "/api/v1/health", nil, &h); err == nil {
				fmt.Printf("Live:     %s (%d workers ready)\n", h.Status, h.WorkersReady)
			}
			return nil
		},
	}
}

func newDeployDestroyCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use: "destroy [name]", Short: "Destroy a server", Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, _ := loadDeployments()
			name := store.Active; if len(args) > 0 { name = args[0] }
			d := store.Get(name)
			if d == nil { return fmt.Errorf("server %q not found", name) }
			if !force {
				fmt.Printf("Destroy %q? (yes/no): ", name)
				var c string; fmt.Scanln(&c)
				if c != "yes" { fmt.Println("Aborted."); return nil }
			}
			if d.Provider == "local" {
				out, _ := exec.Command("pgrep", "-f", "lokad").Output()
				if len(out) > 0 { exec.Command("kill", strings.TrimSpace(string(out))).Run() }
			}
			store.Remove(name); saveDeployments(store)
			fmt.Printf("Server %q destroyed\n", name)
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "Skip confirmation")
	return cmd
}

func deployAWS(o deployOpts) error { r:=o.Region; if r=="" {r="us-east-1"}; fmt.Printf("Deploying %q to AWS (%s, %d workers)...\n", o.Name, r, o.Workers); return nil }
func deployGCP(o deployOpts) error { z:=o.Zone; if z=="" {z="us-central1-a"}; fmt.Printf("Deploying %q to GCP (%s, %d workers)...\n", o.Name, z, o.Workers); return nil }
func deployAzure(o deployOpts) error { fmt.Printf("Deploying %q to Azure (%d workers)...\n", o.Name, o.Workers); return nil }
func deployDigitalOcean(o deployOpts) error { fmt.Printf("Deploying %q to DigitalOcean (%d workers)...\n", o.Name, o.Workers); return nil }
func deployOVH(o deployOpts) error { fmt.Printf("Deploying %q to OVH (%d workers)...\n", o.Name, o.Workers); return nil }

func findBinary(name string) (string, error) {
	if p, err := exec.LookPath(name); err == nil { return p, nil }
	for _, p := range []string{"./bin/" + name, "/usr/local/bin/" + name} { if _, err := os.Stat(p); err == nil { return p, nil } }
	return "", fmt.Errorf("%s not found", name)
}
