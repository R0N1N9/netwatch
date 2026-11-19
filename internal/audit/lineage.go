package audit

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// ProcConn ties one established connection on this host to the process that owns it.
type ProcConn struct {
	PID    int    `json:"pid"`
	Comm   string `json:"comm"`
	Exe    string `json:"exe"`
	Remote string `json:"remote"`
	Public bool   `json:"public"`
}

// Lineage maps this host's established TCP connections to their processes.
// Sockets owned by other users are only visible when running as root.
func Lineage() []ProcConn {
	type sock struct {
		remote string
		inode  uint64
	}
	var socks []sock
	for _, path := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(f)
		sc.Scan() // header
		for sc.Scan() {
			if remote, inode, ok := parseTCPLine(sc.Text()); ok {
				socks = append(socks, sock{remote, inode})
			}
		}
		f.Close()
	}
	if len(socks) == 0 {
		return nil
	}
	owners := socketOwners()
	var out []ProcConn
	seen := map[string]bool{}
	for _, s := range socks {
		pid := owners[s.inode]
		key := fmt.Sprintf("%d|%s", pid, s.remote)
		if seen[key] {
			continue
		}
		seen[key] = true
		pc := ProcConn{PID: pid, Remote: s.remote, Comm: "(unknown — owned by another user)"}
		if pid > 0 {
			if b, err := os.ReadFile(fmt.Sprintf("/proc/%d/comm", pid)); err == nil {
				pc.Comm = strings.TrimSpace(string(b))
			}
			pc.Exe, _ = os.Readlink(fmt.Sprintf("/proc/%d/exe", pid))
		}
		host := s.remote[:strings.LastIndex(s.remote, ":")]
		ip := net.ParseIP(strings.Trim(host, "[]"))
		pc.Public = ip != nil && !ip.IsPrivate() && !ip.IsLoopback() && !ip.IsLinkLocalUnicast()
		out = append(out, pc)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Public != out[j].Public {
			return out[i].Public
		}
		if out[i].Comm != out[j].Comm {
			return out[i].Comm < out[j].Comm
		}
		return out[i].Remote < out[j].Remote
	})
	return out
}

// LineageLines renders Lineage for the ops consoles.
func LineageLines() []string {
	conns := Lineage()
	pub := 0
	var rows []string
	for _, c := range conns {
		flag := ""
		if c.Public {
			pub++
			flag = "  ⚠ PUBLIC"
		}
		rows = append(rows, fmt.Sprintf("%-16s pid %-7d → %s%s", c.Comm, c.PID, c.Remote, flag))
	}
	if len(rows) == 0 {
		rows = []string{"no established connections"}
	}
	return prepend(fmt.Sprintf("◆ PROCESS LINEAGE · %d connections (%d public) · [T1071 — which program is talking]", len(conns), pub), rows)
}

// parseTCPLine extracts the remote endpoint and socket inode of an ESTABLISHED
// row from /proc/net/tcp{,6}.
func parseTCPLine(line string) (remote string, inode uint64, ok bool) {
	f := strings.Fields(line)
	if len(f) < 10 || f[3] != "01" {
		return "", 0, false
	}
	remote = decodeEndpoint(f[2])
	inode, err := strconv.ParseUint(f[9], 10, 64)
	if remote == "" || err != nil || inode == 0 {
		return "", 0, false
	}
	return remote, inode, true
}

// socketOwners maps socket inodes to the first process holding them open.
func socketOwners() map[uint64]int {
	owners := map[uint64]int{}
	fds, _ := filepath.Glob("/proc/[0-9]*/fd/*")
	for _, fd := range fds {
		link, err := os.Readlink(fd)
		if err != nil || !strings.HasPrefix(link, "socket:[") {
			continue
		}
		inode, err := strconv.ParseUint(strings.TrimSuffix(strings.TrimPrefix(link, "socket:["), "]"), 10, 64)
		if err != nil {
			continue
		}
		pid, _ := strconv.Atoi(strings.Split(fd, "/")[2])
		if _, ok := owners[inode]; !ok {
			owners[inode] = pid
		}
	}
	return owners
}
