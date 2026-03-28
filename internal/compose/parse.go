// Package compose parses Docker Compose files into LOKA service components.
package compose

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// ComposeFile represents a docker-compose.yml.
type ComposeFile struct {
	Version  string                    `yaml:"version,omitempty"`
	Services map[string]ComposeService `yaml:"services"`
}

// ComposeService represents a single service in a compose file.
type ComposeService struct {
	Image       string      `yaml:"image,omitempty"`
	Command     interface{} `yaml:"command,omitempty"`      // string or []string
	Entrypoint  interface{} `yaml:"entrypoint,omitempty"`   // string or []string
	Ports       []string    `yaml:"ports,omitempty"`        // "host:container" or "container"
	Environment interface{} `yaml:"environment,omitempty"`  // map or []string
	EnvFile     interface{} `yaml:"env_file,omitempty"`     // string or []string
	Volumes     []string    `yaml:"volumes,omitempty"`
	DependsOn   interface{} `yaml:"depends_on,omitempty"`   // []string or map
	WorkingDir  string      `yaml:"working_dir,omitempty"`
	Restart     string      `yaml:"restart,omitempty"`
}

// Component is the LOKA-native representation of a compose service.
type Component struct {
	Name      string
	Image     string
	Command   string
	Port      int               // Primary container port.
	HostPort  int               // Host port mapping (0 = internal only).
	Env       map[string]string
	Volumes   []string
	DependsOn []string
	Workdir   string
}

// Parse reads a docker-compose.yml file and returns the parsed structure.
func Parse(path string) (*ComposeFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read compose file: %w", err)
	}
	var cf ComposeFile
	if err := yaml.Unmarshal(data, &cf); err != nil {
		return nil, fmt.Errorf("parse compose file: %w", err)
	}
	if len(cf.Services) == 0 {
		return nil, fmt.Errorf("no services defined in compose file")
	}
	return &cf, nil
}

// ToComponents converts a ComposeFile to a list of LOKA components.
func (cf *ComposeFile) ToComponents() []Component {
	var components []Component
	for name, svc := range cf.Services {
		c := Component{
			Name:      name,
			Image:     svc.Image,
			Command:   parseCommand(svc.Command),
			Env:       parseEnvironment(svc.Environment),
			Volumes:   svc.Volumes,
			DependsOn: parseDependsOn(svc.DependsOn),
			Workdir:   svc.WorkingDir,
		}
		c.Port, c.HostPort = parsePorts(svc.Ports)
		components = append(components, c)
	}
	return components
}

// DeployOrder returns component names in topological order (respecting depends_on).
func (cf *ComposeFile) DeployOrder() []string {
	components := cf.ToComponents()

	// Build dependency graph.
	deps := make(map[string][]string)
	all := make(map[string]bool)
	for _, c := range components {
		all[c.Name] = true
		deps[c.Name] = c.DependsOn
	}

	// Kahn's algorithm.
	inDegree := make(map[string]int)
	for name := range all {
		inDegree[name] = 0
	}
	for _, c := range components {
		for _, dep := range c.DependsOn {
			inDegree[c.Name]++
			_ = dep
		}
	}

	var queue []string
	for name, deg := range inDegree {
		if deg == 0 {
			queue = append(queue, name)
		}
	}

	var order []string
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		order = append(order, name)
		for other, depList := range deps {
			for _, dep := range depList {
				if dep == name {
					inDegree[other]--
					if inDegree[other] == 0 {
						queue = append(queue, other)
					}
				}
			}
		}
	}

	// Add any remaining (cycles or disconnected).
	for name := range all {
		found := false
		for _, o := range order {
			if o == name {
				found = true
				break
			}
		}
		if !found {
			order = append(order, name)
		}
	}

	return order
}

// FindComposeFile looks for docker-compose.yml or compose.yml in a directory.
func FindComposeFile(dir string) string {
	candidates := []string{
		dir + "/docker-compose.yml",
		dir + "/docker-compose.yaml",
		dir + "/compose.yml",
		dir + "/compose.yaml",
	}
	for _, path := range candidates {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}

// parseCommand handles both string and []string command formats.
func parseCommand(cmd interface{}) string {
	if cmd == nil {
		return ""
	}
	switch v := cmd.(type) {
	case string:
		return v
	case []interface{}:
		parts := make([]string, len(v))
		for i, p := range v {
			parts[i] = fmt.Sprint(p)
		}
		return strings.Join(parts, " ")
	}
	return fmt.Sprint(cmd)
}

// parseEnvironment handles both map and list environment formats.
func parseEnvironment(env interface{}) map[string]string {
	result := make(map[string]string)
	if env == nil {
		return result
	}
	switch v := env.(type) {
	case map[string]interface{}:
		for key, val := range v {
			result[key] = fmt.Sprint(val)
		}
	case []interface{}:
		for _, item := range v {
			s := fmt.Sprint(item)
			parts := strings.SplitN(s, "=", 2)
			if len(parts) == 2 {
				result[parts[0]] = parts[1]
			} else {
				result[parts[0]] = ""
			}
		}
	}
	return result
}

// parsePorts extracts the primary container port and host port from compose ports.
func parsePorts(ports []string) (containerPort, hostPort int) {
	if len(ports) == 0 {
		return 0, 0
	}
	// Use the first port mapping.
	p := ports[0]
	parts := strings.Split(p, ":")
	switch len(parts) {
	case 1:
		// "3000" — container port only, no host mapping.
		containerPort, _ = strconv.Atoi(strings.TrimSpace(parts[0]))
	case 2:
		// "8080:3000" — host:container.
		hostPort, _ = strconv.Atoi(strings.TrimSpace(parts[0]))
		containerPort, _ = strconv.Atoi(strings.TrimSpace(parts[1]))
	case 3:
		// "127.0.0.1:8080:3000" — host_ip:host:container.
		hostPort, _ = strconv.Atoi(strings.TrimSpace(parts[1]))
		containerPort, _ = strconv.Atoi(strings.TrimSpace(parts[2]))
	}
	return containerPort, hostPort
}

// parseDependsOn handles both []string and map formats.
func parseDependsOn(dep interface{}) []string {
	if dep == nil {
		return nil
	}
	switch v := dep.(type) {
	case []interface{}:
		result := make([]string, len(v))
		for i, d := range v {
			result[i] = fmt.Sprint(d)
		}
		return result
	case map[string]interface{}:
		var result []string
		for name := range v {
			result = append(result, name)
		}
		return result
	}
	return nil
}
