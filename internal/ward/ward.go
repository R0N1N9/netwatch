// Package ward learns what each device normally does on the network — who it
// talks to, on which ports, at what hours, how much — and reports when it
// steps outside that profile.
package ward

import (
	"fmt"
	"sort"
	"sync"
	"time"
)

// Obs is one observed conversation by a device.
type Obs struct {
	Host  string `json:"host"` // the device
	Dst   string `json:"dst"`  // server name if known, else address
	Port  uint16 `json:"port"`
	Bytes uint64 `json:"bytes"`
	Hour  int    `json:"hour"` // local hour 0-23
	JA3   string `json:"ja3"`  // TLS fingerprint, if seen
}

// Deviation is a step outside the learned profile.
type Deviation struct {
	Time   time.Time `json:"time"`
	Host   string    `json:"host"`
	Kind   string    `json:"kind"` // new-destination | new-port | odd-hour | volume | fingerprint
	Detail string    `json:"detail"`
	Seen   Obs       `json:"seen"` // what was observed, so it can be accepted
}

// Profile is what a device has been seen to do.
type Profile struct {
	Host     string         `json:"host"`
	Learning bool           `json:"learning"`
	Samples  int            `json:"samples"`
	Since    int64          `json:"since"` // unix seconds of the first observation
	Dsts     map[string]int `json:"dsts"`
	Ports    map[uint16]int `json:"ports"`
	Hours    [24]int        `json:"hours"`
	MaxBytes uint64         `json:"maxBytes"`
	JA3      string         `json:"ja3"`
}

// Ward holds a profile per device.
type Ward struct {
	mu       sync.Mutex
	LearnN   int           // observations before a profile is enforced
	MinAge   time.Duration // and how long it must have been watched (so every hour of the day is seen)
	profiles map[string]*Profile
	seen     map[string]time.Time // deviation key -> last reported
	devs     []Deviation
}

// New returns a ward that learns for n observations per device.
func New(n int) *Ward {
	return &Ward{LearnN: n, profiles: map[string]*Profile{}, seen: map[string]time.Time{}}
}

func (w *Ward) profile(host string) *Profile {
	p := w.profiles[host]
	if p == nil {
		p = &Profile{Host: host, Learning: true, Dsts: map[string]int{}, Ports: map[uint16]int{}}
		w.profiles[host] = p
	}
	return p
}

// Observe folds an observation in. While a device is still being learned it
// only records; afterwards it returns any deviations (each reported at most
// once an hour).
func (w *Ward) Observe(o Obs, now time.Time) []Deviation {
	w.mu.Lock()
	defer w.mu.Unlock()
	p := w.profile(o.Host)
	if p.Since == 0 {
		p.Since = now.Unix()
	}
	if p.Learning {
		p.Samples++
		p.Dsts[o.Dst]++
		p.Ports[o.Port]++
		p.Hours[o.Hour%24]++
		if o.Bytes > p.MaxBytes {
			p.MaxBytes = o.Bytes
		}
		if o.JA3 != "" && p.JA3 == "" {
			p.JA3 = o.JA3
		}
		if p.Samples >= w.LearnN && now.Sub(time.Unix(p.Since, 0)) >= w.MinAge {
			p.Learning = false
		}
		return nil
	}
	var out []Deviation
	add := func(kind, detail string) {
		key := o.Host + "|" + kind + "|" + detail
		if last, ok := w.seen[key]; ok && now.Sub(last) < time.Hour {
			return
		}
		w.seen[key] = now
		d := Deviation{Time: now, Host: o.Host, Kind: kind, Detail: detail, Seen: o}
		out = append(out, d)
		w.devs = append(w.devs, d)
		if len(w.devs) > 200 {
			w.devs = w.devs[len(w.devs)-200:]
		}
	}
	if p.Dsts[o.Dst] == 0 {
		add("new-destination", "talked to "+o.Dst+", which it never has before")
	}
	if p.Ports[o.Port] == 0 {
		add("new-port", fmt.Sprintf("used port %d for the first time", o.Port))
	}
	if p.Hours[o.Hour%24] == 0 {
		add("odd-hour", fmt.Sprintf("active at %02d:00, an hour it is normally silent", o.Hour%24))
	}
	if p.MaxBytes > 0 && o.Bytes > p.MaxBytes*3 {
		add("volume", fmt.Sprintf("sent %d bytes in one conversation, over three times its usual ceiling of %d", o.Bytes, p.MaxBytes))
	}
	if o.JA3 != "" && p.JA3 != "" && o.JA3 != p.JA3 {
		add("fingerprint", "its TLS fingerprint changed — new firmware or different software is talking")
	}
	return out
}

// Accept adds an observation to a device's profile, so it stops being flagged.
func (w *Ward) Accept(o Obs) {
	w.mu.Lock()
	defer w.mu.Unlock()
	p := w.profile(o.Host)
	p.Dsts[o.Dst]++
	p.Ports[o.Port]++
	p.Hours[o.Hour%24]++
	if o.Bytes > p.MaxBytes {
		p.MaxBytes = o.Bytes
	}
	if o.JA3 != "" {
		p.JA3 = o.JA3
	}
	keep := w.devs[:0]
	for _, d := range w.devs {
		if d.Seen != o {
			keep = append(keep, d)
		}
	}
	w.devs = keep
}

// Relearn puts a device back into learning from scratch.
func (w *Ward) Relearn(host string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.profiles, host)
}

// Profiles returns a copy of every profile, sorted by host.
func (w *Ward) Profiles() []Profile {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make([]Profile, 0, len(w.profiles))
	for _, p := range w.profiles {
		c := *p
		c.Dsts, c.Ports = map[string]int{}, map[uint16]int{}
		for k, v := range p.Dsts {
			c.Dsts[k] = v
		}
		for k, v := range p.Ports {
			c.Ports[k] = v
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Host < out[j].Host })
	return out
}

// Deviations returns recorded deviations, newest first.
func (w *Ward) Deviations() []Deviation {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make([]Deviation, len(w.devs))
	for i, d := range w.devs {
		out[len(w.devs)-1-i] = d
	}
	return out
}

// Export returns every profile for saving.
func (w *Ward) Export() []Profile { return w.Profiles() }

// Import restores saved profiles.
func (w *Ward) Import(ps []Profile) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, p := range ps {
		if p.Host == "" || len(w.profiles) >= 512 {
			continue
		}
		c := p
		if c.Dsts == nil {
			c.Dsts = map[string]int{}
		}
		if c.Ports == nil {
			c.Ports = map[uint16]int{}
		}
		w.profiles[p.Host] = &c
	}
}
