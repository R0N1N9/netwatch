// Package persona is a talking decoy: a port that pretends to be a small
// device's login prompt, lets a visitor "in" on the second try, answers a few
// commands with canned text, and records the whole conversation.
//
// Nothing a visitor types is ever executed: input is only compared against a
// table of replies, or handed to an optional responder as text. Passwords are
// not kept — only their length.
package persona

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"sort"
	"strings"
	"sync"
	"time"
)

// Line is one line of a session.
type Line struct {
	T    int64  `json:"t"`
	Dir  string `json:"dir"` // in (the visitor) | out (the decoy)
	Text string `json:"text"`
}

// Session is one visit.
type Session struct {
	ID      string   `json:"id"`
	Port    int      `json:"port"`
	Source  string   `json:"source"`
	Start   int64    `json:"start"`
	End     int64    `json:"end"`
	Lines   []Line   `json:"lines"`
	Users   []string `json:"users"`
	Summary string   `json:"summary"`
}

// Responder may answer a command the script does not know. ok=false falls back to "not found".
type Responder func(ctx context.Context, history []Line, input string) (reply string, ok bool)

const (
	banner      = "\r\nBusyBox v1.31.1 (2021-03-04 08:12:09 CST) built-in shell (ash)\r\n\r\n"
	maxLine     = 512
	maxCommands = 16
	sessionTime = 90 * time.Second
	maxSessions = 4 // at once, per port
)

// replies is the whole "system" behind the prompt.
var replies = map[string]string{
	"id":                "uid=0(root) gid=0(root)",
	"whoami":            "root",
	"pwd":               "/",
	"ls":                "bin   dev   etc   lib   mnt   proc  sbin  sys   tmp   usr   var",
	"uname -a":          "Linux (none) 3.10.14 #1 Thu Mar 4 08:10:51 CST 2021 mips GNU/Linux",
	"uname":             "Linux",
	"cat /proc/cpuinfo": "system type\t\t: MT7628\nprocessor\t\t: 0\ncpu model\t\t: MIPS 24KEc V5.5\nBogoMIPS\t\t: 385.84",
	"ps":                "  PID USER       VSZ STAT COMMAND\n    1 root      1528 S    init\n  312 root      1524 S    telnetd\n  401 root      1532 S    -sh",
	"ifconfig":          "br0       Link encap:Ethernet  HWaddr 00:0C:43:28:80:A1\n          inet addr:192.168.0.1  Bcast:192.168.0.255  Mask:255.255.255.0",
	"free":              "             total       used       free\nMem:         59648      31204      28444",
	"mount":             "rootfs on / type rootfs (rw)\n/dev/root on / type squashfs (ro,relatime)",
	"enable":            "",
	"shell":             "",
	"sh":                "",
	"system":            "",
}

// reply answers one command line from the table. ok=false means the script has nothing.
func reply(cmd string) (string, bool) {
	cmd = strings.TrimSpace(cmd)
	if r, ok := replies[cmd]; ok {
		return r, true
	}
	first := strings.Fields(cmd + " x")[0]
	switch first {
	case "wget", "curl", "tftp", "ftpget":
		return first + ": can't connect to remote host: Network is unreachable", true
	case "cd", "export", "echo":
		if first == "echo" {
			return strings.TrimSpace(strings.TrimPrefix(cmd, "echo")), true
		}
		return "", true
	case "rm", "chmod", "kill", "killall", "mv", "cp":
		return "", true
	case "cat":
		return "cat: can't open '" + strings.TrimSpace(strings.TrimPrefix(cmd, "cat")) + "': No such file or directory", true
	case "/bin/busybox", "busybox":
		if f := strings.Fields(cmd); len(f) > 1 {
			return f[1] + ": applet not found", true
		}
		return "BusyBox v1.31.1 multi-call binary.", true
	}
	return "", false
}

// clean keeps printable ASCII only: telnet option bytes and control characters are dropped.
func clean(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= 0x20 && r < 0x7f {
			b.WriteRune(r)
		}
	}
	out := strings.TrimSpace(b.String())
	if len(out) > maxLine {
		out = out[:maxLine]
	}
	return out
}

// Summarise describes what the visitor did, from the transcript alone.
func Summarise(s Session) string {
	var cmds []string
	logins := 0
	for _, l := range s.Lines {
		if l.Dir != "in" {
			continue
		}
		if strings.HasPrefix(l.Text, "login ") {
			logins++
		} else if l.Text != "" {
			cmds = append(cmds, l.Text)
		}
	}
	if logins == 0 && len(cmds) == 0 {
		return "Connected, saw the prompt and left without typing."
	}
	all := strings.ToLower(strings.Join(cmds, "\n"))
	any := func(words ...string) bool {
		for _, w := range words {
			if strings.Contains(all, w) {
				return true
			}
		}
		return false
	}
	var did []string
	if any("wget", "curl", "tftp", "ftpget") {
		did = append(did, "tried to download something onto the device")
	}
	if any("uname", "cpuinfo", "/proc", "ifconfig", "ps", "mount", "id", "whoami") {
		did = append(did, "looked around to learn what the device is")
	}
	if any("crontab", "rc.local", "init.d", "authorized_keys") {
		did = append(did, "tried to make its access permanent")
	}
	if any("rm -rf", "kill", "iptables", "dd if=") {
		did = append(did, "tried to stop or wipe things")
	}
	out := fmt.Sprintf("%d login attempt(s)", logins)
	if len(s.Users) > 0 {
		out += " as " + strings.Join(s.Users, ", ")
	}
	if len(cmds) > 0 {
		out += fmt.Sprintf(", then %d command(s)", len(cmds))
	}
	out += "."
	if len(did) > 0 {
		out += " It " + strings.Join(did, ", and ") + "."
	}
	if any("/bin/busybox", "enable\nsystem", "enable\nshell") || (len(cmds) >= 4 && s.End-s.Start < 4000) {
		out += " The pattern and speed look like an automated bot of the kind that hunts routers and cameras."
	}
	return out
}

