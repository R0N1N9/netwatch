// Package blackwall quarantines hosts at the firewall via nftables.
//
// It keeps one dedicated table (inet netrunner) and rebuilds it atomically on
// every change, so the live set of quarantined IPs is always exactly what the
// operator asked for. Requires CAP_NET_ADMIN (or root) and the `nft` binary.
package blackwall

import (
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"sync"
)

const tableName = "netrunner"

// Wall is the BLACKWALL controller.
type Wall struct {
	mu  sync.Mutex
	ips map[string]struct{}
}

// New returns an empty wall. Nothing touches the firewall until Quarantine.
func New() *Wall { return &Wall{ips: map[string]struct{}{}} }

// Quarantine blocks all inbound traffic from ip on this host.
func (w *Wall) Quarantine(ip string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.ips[ip] = struct{}{}
	if err := w.apply(); err != nil {
		delete(w.ips, ip)
		return err
	}
	return nil
}

// Release lifts the quarantine on ip.
func (w *Wall) Release(ip string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.ips, ip)
	return w.apply()
}

// List returns the currently quarantined IPs.
func (w *Wall) List() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make([]string, 0, len(w.ips))
	for ip := range w.ips {
		out = append(out, ip)
	}
	sort.Strings(out)
	return out
}

// apply rebuilds the nftables table from the current IP set (caller holds lock).
func (w *Wall) apply() error {
	ips := make([]string, 0, len(w.ips))
	for ip := range w.ips {
		ips = append(ips, ip)
	}
	sort.Strings(ips)

	var b strings.Builder
	b.WriteString("add table inet " + tableName + "\n")
	b.WriteString("flush table inet " + tableName + "\n")
	b.WriteString("table inet " + tableName + " {\n")
	b.WriteString("  set quar { type ipv4_addr;")
	if len(ips) > 0 {
		b.WriteString(" elements = { " + strings.Join(ips, ", ") + " };")
	}
	b.WriteString(" }\n")
	b.WriteString("  chain block { type filter hook input priority -150; ip saddr @quar drop; }\n")
	b.WriteString("}\n")

	cmd := exec.Command("nft", "-f", "-")
	cmd.Stdin = strings.NewReader(b.String())
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if strings.Contains(msg, "permission") || strings.Contains(err.Error(), "not found") {
			return fmt.Errorf("blackwall needs nft + CAP_NET_ADMIN (run: sudo setcap cap_net_raw,cap_net_admin+eip $(which netrunner)): %s", msg)
		}
		return fmt.Errorf("nft failed: %v: %s", err, msg)
	}
	return nil
}
