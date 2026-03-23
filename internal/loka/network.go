package loka

import (
	"fmt"
	"net"
	"strconv"
	"strings"
)

// NetworkPolicy defines inbound and outbound network access rules for a session.
// Rules are evaluated top-to-bottom; first match wins.
// If no rules match, the default action applies.
type NetworkPolicy struct {
	// Outbound rules control traffic FROM the session TO external targets.
	Outbound NetworkRuleSet `json:"outbound"`

	// Inbound rules control traffic FROM external sources TO the session.
	Inbound NetworkRuleSet `json:"inbound"`
}

// NetworkRuleSet is an ordered list of rules with a default action.
type NetworkRuleSet struct {
	// DefaultAction is "allow" or "deny" when no rules match.
	// Default: "deny" (deny by default, allow by rule).
	DefaultAction NetworkAction `json:"default_action"`

	// Rules are evaluated in order; first match wins.
	Rules []NetworkRule `json:"rules"`
}

// NetworkAction is the action to take for a rule.
type NetworkAction string

const (
	NetworkAllow NetworkAction = "allow"
	NetworkDeny  NetworkAction = "deny"
)

// NetworkRule defines a single network access rule.
type NetworkRule struct {
	// Action: "allow" or "deny".
	Action NetworkAction `json:"action"`

	// Target matches destination (outbound) or source (inbound).
	// Supports:
	//   - CIDR:      "10.0.0.0/8", "192.168.1.0/24", "0.0.0.0/0" (any)
	//   - Single IP: "93.184.216.34"
	//   - Domain:    "example.com" (exact match)
	//   - Wildcard:  "*.example.com" (subdomain match)
	//   - Special:   "any" (matches everything)
	Target string `json:"target"`

	// Ports restricts the rule to specific ports.
	// Supports:
	//   - Single: "80", "443"
	//   - Range:  "8000-9000"
	//   - List:   "80,443,8080"
	//   - Any:    "" or "*" (all ports)
	Ports string `json:"ports,omitempty"`

	// Protocol: "tcp", "udp", or "" (both).
	Protocol string `json:"protocol,omitempty"`

	// Description is a human-readable note.
	Description string `json:"description,omitempty"`
}

// DefaultNetworkPolicy returns a deny-all policy (most restrictive).
func DefaultNetworkPolicy() NetworkPolicy {
	return NetworkPolicy{
		Outbound: NetworkRuleSet{DefaultAction: NetworkDeny},
		Inbound:  NetworkRuleSet{DefaultAction: NetworkDeny},
	}
}

// AllowAllNetworkPolicy returns a policy that allows all traffic.
func AllowAllNetworkPolicy() NetworkPolicy {
	return NetworkPolicy{
		Outbound: NetworkRuleSet{DefaultAction: NetworkAllow},
		Inbound:  NetworkRuleSet{DefaultAction: NetworkAllow},
	}
}

// ModeNetworkPolicies maps execution modes to their default network policies.
var ModeNetworkPolicies = map[ExecMode]NetworkPolicy{
	ModeExplore: {
		Outbound: NetworkRuleSet{
			DefaultAction: NetworkDeny,
			Rules: []NetworkRule{
				{Action: NetworkAllow, Target: "any", Ports: "53", Protocol: "udp", Description: "DNS lookups"},
			},
		},
		Inbound: NetworkRuleSet{DefaultAction: NetworkDeny},
	},
	ModeExecute: {
		Outbound: NetworkRuleSet{
			DefaultAction: NetworkAllow,
			Rules: []NetworkRule{
				{Action: NetworkDeny, Target: "169.254.169.254", Description: "Block cloud metadata"},
				{Action: NetworkDeny, Target: "10.0.0.0/8", Description: "Block private networks"},
				{Action: NetworkDeny, Target: "172.16.0.0/12", Description: "Block private networks"},
				{Action: NetworkDeny, Target: "192.168.0.0/16", Description: "Block private networks"},
			},
		},
		Inbound: NetworkRuleSet{DefaultAction: NetworkDeny},
	},
	ModeAsk: {
		Outbound: NetworkRuleSet{
			DefaultAction: NetworkDeny,
			Rules: []NetworkRule{
				{Action: NetworkAllow, Target: "any", Ports: "53", Protocol: "udp", Description: "DNS lookups"},
				{Action: NetworkAllow, Target: "any", Ports: "443", Protocol: "tcp", Description: "HTTPS"},
			},
		},
		Inbound: NetworkRuleSet{DefaultAction: NetworkDeny},
	},
}

