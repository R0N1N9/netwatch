package sonar

import (
	"encoding/json"
	"os"
	"sort"
	"sync"
)

// SleepPoint is one minute on a link: breathing rate (0 = none found) and how
// many times movement started.
type SleepPoint struct {
	T      int64   `json:"t"` // unix ms
	Breath float64 `json:"b"`
	Moves  int     `json:"m"`
}

// SleepLog keeps the last 24 hours of per-minute readings for each node.
type SleepLog struct {
	mu     sync.Mutex
	path   string // "" = memory only
	series map[string][]SleepPoint
}

const sleepKeep = 24 * 60

// OpenSleepLog loads the log at path, or starts an empty one.
func OpenSleepLog(path string) *SleepLog {
	l := &SleepLog{path: path, series: map[string][]SleepPoint{}}
	if path != "" {
		if b, err := os.ReadFile(path); err == nil {
			json.Unmarshal(b, &l.series)
		}
	}
	return l
}

// Add appends a reading and saves the log.
func (l *SleepLog) Add(node string, p SleepPoint) {
	l.mu.Lock()
	defer l.mu.Unlock()
	s := append(l.series[node], p)
	if len(s) > sleepKeep {
		s = s[len(s)-sleepKeep:]
	}
	l.series[node] = s
	if l.path != "" {
		if b, err := json.Marshal(l.series); err == nil {
			os.WriteFile(l.path, b, 0o600)
		}
	}
}

// Series returns a node's readings, oldest first.
func (l *SleepLog) Series(node string) []SleepPoint {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]SleepPoint(nil), l.series[node]...)
}

// Nodes lists the nodes that have readings.
func (l *SleepLog) Nodes() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]string, 0, len(l.series))
	for k := range l.series {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
