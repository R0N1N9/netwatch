// Package copilot is NetWatch's AI daemon. Today it reasons over network state
// with a local heuristic; the Copilot interface lets an LLM backend (local model
// or API) drop in behind the same call without touching the UI or server.
package copilot

import (
	"context"
	"fmt"
	"strings"

	"github.com/r0n1n9/netwatch/internal/model"
)

// Context is the network snapshot handed to the copilot for each question.
type Context struct {
	Devices     []model.Device
	Alerts      []model.Alert
	Quarantined map[string]bool
	Iface       string
	Subnet      string
}

// Copilot answers operator questions about the network.
type Copilot interface {
	Ask(ctx context.Context, query string, c Context) string
}

// Default returns the built-in heuristic copilot.
func Default() Copilot { return heuristic{} }

type heuristic struct{}

func (heuristic) Ask(_ context.Context, query string, c Context) string {
	q := strings.ToLower(query)

	var crit, warn, newDev int
	var critLines []string
	for _, a := range c.Alerts {
		switch a.Severity {
		case model.SevCritical:
			crit++
			critLines = append(critLines, "• "+a.Message)
		case model.SevWarn:
			warn++
		}
		if a.Kind == "new-device" {
			newDev++
		}
	}
	trusted, untrusted := 0, 0
	var untrustedIPs []string
	for _, d := range c.Devices {
		if d.Trusted || d.IsSelf || d.IsGateway {
			trusted++
		} else {
			untrusted++
			untrustedIPs = append(untrustedIPs, d.IP.String())
		}
	}

	switch {
	case containsAny(q, "quarantine", "block", "blackwall", "isolate"):
		if crit > 0 || untrusted > 0 {
			t := firstN(untrustedIPs, 3)
			return fmt.Sprintf("Recommend BLACKWALL on the unverified host(s): %s. Select the host and hit QUARANTINE — it drops their traffic at the firewall. Keep the gateway and your own rig off the wall.",
				strings.Join(t, ", "))
		}
		return "Nothing on the segment warrants a quarantine right now. Everything is trusted, gateway MAC is stable."

	case containsAny(q, "weird", "wrong", "threat", "suspicious", "danger", "attack", "status", "sitrep", "what's", "whats"):
		var b strings.Builder
		fmt.Fprintf(&b, "SITREP · %s on %s: %d hosts, %d trusted, %d unverified.\n", c.Iface, c.Subnet, len(c.Devices), trusted, untrusted)
		if crit > 0 {
			fmt.Fprintf(&b, "⚠ %d CRITICAL:\n%s\nThat MAC flip is the textbook ARP-spoof / MITM tell — verify the router and consider BLACKWALL.\n", crit, strings.Join(critLines, "\n"))
		}
		if newDev > 0 {
			fmt.Fprintf(&b, "%d new host(s) since baseline; %d still unverified (%s).\n", newDev, untrusted, strings.Join(firstN(untrustedIPs, 4), ", "))
		}
		if crit == 0 && newDev == 0 {
			b.WriteString("No anomalies. The wall holds.")
		}
		return strings.TrimSpace(b.String())

	case containsAny(q, "new", "join", "unknown", "unverified"):
		if untrusted == 0 {
			return "No unverified hosts — every device on the segment is trusted or known infrastructure."
		}
		return fmt.Sprintf("%d unverified host(s): %s. Mark the ones you recognise as TRUSTED to tighten new-device detection.",
			untrusted, strings.Join(untrustedIPs, ", "))

	default:
		return fmt.Sprintf("Watching %s (%s): %d hosts online, %d trusted, %d threats, %d warnings. Ask me \"what's weird right now?\", \"any new devices?\", or \"should I quarantine anything?\".",
			c.Iface, c.Subnet, len(c.Devices), trusted, crit, warn)
	}
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

func firstN(s []string, n int) []string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
