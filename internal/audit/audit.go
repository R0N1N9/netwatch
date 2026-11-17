// Package audit provides blue-team host introspection for the kill-chain deck:
// listening sockets, persistence artifacts, and live outbound connections.
// Everything here is read-only and localhost-scoped.
package audit

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Listeners enumerates listening TCP sockets (C2 / backdoor surface). It prefers
// `ss` for process names and falls back to parsing /proc/net/tcp directly.
func Listeners() []string {
	if rows := ssListeners(); rows != nil {
		return prepend(fmt.Sprintf("◆ LISTENING SOCKETS · %d · [T1571/T1021 surface]", len(rows)), rows)
	}
	var rows []string
	seen := map[string]bool{}
	for proto, path := range map[string]string{"tcp": "/proc/net/tcp", "tcp6": "/proc/net/tcp6"} {
		for _, ep := range listenState(path) {
			key := proto + ep
			if seen[key] {
				continue
			}
			seen[key] = true
			rows = append(rows, fmt.Sprintf("%-5s %s", proto, ep))
		}
	}
	sort.Strings(rows)
	if len(rows) == 0 {
		rows = []string{"no listening sockets"}
	}
	return prepend(fmt.Sprintf("◆ LISTENING SOCKETS · %d · [T1571/T1021 surface]", len(rows)), rows)
}

func ssListeners() []string {
	out, err := exec.Command("ss", "-tlnH").CombinedOutput()
	if err != nil {
		return nil
	}
	var rows []string
	sc := bufio.NewScanner(strings.NewReader(string(out)))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 5 {
			continue
		}
		proc := ""
		if last := f[len(f)-1]; strings.Contains(last, "users:") {
			proc = procName(last)
		}
		rows = append(rows, fmt.Sprintf("%-4s %-24s %s", f[0], f[3], proc))
	}
	return rows
}

// listenState parses /proc/net/tcp{,6} for sockets in LISTEN (0A).
func listenState(path string) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []string
	sc := bufio.NewScanner(f)
	sc.Scan() // header
	for sc.Scan() {
		ff := strings.Fields(sc.Text())
		if len(ff) < 4 || ff[3] != "0A" { // 0A = LISTEN
			continue
		}
		if ep := decodeEndpoint(ff[1]); ep != "" {
			out = append(out, ep)
		}
	}
	return out
}

// Persistence surveys common persistence locations (T1053/T1543/T1547).
func Persistence() []string {
	var out []string
	add := func(cat, item string) { out = append(out, fmt.Sprintf("[%-9s] %s", cat, item)) }
	home, _ := os.UserHomeDir()

	for _, d := range []string{"/etc/systemd/system", filepath.Join(home, ".config/systemd/user")} {
		for _, f := range glob(d, "*.service") {
			add("systemd", f)
		}
	}
	if exists("/etc/crontab") {
		add("cron", "/etc/crontab")
	}
	for _, f := range glob("/etc/cron.d", "*") {
		add("cron", f)
	}
	for _, n := range []string{".bashrc", ".bash_profile", ".profile", ".zshrc"} {
		if p := filepath.Join(home, n); exists(p) {
			add("shell-rc", p)
		}
	}
	for _, f := range glob(filepath.Join(home, ".config/autostart"), "*.desktop") {
		add("autostart", f)
	}
	if exists("/etc/rc.local") {
		add("rc.local", "/etc/rc.local")
	}
	if len(out) == 0 {
		out = []string{"no common persistence artifacts found"}
	}
	return prepend(fmt.Sprintf("◆ PERSISTENCE AUDIT · %d artifacts · [T1543/T1547]", len(out)), out)
}

// Connections lists established outbound/inbound TCP peers (C2 / exfil hunting).
func Connections() []string {
	seen := map[string]bool{}
	var pub, priv []string
	for _, path := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		for _, rem := range established(path) {
			if seen[rem] {
				continue
			}
			seen[rem] = true
			host := rem[:strings.LastIndex(rem, ":")]
			ip := net.ParseIP(strings.Trim(host, "[]"))
			if ip != nil && !ip.IsPrivate() && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() {
				pub = append(pub, "→ "+rem+"  ⚠ PUBLIC")
			} else {
				priv = append(priv, "→ "+rem)
			}
		}
	}
	sort.Strings(pub)
	sort.Strings(priv)
	rows := append(pub, priv...)
	if len(rows) == 0 {
		rows = []string{"no established connections"}
	}
	return prepend(fmt.Sprintf("◆ ESTABLISHED PEERS · %d (%d public) · [C2/exfil hunt]", len(rows), len(pub)), rows)
}

// Eicar returns the standard EICAR anti-malware test string (harmless) so the
// operator can validate that AV/EDR detection actually fires (T1059 test).
func Eicar() []string {
	s := `X5O!P%@AP[4\PZX54(P^)7CC)7}$` + "EICAR-STANDARD-ANTIVIRUS-TEST-FILE!$H+H*"
	return []string{
		"◆ EICAR DETECTION TEST · [benign AV/EDR validation]",
		"Write this 68-byte string to a file; a working scanner must flag it.",
		"It is inert — not malware — the industry-standard test artifact.",
		"",
		s,
	}
}

// ---- helpers ----

func established(path string) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []string
	sc := bufio.NewScanner(f)
	sc.Scan() // header
	for sc.Scan() {
		ff := strings.Fields(sc.Text())
		if len(ff) < 4 || ff[3] != "01" { // 01 = ESTABLISHED
			continue
		}
		if ep := decodeEndpoint(ff[2]); ep != "" {
			out = append(out, ep)
		}
	}
	return out
}

// decodeEndpoint parses "HEXADDR:HEXPORT" from /proc/net/tcp{,6}.
func decodeEndpoint(s string) string {
	parts := strings.Split(s, ":")
	if len(parts) != 2 {
		return ""
	}
	raw, err := hex.DecodeString(parts[0])
	if err != nil || (len(raw) != 4 && len(raw) != 16) {
		return ""
	}
	// /proc stores each 32-bit word little-endian; reverse per 4 bytes.
	b := make([]byte, len(raw))
	for i := 0; i < len(raw); i += 4 {
		b[i], b[i+1], b[i+2], b[i+3] = raw[i+3], raw[i+2], raw[i+1], raw[i]
	}
	ip := net.IP(b)
	port, _ := strconv.ParseInt(parts[1], 16, 32)
	if len(raw) == 16 {
		return fmt.Sprintf("[%s]:%d", ip.String(), port)
	}
	return fmt.Sprintf("%s:%d", ip.String(), port)
}

func procName(field string) string {
	i := strings.Index(field, `("`)
	if i < 0 {
		return ""
	}
	rest := field[i+2:]
	j := strings.Index(rest, `"`)
	if j < 0 {
		return ""
	}
	return rest[:j]
}

func glob(dir, pat string) []string {
	m, _ := filepath.Glob(filepath.Join(dir, pat))
	return m
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func prepend(head string, rest []string) []string {
	return append([]string{head}, rest...)
}