// ── Matching ────────────────────────────────────────────

// Evaluate checks if a connection is allowed by the rule set.
// Returns the action and the matching rule (nil if default action was used).
func (rs *NetworkRuleSet) Evaluate(ip string, port int, protocol string) (NetworkAction, *NetworkRule) {
	for i := range rs.Rules {
		rule := &rs.Rules[i]
		if rule.Matches(ip, port, protocol) {
			return rule.Action, rule
		}
	}
	action := rs.DefaultAction
	if action == "" {
		action = NetworkDeny
	}
	return action, nil
}

// Matches checks if a connection matches this rule.
func (r *NetworkRule) Matches(ip string, port int, protocol string) bool {
	// Check protocol.
	if r.Protocol != "" && protocol != "" && r.Protocol != protocol {
		return false
	}

	// Check target.
	if !r.matchesTarget(ip) {
		return false
	}

	// Check ports.
	if !r.matchesPorts(port) {
		return false
	}

	return true
}

func (r *NetworkRule) matchesTarget(ip string) bool {
	target := r.Target

	// Special: match anything.
	if target == "any" || target == "*" || target == "0.0.0.0/0" || target == "::/0" {
		return true
	}

	// CIDR match.
	if strings.Contains(target, "/") {
		_, cidr, err := net.ParseCIDR(target)
		if err == nil {
			parsedIP := net.ParseIP(ip)
			if parsedIP != nil {
				return cidr.Contains(parsedIP)
			}
		}
		return false
	}

	// Single IP exact match.
	if net.ParseIP(target) != nil {
		return target == ip
	}

	// Domain match (wildcard or exact).
	// IP parameter might be the resolved IP or the original domain.
	if strings.HasPrefix(target, "*.") {
		// Wildcard: *.example.com matches sub.example.com, a.b.example.com
		suffix := target[1:] // ".example.com"
		return strings.HasSuffix(ip, suffix) || ip == target[2:]
	}

	// Exact domain match.
	return ip == target
}

func (r *NetworkRule) matchesPorts(port int) bool {
	if port == 0 {
		return true // No port specified in the connection.
	}

	portSpec := r.Ports
	if portSpec == "" || portSpec == "*" {
		return true // Match any port.
	}

	// Parse comma-separated port specs.
	for _, spec := range strings.Split(portSpec, ",") {
		spec = strings.TrimSpace(spec)

		// Range: "8000-9000"
		if strings.Contains(spec, "-") {
			parts := strings.SplitN(spec, "-", 2)
			lo, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
			hi, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
			if err1 == nil && err2 == nil && port >= lo && port <= hi {
				return true
			}
			continue
		}

		// Single port: "443"
		p, err := strconv.Atoi(spec)
		if err == nil && port == p {
			return true
		}
	}

	return false
}

// ── Helpers ─────────────────────────────────────────────

// IsAllowed is a convenience method for checking outbound access.
func (np *NetworkPolicy) IsOutboundAllowed(ip string, port int, protocol string) (bool, string) {
	action, rule := np.Outbound.Evaluate(ip, port, protocol)
	if action == NetworkAllow {
		return true, ""
	}
	reason := "denied by default policy"
	if rule != nil {
		reason = fmt.Sprintf("denied by rule: %s", rule.Description)
		if rule.Description == "" {
			reason = fmt.Sprintf("denied: target=%s ports=%s", rule.Target, rule.Ports)
		}
	}
	return false, reason
}

