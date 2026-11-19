package hostwatch

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"time"
)

// Surface is a snapshot of what this machine and network expose.
type Surface struct {
	Taken       time.Time `json:"taken"`
	Listeners   []string  `json:"listeners"`
	Persistence []string  `json:"persistence"`
	Hosts       []string  `json:"hosts"`
	USB         []string  `json:"usb"`
}

// Diff lists what was added to and removed from each part of the surface.
func Diff(old, cur Surface) []string {
	var out []string
	part := func(name string, a, b []string) {
		in := func(list []string) map[string]bool {
			m := map[string]bool{}
			for _, x := range list {
				m[x] = true
			}
			return m
		}
		was, is := in(a), in(b)
		var add, gone []string
		for x := range is {
			if !was[x] {
				add = append(add, x)
			}
		}
		for x := range was {
			if !is[x] {
				gone = append(gone, x)
			}
		}
		sort.Strings(add)
		sort.Strings(gone)
		for _, x := range add {
			out = append(out, fmt.Sprintf("+ %-11s %s", name, x))
		}
		for _, x := range gone {
			out = append(out, fmt.Sprintf("- %-11s %s", name, x))
		}
	}
	part("listener", old.Listeners, cur.Listeners)
	part("persistence", old.Persistence, cur.Persistence)
	part("host", old.Hosts, cur.Hosts)
	part("usb", old.USB, cur.USB)
	return out
}

// SurfaceStore keeps one snapshot per day in a directory ("" = memory only).
type SurfaceStore struct {
	Dir string
	mem map[string]Surface
}

func (s *SurfaceStore) file(day string) string {
	return filepath.Join(s.Dir, "surface-"+day+".json")
}

// Baseline records cur as today's snapshot if there is none yet, and returns
// the snapshot to compare against: the most recent earlier day, else today's.
func (s *SurfaceStore) Baseline(cur Surface) (base Surface, since string, ok bool) {
	today := cur.Taken.Format("2006-01-02")
	days := s.days()
	if _, have := s.load(today); !have {
		s.save(today, cur)
	}
	for i := len(days) - 1; i >= 0; i-- {
		if days[i] < today {
			b, ok := s.load(days[i])
			return b, days[i], ok
		}
	}
	b, ok := s.load(today)
	return b, "NetWatch first ran today", ok
}

func (s *SurfaceStore) days() []string {
	var out []string
	if s.Dir == "" {
		for d := range s.mem {
			out = append(out, d)
		}
	} else {
		files, _ := filepath.Glob(s.file("*"))
		for _, f := range files {
			b := filepath.Base(f)
			out = append(out, b[len("surface-"):len(b)-len(".json")])
		}
	}
	sort.Strings(out)
	return out
}

func (s *SurfaceStore) load(day string) (Surface, bool) {
	if s.Dir == "" {
		v, ok := s.mem[day]
		return v, ok
	}
	b, err := os.ReadFile(s.file(day))
	if err != nil {
		return Surface{}, false
	}
	var v Surface
	return v, json.Unmarshal(b, &v) == nil
}

func (s *SurfaceStore) save(day string, v Surface) {
	if s.Dir == "" {
		if s.mem == nil {
			s.mem = map[string]Surface{}
		}
		s.mem[day] = v
		return
	}
	os.MkdirAll(s.Dir, 0o700)
	if b, err := json.Marshal(v); err == nil {
		os.WriteFile(s.file(day), b, 0o600)
	}
}

// Lock locks the desktop session. It is a variable so tests can replace it.
var Lock = func() error {
	if err := exec.Command("loginctl", "lock-session").Run(); err == nil {
		return nil
	}
	if err := exec.Command("xdg-screensaver", "lock").Run(); err == nil {
		return nil
	}
	return fmt.Errorf("could not lock the screen (tried loginctl and xdg-screensaver)")
}
