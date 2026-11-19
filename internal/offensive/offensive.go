// Package offensive holds the Netrunner deck's recon tooling.
//
// Scope guardrail: scanning is restricted to private / loopback / link-local
// address space (your own lab networks). Public targets are refused — point it
// only at hosts you own or are explicitly authorized to test.
package offensive

import (
	"context"
	"fmt"
	"net"
	"sort"
	"sync"
	"time"
)

// commonPorts covers general services plus the IoT-ish ports worth probing in a lab.
var commonPorts = map[int]string{
	21: "ftp", 22: "ssh", 23: "telnet", 25: "smtp", 53: "dns", 80: "http",
	110: "pop3", 123: "ntp", 135: "msrpc", 139: "netbios", 143: "imap",
	443: "https", 445: "smb", 554: "rtsp", 993: "imaps", 1883: "mqtt",
	3306: "mysql", 3389: "rdp", 5000: "upnp/http", 5353: "mdns", 5900: "vnc",
	6379: "redis", 8080: "http-alt", 8443: "https-alt", 8883: "mqtt-tls",
	9100: "printer", 1900: "ssdp", 2323: "telnet-alt", 37777: "dahua-dvr",
	49152: "upnp",
}

// PortScan runs a TCP-connect scan of target and returns human-readable lines.
func PortScan(ctx context.Context, target string) []string {
	ip := net.ParseIP(target)
	if ip == nil {
		return []string{"✗ invalid target — expected a single IPv4 address (CIDR ranges coming soon)"}
	}
	if !isLabScope(ip) {
		return []string{
			"⊘ SCOPE DENIED — " + target + " is a public address.",
			"netwatch recon is locked to private/lab space (10/8, 172.16/12, 192.168/16, loopback).",
			"Point it at hosts you own or are authorized to test.",
		}
	}

	type res struct {
		port int
		svc  string
	}
	ports := make([]int, 0, len(commonPorts))
	for p := range commonPorts {
		ports = append(ports, p)
	}

	var (
		mu   sync.Mutex
		open []res
		wg   sync.WaitGroup
		sem  = make(chan struct{}, 64)
	)
	start := time.Now()
	for _, p := range ports {
		wg.Add(1)
		sem <- struct{}{}
		go func(port int) {
			defer wg.Done()
			defer func() { <-sem }()
			d := net.Dialer{Timeout: 450 * time.Millisecond}
			conn, err := d.DialContext(ctx, "tcp", fmt.Sprintf("%s:%d", target, port))
			if err != nil {
				return
			}
			_ = conn.Close()
			mu.Lock()
			open = append(open, res{port, commonPorts[port]})
			mu.Unlock()
		}(p)
	}
	wg.Wait()
	sort.Slice(open, func(i, j int) bool { return open[i].port < open[j].port })

	lines := []string{fmt.Sprintf("⌁ PROBE %s · %d ports · %s", target, len(ports), time.Since(start).Truncate(time.Millisecond))}
	if len(open) == 0 {
		lines = append(lines, "  no open TCP ports in the common set (host may be down, filtered, or clean)")
		return lines
	}
	lines = append(lines, fmt.Sprintf("  %d OPEN:", len(open)))
	for _, r := range open {
		lines = append(lines, fmt.Sprintf("  %-7s open   %s", fmt.Sprintf("%d/tcp", r.port), r.svc))
	}
	return lines
}

func isLabScope(ip net.IP) bool {
	return ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast()
}
