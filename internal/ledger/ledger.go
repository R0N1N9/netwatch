// Package ledger is NetWatch's tamper-evident record: every alert and every
// action is appended to a hash chain, so a later edit or deletion anywhere in
// the history breaks verification from that point on.
package ledger

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

// Entry is one link in the chain.
type Entry struct {
	Seq      int    `json:"seq"`
	Time     int64  `json:"time"` // unix ms
	Kind     string `json:"kind"` // alert | action | note
	Severity string `json:"severity,omitempty"`
	Source   string `json:"source,omitempty"`
	Text     string `json:"text"`
	Prev     string `json:"prev"`
	Hash     string `json:"hash"`
}

func (e Entry) digest() string {
	h := sha256.Sum256([]byte(fmt.Sprintf("%d|%d|%s|%s|%s|%s|%s", e.Seq, e.Time, e.Kind, e.Severity, e.Source, e.Text, e.Prev)))
	return hex.EncodeToString(h[:])
}

// Ledger is an append-only chain, optionally backed by a JSON-lines file.
type Ledger struct {
	mu      sync.Mutex
	path    string
	entries []Entry
}

// Open loads the chain at path, or starts an empty in-memory one if path is "".
func Open(path string) *Ledger {
	l := &Ledger{path: path}
	if path == "" {
		return l
	}
	f, err := os.Open(path)
	if err != nil {
		return l
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		var e Entry
		if json.Unmarshal(sc.Bytes(), &e) == nil {
			l.entries = append(l.entries, e)
		}
	}
	return l
}

// Append adds an entry and returns it.
func (l *Ledger) Append(kind, severity, source, text string, at time.Time) Entry {
	l.mu.Lock()
	defer l.mu.Unlock()
	e := Entry{Seq: len(l.entries) + 1, Time: at.UnixMilli(), Kind: kind, Severity: severity, Source: source, Text: text}
	if n := len(l.entries); n > 0 {
		e.Seq, e.Prev = l.entries[n-1].Seq+1, l.entries[n-1].Hash
	}
	e.Hash = e.digest()
	l.entries = append(l.entries, e)
	if l.path != "" {
		if f, err := os.OpenFile(l.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
			b, _ := json.Marshal(e)
			f.Write(append(b, '\n'))
			f.Close()
		}
	}
	return e
}

// Verify walks the chain. It returns the sequence number of the first entry
// that does not check out, or 0 if the whole chain is intact.
func (l *Ledger) Verify() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	prev := ""
	for _, e := range l.entries {
		if e.Prev != prev || e.Hash != e.digest() {
			return e.Seq
		}
		prev = e.Hash
	}
	return 0
}

// Len is the number of entries; Head is the latest hash.
func (l *Ledger) Len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.entries)
}

func (l *Ledger) Head() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.entries) == 0 {
		return ""
	}
	return l.entries[len(l.entries)-1].Hash
}

// Span returns the entries with from <= time <= to (unix ms; 0 = open-ended).
func (l *Ledger) Span(from, to int64) []Entry {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []Entry
	for _, e := range l.entries {
		if (from == 0 || e.Time >= from) && (to == 0 || e.Time <= to) {
			out = append(out, e)
		}
	}
	return out
}

// Report writes a plain incident report for a span of the chain.
func Report(entries []Entry, intact bool) string {
	var b strings.Builder
	if len(entries) == 0 {
		return "No recorded events in that span."
	}
	first, last := time.UnixMilli(entries[0].Time), time.UnixMilli(entries[len(entries)-1].Time)
	fmt.Fprintf(&b, "INCIDENT REPORT\n%s to %s (%s)\n\n", first.Format("2006-01-02 15:04:05"), last.Format("15:04:05"), last.Sub(first).Round(time.Second))

	sev, sources := map[string]int{}, map[string]int{}
	actions, notes := 0, 0
	for _, e := range entries {
		if e.Kind == "action" {
			actions++
			continue
		}
		if e.Kind == "note" {
			notes++
			continue
		}
		sev[e.Severity]++
		if e.Source != "" {
			sources[e.Source]++
		}
	}
	fmt.Fprintf(&b, "Summary: %d events (%d critical, %d warning, %d info), %d operator actions and %d notes.\n", len(entries)-actions-notes, sev["CRITICAL"], sev["WARN"], sev["INFO"], actions, notes)
	if len(sources) > 0 {
		type kv struct {
			k string
			v int
		}
		var top []kv
		for k, v := range sources {
			top = append(top, kv{k, v})
		}
		sort.Slice(top, func(i, j int) bool { return top[i].v > top[j].v || (top[i].v == top[j].v && top[i].k < top[j].k) })
		b.WriteString("Most involved: ")
		for i, t := range top {
			if i == 3 {
				break
			}
			if i > 0 {
				b.WriteString(", ")
			}
			fmt.Fprintf(&b, "%s (%d)", t.k, t.v)
		}
		b.WriteString(".\n")
	}
	b.WriteString("\nTimeline:\n")
	for _, e := range entries {
		tag := e.Severity
		if e.Kind == "action" {
			tag = "ACTION"
		} else if e.Kind == "note" {
			tag = "NOTE"
		}
		fmt.Fprintf(&b, "  %s  %-8s %s\n", time.UnixMilli(e.Time).Format("15:04:05"), tag, e.Text)
	}
	if intact {
		fmt.Fprintf(&b, "\nRecord integrity: chain verified; entries %d–%d, head %s.\n", entries[0].Seq, entries[len(entries)-1].Seq, entries[len(entries)-1].Hash[:16])
	} else {
		b.WriteString("\nRecord integrity: THE CHAIN DOES NOT VERIFY. Treat this record as altered.\n")
	}
	return b.String()
}
