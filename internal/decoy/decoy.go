// Package decoy plants things nobody has a reason to touch — a listening port
// with nothing behind it, a file with a tempting name, a hostname that does not
// exist — and reports the moment something touches one.
package decoy

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
)

// Trip is one touch of a decoy.
type Trip struct {
	Time   time.Time `json:"time"`
	Kind   string    `json:"kind"` // honeyport | canary-file | canary-name
	Decoy  string    `json:"decoy"`
	Source string    `json:"source"`
	Detail string    `json:"detail"`
}

// Manager owns the decoys.
type Manager struct {
	mu     sync.Mutex
	onTrip func(Trip)
	ports  map[int]net.Listener
	files  map[string]func() // path -> stop watching
	names  map[string]bool
	trips  []Trip
}

// New returns a manager that calls onTrip for every touch.
func New(onTrip func(Trip)) *Manager {
	return &Manager{onTrip: onTrip, ports: map[int]net.Listener{}, files: map[string]func(){}, names: map[string]bool{}}
}

func (m *Manager) trip(t Trip) {
	m.mu.Lock()
	m.trips = append(m.trips, t)
	if len(m.trips) > 200 {
		m.trips = m.trips[len(m.trips)-200:]
	}
	cb := m.onTrip
	m.mu.Unlock()
	if cb != nil {
		cb(t)
	}
}

// Trips returns recorded touches, newest first.
func (m *Manager) Trips() []Trip {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Trip, len(m.trips))
	for i, t := range m.trips {
		out[len(m.trips)-1-i] = t
	}
	return out
}

// ---- honeyports ----

// OpenPort listens on a TCP port that has no real service. Any connection is a trip.
func (m *Manager) OpenPort(port int) error {
	return m.openPort(fmt.Sprintf("0.0.0.0:%d", port), port)
}

func (m *Manager) openPort(addr string, port int) error {
	m.mu.Lock()
	if _, dup := m.ports[port]; dup {
		m.mu.Unlock()
		return fmt.Errorf("port %d is already a decoy", port)
	}
	m.mu.Unlock()
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.ports[port] = ln
	m.mu.Unlock()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				c.SetDeadline(time.Now().Add(2 * time.Second))
				buf := make([]byte, 256)
				n, _ := c.Read(buf)
				detail := "connected and sent nothing"
				if n > 0 {
					detail = "first bytes: " + printable(buf[:n])
				}
				host, _, _ := net.SplitHostPort(c.RemoteAddr().String())
				m.trip(Trip{Time: time.Now(), Kind: "honeyport", Decoy: fmt.Sprintf("tcp/%d", port), Source: host, Detail: detail})
			}(conn)
		}
	}()
	return nil
}

// ClosePort stops a honeyport.
func (m *Manager) ClosePort(port int) {
	m.mu.Lock()
	ln := m.ports[port]
	delete(m.ports, port)
	m.mu.Unlock()
	if ln != nil {
		ln.Close()
	}
}

func printable(b []byte) string {
	var sb strings.Builder
	for _, r := range string(b) {
		if r < unicode.MaxASCII && unicode.IsPrint(r) {
			sb.WriteRune(r)
		} else {
			sb.WriteByte('.')
		}
		if sb.Len() >= 80 {
			break
		}
	}
	return sb.String()
}

// ---- canary files ----

const canaryHead = "# internal — do not distribute\n"
const canaryBody = canaryHead + "vpn_user=svc-backup\nvpn_pass=Tr4il-%s\n"

// PlantFile writes a tempting file into dir and watches it. Opening it is a trip.
func (m *Manager) PlantFile(dir, name string) (string, error) {
	if name == "" {
		name = "passwords-backup.txt"
	}
	if strings.ContainsAny(name, "/\\") {
		return "", fmt.Errorf("the name must not contain a path")
	}
	path := filepath.Join(dir, name)
	m.mu.Lock()
	_, dup := m.files[path]
	m.mu.Unlock()
	if dup {
		return "", fmt.Errorf("%s is already a canary", path)
	}
	if _, err := os.Stat(path); err == nil {
		return "", fmt.Errorf("%s already exists; NetWatch will not overwrite it", path)
	}
	if err := os.WriteFile(path, []byte(fmt.Sprintf(canaryBody, time.Now().Format("150405"))), 0o600); err != nil {
		return "", err
	}
	stop, err := watchFile(path, func() {
		m.trip(Trip{Time: time.Now(), Kind: "canary-file", Decoy: path, Source: "this machine", Detail: "the file was opened"})
	})
	if err != nil {
		os.Remove(path)
		return "", err
	}
	m.mu.Lock()
	m.files[path] = stop
	m.mu.Unlock()
	return path, nil
}

// Adopt resumes watching a canary file that an earlier run planted.
func (m *Manager) Adopt(path string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if !strings.HasPrefix(string(b), canaryHead) {
		return fmt.Errorf("%s is not a NetWatch canary", path)
	}
	m.mu.Lock()
	_, dup := m.files[path]
	m.mu.Unlock()
	if dup {
		return nil
	}
	stop, err := watchFile(path, func() {
		m.trip(Trip{Time: time.Now(), Kind: "canary-file", Decoy: path, Source: "this machine", Detail: "the file was opened"})
	})
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.files[path] = stop
	m.mu.Unlock()
	return nil
}

// RemoveFile stops watching a canary file and deletes it.
func (m *Manager) RemoveFile(path string) {
	m.mu.Lock()
	stop := m.files[path]
	delete(m.files, path)
	m.mu.Unlock()
	if stop != nil {
		stop()
		os.Remove(path)
	}
}

// ---- canary names ----

// AddName registers a hostname that nothing legitimate looks up.
func (m *Manager) AddName(name string) {
	name = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(name)), ".")
	if name == "" || len(name) > 120 {
		return
	}
	m.mu.Lock()
	m.names[name] = true
	m.mu.Unlock()
}

// RemoveName unregisters a canary name.
func (m *Manager) RemoveName(name string) {
	m.mu.Lock()
	delete(m.names, strings.ToLower(name))
	m.mu.Unlock()
}

// SeenDNS is called for every DNS query NetWatch observes.
func (m *Manager) SeenDNS(src, name string) {
	name = strings.TrimSuffix(strings.ToLower(name), ".")
	m.mu.Lock()
	hit := m.names[name]
	m.mu.Unlock()
	if hit {
		m.trip(Trip{Time: time.Now(), Kind: "canary-name", Decoy: name, Source: src, Detail: "the name was looked up"})
	}
}

// State lists the decoys currently planted.
type State struct {
	Ports []int    `json:"ports"`
	Files []string `json:"files"`
	Names []string `json:"names"`
}

func (m *Manager) State() State {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := State{Ports: []int{}, Files: []string{}, Names: []string{}}
	for p := range m.ports {
		s.Ports = append(s.Ports, p)
	}
	for f := range m.files {
		s.Files = append(s.Files, f)
	}
	for n := range m.names {
		s.Names = append(s.Names, n)
	}
	sort.Ints(s.Ports)
	sort.Strings(s.Files)
	sort.Strings(s.Names)
	return s
}

// Close removes every decoy.
func (m *Manager) Close() {
	st := m.State()
	for _, p := range st.Ports {
		m.ClosePort(p)
	}
	for _, f := range st.Files {
		m.RemoveFile(f)
	}
}
