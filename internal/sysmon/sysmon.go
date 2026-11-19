// Package sysmon reports the host's own CPU and memory use (Linux /proc) for
// the console's resource bars.
package sysmon

import (
	"context"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Stats is a point-in-time reading. OK is false when /proc is unavailable.
type Stats struct {
	OK       bool    `json:"ok"`
	CPU      float64 `json:"cpu"` // percent busy across all cores
	Cores    int     `json:"cores"`
	Load1    float64 `json:"load1"`
	MemUsed  uint64  `json:"memUsed"`  // bytes
	MemTotal uint64  `json:"memTotal"` // bytes
	MemPct   float64 `json:"memPct"`
}

// Monitor samples CPU once a second so readings are a recent average.
type Monitor struct {
	mu        sync.Mutex
	cpu       float64
	have      bool
	prevIdle  uint64
	prevTotal uint64
}

// Run samples until ctx ends.
func (m *Monitor) Run(ctx context.Context) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		m.sample()
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (m *Monitor) sample() {
	b, err := os.ReadFile("/proc/stat")
	if err != nil {
		return
	}
	idle, total, ok := parseCPU(strings.SplitN(string(b), "\n", 2)[0])
	if !ok {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.prevTotal != 0 && total > m.prevTotal {
		m.cpu = 100 * (1 - float64(idle-m.prevIdle)/float64(total-m.prevTotal))
		m.have = true
	}
	m.prevIdle, m.prevTotal = idle, total
}

// Snapshot returns the latest reading.
func (m *Monitor) Snapshot() Stats {
	m.mu.Lock()
	s := Stats{CPU: m.cpu, Cores: runtime.NumCPU(), OK: m.have}
	m.mu.Unlock()
	if b, err := os.ReadFile("/proc/meminfo"); err == nil {
		s.MemTotal, s.MemUsed = parseMem(string(b))
		if s.MemTotal > 0 {
			s.MemPct = 100 * float64(s.MemUsed) / float64(s.MemTotal)
		}
	} else {
		s.OK = false
	}
	if b, err := os.ReadFile("/proc/loadavg"); err == nil {
		if f := strings.Fields(string(b)); len(f) > 0 {
			s.Load1, _ = strconv.ParseFloat(f[0], 64)
		}
	}
	return s
}

// parseCPU reads the aggregate "cpu ..." line of /proc/stat.
func parseCPU(line string) (idle, total uint64, ok bool) {
	f := strings.Fields(line)
	if len(f) < 5 || f[0] != "cpu" {
		return 0, 0, false
	}
	for i, v := range f[1:] {
		if i >= 8 { // guest time is already counted inside user/nice
			break
		}
		n, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			return 0, 0, false
		}
		total += n
		if i == 3 || i == 4 { // idle + iowait
			idle += n
		}
	}
	return idle, total, true
}

// parseMem returns total and used bytes from /proc/meminfo (used = total - available).
func parseMem(text string) (total, used uint64) {
	var avail uint64
	for _, line := range strings.Split(text, "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		n, _ := strconv.ParseUint(f[1], 10, 64)
		switch f[0] {
		case "MemTotal:":
			total = n * 1024
		case "MemAvailable:":
			avail = n * 1024
		}
	}
	if avail > total {
		return total, 0
	}
	return total, total - avail
}