// IsInboundAllowed checks if inbound access is allowed.
func (np *NetworkPolicy) IsInboundAllowed(ip string, port int, protocol string) (bool, string) {
	action, rule := np.Inbound.Evaluate(ip, port, protocol)
	if action == NetworkAllow {
		return true, ""
	}
	reason := "denied by default policy"
	if rule != nil {
		reason = fmt.Sprintf("denied by rule: %s", rule.Description)
	}
	return false, reason
}

// ── iptables Generation ─────────────────────────────────

// GenerateIptablesRules generates iptables commands for a VM's TAP interface.
// This is called on the HOST to configure the VM's network access.
func (np *NetworkPolicy) GenerateIptablesRules(tapInterface string) []string {
	var rules []string

	chain := fmt.Sprintf("LOKA_%s", strings.ReplaceAll(tapInterface, "-", "_"))

	// Create chain.
	rules = append(rules,
		fmt.Sprintf("iptables -N %s 2>/dev/null || iptables -F %s", chain, chain),
	)

	// Outbound rules (FORWARD chain, traffic going OUT of the VM).
	for _, rule := range np.Outbound.Rules {
		rules = append(rules, rule.toIptables(chain, "FORWARD", "-o", tapInterface)...)
	}

	// Default outbound action.
	if np.Outbound.DefaultAction == NetworkDeny {
		rules = append(rules,
			fmt.Sprintf("iptables -A %s -o %s -j DROP", chain, tapInterface),
		)
	}

	// Inbound rules (FORWARD chain, traffic going IN to the VM).
	for _, rule := range np.Inbound.Rules {
		rules = append(rules, rule.toIptables(chain, "FORWARD", "-i", tapInterface)...)
	}

	// Default inbound action.
	if np.Inbound.DefaultAction == NetworkDeny {
		rules = append(rules,
			fmt.Sprintf("iptables -A %s -i %s -j DROP", chain, tapInterface),
		)
	}

	// Jump from FORWARD to our chain.
	rules = append(rules,
		fmt.Sprintf("iptables -I FORWARD -o %s -j %s", tapInterface, chain),
		fmt.Sprintf("iptables -I FORWARD -i %s -j %s", tapInterface, chain),
	)

	return rules
}

func (r *NetworkRule) toIptables(chain, iptChain, dirFlag, iface string) []string {
	var rules []string

	target := "ACCEPT"
	if r.Action == NetworkDeny {
		target = "DROP"
	}

	base := fmt.Sprintf("iptables -A %s %s %s", chain, dirFlag, iface)

	// Add destination/source match.
	if r.Target != "any" && r.Target != "*" {
		if strings.Contains(r.Target, "/") || net.ParseIP(r.Target) != nil {
			if dirFlag == "-o" {
				base += fmt.Sprintf(" -d %s", r.Target)
			} else {
				base += fmt.Sprintf(" -s %s", r.Target)
			}
		}
		// Domain-based rules need ipset or DNS resolution — skipped for iptables.
	}

	// Add protocol.
	if r.Protocol != "" {
		base += fmt.Sprintf(" -p %s", r.Protocol)
	}

	// Add ports.
	if r.Ports != "" && r.Ports != "*" {
		if r.Protocol == "" {
			// Need to specify protocol for port matching.
			// Generate both TCP and UDP rules.
			for _, proto := range []string{"tcp", "udp"} {
				portRule := base + fmt.Sprintf(" -p %s", proto)
				if strings.Contains(r.Ports, ",") || strings.Contains(r.Ports, "-") {
					portRule += fmt.Sprintf(" -m multiport --dports %s", r.Ports)
				} else {
					portRule += fmt.Sprintf(" --dport %s", r.Ports)
				}
				rules = append(rules, portRule+" -j "+target)
			}
			return rules
		}

		if strings.Contains(r.Ports, ",") || strings.Contains(r.Ports, "-") {
			base += fmt.Sprintf(" -m multiport --dports %s", r.Ports)
		} else {
			base += fmt.Sprintf(" --dport %s", r.Ports)
		}
	}

	rules = append(rules, base+" -j "+target)
	return rules
}
