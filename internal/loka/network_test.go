package loka

import (
	"strings"
	"testing"
)

// ── Port Matching ───────────────────────────────────────

func TestNetworkRule_MatchesPorts(t *testing.T) {
	tests := []struct {
		ports  string
		port   int
		expect bool
	}{
		{"", 80, true},           // Empty = any.
		{"*", 443, true},         // Wildcard = any.
		{"80", 80, true},         // Exact match.
		{"80", 443, false},       // No match.
		{"80,443", 80, true},     // List match first.
		{"80,443", 443, true},    // List match second.
		{"80,443", 8080, false},  // List no match.
		{"8000-9000", 8080, true},  // Range match.
		{"8000-9000", 7999, false}, // Below range.
		{"8000-9000", 9001, false}, // Above range.
		{"80,443,8000-9000", 8500, true}, // Mixed list+range.
		{"80,443,8000-9000", 22, false},
	}

	for _, tt := range tests {
		r := NetworkRule{Action: NetworkAllow, Target: "any", Ports: tt.ports}
		got := r.matchesPorts(tt.port)
		if got != tt.expect {
			t.Errorf("ports=%q port=%d: got %v, want %v", tt.ports, tt.port, got, tt.expect)
		}
	}
}

// ── Target Matching ─────────────────────────────────────

func TestNetworkRule_MatchesTarget(t *testing.T) {
	tests := []struct {
		target string
		ip     string
		expect bool
	}{
		{"any", "1.2.3.4", true},
		{"*", "1.2.3.4", true},
		{"0.0.0.0/0", "1.2.3.4", true},

		// Exact IP.
		{"93.184.216.34", "93.184.216.34", true},
		{"93.184.216.34", "93.184.216.35", false},

		// CIDR.
		{"10.0.0.0/8", "10.0.0.1", true},
		{"10.0.0.0/8", "10.255.255.255", true},
		{"10.0.0.0/8", "11.0.0.1", false},
		{"192.168.1.0/24", "192.168.1.100", true},
		{"192.168.1.0/24", "192.168.2.100", false},
		{"172.16.0.0/12", "172.16.0.1", true},
		{"172.16.0.0/12", "172.31.255.255", true},
		{"172.16.0.0/12", "172.32.0.1", false},

		// Domain exact match.
		{"example.com", "example.com", true},
		{"example.com", "sub.example.com", false},

		// Wildcard domain.
		{"*.example.com", "sub.example.com", true},
		{"*.example.com", "a.b.example.com", true},
		{"*.example.com", "example.com", true},
		{"*.example.com", "evil.com", false},

		// IPv6.
		{"::1", "::1", true},
		{"::/0", "2001:db8::1", true},
	}

	for _, tt := range tests {
		r := NetworkRule{Action: NetworkAllow, Target: tt.target}
		got := r.matchesTarget(tt.ip)
		if got != tt.expect {
			t.Errorf("target=%q ip=%q: got %v, want %v", tt.target, tt.ip, got, tt.expect)
		}
	}
}

// ── Protocol Matching ───────────────────────────────────

func TestNetworkRule_MatchesProtocol(t *testing.T) {
	// TCP rule, TCP connection.
	r1 := NetworkRule{Action: NetworkAllow, Target: "any", Protocol: "tcp"}
	if !r1.Matches("1.2.3.4", 80, "tcp") {
		t.Error("tcp rule should match tcp")
	}
	if r1.Matches("1.2.3.4", 80, "udp") {
		t.Error("tcp rule should not match udp")
	}

	// No protocol = matches both.
	r2 := NetworkRule{Action: NetworkAllow, Target: "any"}
	if !r2.Matches("1.2.3.4", 80, "tcp") {
		t.Error("no-protocol rule should match tcp")
	}
	if !r2.Matches("1.2.3.4", 80, "udp") {
		t.Error("no-protocol rule should match udp")
	}
}

// ── Rule Set Evaluation ─────────────────────────────────

func TestNetworkRuleSet_Evaluate(t *testing.T) {
	rs := NetworkRuleSet{
		DefaultAction: NetworkDeny,
		Rules: []NetworkRule{
			{Action: NetworkAllow, Target: "any", Ports: "53", Protocol: "udp", Description: "DNS"},
			{Action: NetworkAllow, Target: "any", Ports: "443", Protocol: "tcp", Description: "HTTPS"},
			{Action: NetworkDeny, Target: "10.0.0.0/8", Description: "Block private"},
		},
	}

	// DNS allowed.
	action, rule := rs.Evaluate("8.8.8.8", 53, "udp")
	if action != NetworkAllow || rule.Description != "DNS" {
		t.Errorf("DNS should be allowed, got %s", action)
	}

	// HTTPS allowed.
	action, rule = rs.Evaluate("93.184.216.34", 443, "tcp")
	if action != NetworkAllow || rule.Description != "HTTPS" {
		t.Errorf("HTTPS should be allowed, got %s", action)
	}

	// HTTP denied (port 80 not in rules → default deny).
	action, _ = rs.Evaluate("93.184.216.34", 80, "tcp")
	if action != NetworkDeny {
		t.Errorf("HTTP should be denied, got %s", action)
	}

	// Private IP blocked by explicit rule (even on allowed port).
	// Note: the DNS rule matches first because it's "any" target + port 53.
	action, _ = rs.Evaluate("10.0.0.1", 80, "tcp")
	if action != NetworkDeny {
		t.Errorf("private IP on port 80 should be denied, got %s", action)
	}
}