// Manager owns the talking decoys.
type Manager struct {
	mu        sync.Mutex
	ports     map[int]net.Listener
	active    map[int]int
	sessions  []Session
	seq       int
	onSession func(Session)

	// Model, if set, answers commands the script does not know.
	Model Responder
}

// New returns a manager that calls onSession when a visit ends.
func New(onSession func(Session)) *Manager {
	return &Manager{ports: map[int]net.Listener{}, active: map[int]int{}, onSession: onSession}
}

// Ports lists the open decoys.
func (m *Manager) Ports() []int {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]int, 0, len(m.ports))
	for p := range m.ports {
		out = append(out, p)
	}
	sort.Ints(out)
	return out
}

// Sessions returns recorded visits, newest first.
func (m *Manager) Sessions() []Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Session, len(m.sessions))
	for i, s := range m.sessions {
		out[len(m.sessions)-1-i] = s
	}
	return out
}

// Open starts a talking decoy on a TCP port.
func (m *Manager) Open(port int) error { return m.open(fmt.Sprintf("0.0.0.0:%d", port), port) }

func (m *Manager) open(addr string, port int) error {
	m.mu.Lock()
	_, dup := m.ports[port]
	m.mu.Unlock()
	if dup {
		return fmt.Errorf("port %d is already a talking decoy", port)
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.ports[port] = ln
	m.mu.Unlock()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			m.mu.Lock()
			busy := m.active[port] >= maxSessions
			if !busy {
				m.active[port]++
			}
			m.mu.Unlock()
			if busy {
				c.Close()
				continue
			}
			go func() {
				m.serve(c, port)
				m.mu.Lock()
				m.active[port]--
				m.mu.Unlock()
			}()
		}
	}()
	return nil
}

// Close stops a decoy.
func (m *Manager) Close(port int) {
	m.mu.Lock()
	ln := m.ports[port]
	delete(m.ports, port)
	m.mu.Unlock()
	if ln != nil {
		ln.Close()
	}
}

// CloseAll stops every decoy.
func (m *Manager) CloseAll() {
	for _, p := range m.Ports() {
		m.Close(p)
	}
}

func (m *Manager) serve(c net.Conn, port int) {
	defer c.Close()
	start := time.Now()
	c.SetDeadline(start.Add(sessionTime))
	host, _, _ := net.SplitHostPort(c.RemoteAddr().String())
	s := Session{Port: port, Source: host, Start: start.UnixMilli(), Lines: []Line{}, Users: []string{}}
	r := bufio.NewReaderSize(c, 2048)
	say := func(text string) {
		fmt.Fprint(c, text)
		if t := clean(text); t != "" {
			s.Lines = append(s.Lines, Line{time.Now().UnixMilli(), "out", t})
		}
	}
	read := func() (string, bool) {
		raw, err := r.ReadString('\n')
		if err != nil && raw == "" {
			return "", false
		}
		return clean(raw), true
	}
	say(banner)
	in := false
	for attempt := 1; attempt <= 3 && !in; attempt++ {
		say("(none) login: ")
		user, ok := read()
		if !ok {
			break
		}
		say("Password: ")
		pass, ok := read()
		if !ok {
			break
		}
		if user != "" && !containsStr(s.Users, user) && len(s.Users) < 8 {
			s.Users = append(s.Users, user)
		}
		s.Lines = append(s.Lines, Line{time.Now().UnixMilli(), "in", fmt.Sprintf("login %s / (%d characters)", user, len(pass))})
		if attempt >= 2 {
			in = true
		} else {
			say("Login incorrect\r\n\r\n")
		}
	}
	for n := 0; in && n < maxCommands; n++ {
		say("\r\n# ")
		cmd, ok := read()
		if !ok {
			break
		}
		s.Lines = append(s.Lines, Line{time.Now().UnixMilli(), "in", cmd})
		if cmd == "exit" || cmd == "quit" || cmd == "logout" {
			break
		}
		out, known := reply(cmd)
		if !known && cmd != "" {
			if m.Model != nil {
				ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
				out, known = m.Model(ctx, s.Lines, cmd)
				cancel()
			}
			if !known {
				out = "-sh: " + strings.Fields(cmd)[0] + ": not found"
			}
		}
		if out != "" {
			say(strings.ReplaceAll(out, "\n", "\r\n"))
		}
	}
	s.End = time.Now().UnixMilli()
	s.Summary = Summarise(s)
	m.mu.Lock()
	m.seq++
	s.ID = fmt.Sprintf("s%d-%d", s.Start, m.seq)
	m.sessions = append(m.sessions, s)
	if len(m.sessions) > 60 {
		m.sessions = m.sessions[len(m.sessions)-60:]
	}
	cb := m.onSession
	m.mu.Unlock()
	if cb != nil {
		cb(s)
	}
}

func containsStr(v []string, x string) bool {
	for _, y := range v {
		if y == x {
			return true
		}
	}
	return false
}