func TestNetworkRuleSet_DefaultAllow(t *testing.T) {
	rs := NetworkRuleSet{
		DefaultAction: NetworkAllow,
		Rules: []NetworkRule{
			{Action: NetworkDeny, Target: "169.254.169.254", Description: "Block metadata"},
		},
	}

	// Normal traffic allowed by default.
	action, _ := rs.Evaluate("93.184.216.34", 443, "tcp")
	if action != NetworkAllow {
		t.Error("should be allowed by default")
	}

	// Metadata endpoint blocked.
	action, rule := rs.Evaluate("169.254.169.254", 80, "tcp")
	if action != NetworkDeny || rule.Description != "Block metadata" {
		t.Error("metadata should be blocked")
	}
}

// ── Full Policy Evaluation ──────────────────────────────

func TestNetworkPolicy_IsOutboundAllowed(t *testing.T) {
	policy := NetworkPolicy{
		Outbound: NetworkRuleSet{
			DefaultAction: NetworkDeny,
			Rules: []NetworkRule{
				{Action: NetworkAllow, Target: "*.pypi.org", Ports: "443", Protocol: "tcp", Description: "PyPI"},
				{Action: NetworkAllow, Target: "*.github.com", Ports: "443", Protocol: "tcp", Description: "GitHub"},
				{Action: NetworkAllow, Target: "any", Ports: "53", Protocol: "udp", Description: "DNS"},
			},
		},
	}

	// PyPI allowed.
	ok, _ := policy.IsOutboundAllowed("pypi.org", 443, "tcp")
	if !ok {
		t.Error("pypi.org:443 should be allowed")
	}

	// GitHub subdomain allowed.
	ok, _ = policy.IsOutboundAllowed("api.github.com", 443, "tcp")
	if !ok {
		t.Error("api.github.com:443 should be allowed")
	}

	// Random HTTP denied.
	ok, reason := policy.IsOutboundAllowed("evil.com", 80, "tcp")
	if ok {
		t.Error("evil.com:80 should be denied")
	}
	if reason == "" {
		t.Error("denial should have a reason")
	}
}

// ── Mode Defaults ───────────────────────────────────────

func TestModeNetworkPolicies(t *testing.T) {
	// Inspect: only DNS allowed.
	inspect := ModeNetworkPolicies[ModeExplore]
	ok, _ := inspect.IsOutboundAllowed("8.8.8.8", 53, "udp")
	if !ok {
		t.Error("inspect should allow DNS")
	}
	ok, _ = inspect.IsOutboundAllowed("1.2.3.4", 443, "tcp")
	if ok {
		t.Error("inspect should deny HTTPS")
	}

	// Execute: allow most, block private + metadata.
	execute := ModeNetworkPolicies[ModeExecute]
	ok, _ = execute.IsOutboundAllowed("93.184.216.34", 443, "tcp")
	if !ok {
		t.Error("execute should allow public HTTPS")
	}
	ok, _ = execute.IsOutboundAllowed("169.254.169.254", 80, "tcp")
	if ok {
		t.Error("execute should block cloud metadata")
	}
	ok, _ = execute.IsOutboundAllowed("10.0.0.1", 80, "tcp")
	if ok {
		t.Error("execute should block private networks")
	}

	// Commit: allow all.
	commit := ModeNetworkPolicies[ModeExecute]
	ok, _ = commit.IsOutboundAllowed("anything", 12345, "tcp")
	if !ok {
		t.Error("commit should allow everything")
	}
}

// ── iptables Generation ─────────────────────────────────

func TestGenerateIptablesRules(t *testing.T) {
	policy := NetworkPolicy{
		Outbound: NetworkRuleSet{
			DefaultAction: NetworkDeny,
			Rules: []NetworkRule{
				{Action: NetworkAllow, Target: "any", Ports: "53", Protocol: "udp"},
				{Action: NetworkAllow, Target: "any", Ports: "443", Protocol: "tcp"},
				{Action: NetworkDeny, Target: "10.0.0.0/8"},
			},
		},
		Inbound: NetworkRuleSet{DefaultAction: NetworkDeny},
	}

	rules := policy.GenerateIptablesRules("tap0")
	joined := strings.Join(rules, "\n")

	if !strings.Contains(joined, "-p udp --dport 53 -j ACCEPT") {
		t.Error("should have DNS allow rule")
	}
	if !strings.Contains(joined, "-p tcp --dport 443 -j ACCEPT") {
		t.Error("should have HTTPS allow rule")
	}
	if !strings.Contains(joined, "-d 10.0.0.0/8") {
		t.Error("should have private network deny rule")
	}
	if !strings.Contains(joined, "-j DROP") {
		t.Error("should have default DROP")
	}
}

// ── ExecPolicy Integration ──────────────────────────────

func TestExecPolicy_EffectiveNetworkPolicy(t *testing.T) {
	// No explicit policy — use mode default.
	p := ExecPolicy{}
	np := p.EffectiveNetworkPolicy(ModeExplore)
	if np.Outbound.DefaultAction != NetworkDeny {
		t.Error("inspect default should deny outbound")
	}

	// Explicit policy overrides mode default.
	custom := &NetworkPolicy{
		Outbound: NetworkRuleSet{DefaultAction: NetworkAllow},
	}
	p2 := ExecPolicy{NetworkPolicy: custom}
	np2 := p2.EffectiveNetworkPolicy(ModeExplore)
	if np2.Outbound.DefaultAction != NetworkAllow {
		t.Error("explicit policy should override mode default")
	}
}
