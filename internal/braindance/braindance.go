// Package braindance records what SONAR sensed and plays it back later: a
// rolling buffer of the presence field plus saved clips you can replay in the
// room view. Clips are stored as JSON files, or in memory when no directory is
// given.
package braindance

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"sync"
	"time"
)

// NodeSample is one node's reading at one instant.
type NodeSample struct {
	Node   string  `json:"n"`
	State  string  `json:"s"`
	Score  float64 `json:"v"`
	Breath float64 `json:"b,omitempty"`
	DistM  float64 `json:"d,omitempty"`
}

// Sample is the whole field at one instant (T in unix milliseconds).
type Sample struct {
	T     int64        `json:"t"`
	Nodes []NodeSample `json:"n"`
}

// Point is a position on the room map.
type Point struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// Clip is a saved recording. Samples is empty in listings.
type Clip struct {
	ID      string           `json:"id"`
	Name    string           `json:"name"`
	Start   int64            `json:"start"`
	End     int64            `json:"end"`
	Peak    float64          `json:"peak"`
	Auto    bool             `json:"auto"`
	Layout  map[string]Point `json:"layout,omitempty"`
	Samples []Sample         `json:"samples,omitempty"`
}

const (
	bufferCap  = 15000 // ~50 min at 5 Hz
	clipCap    = 3000  // ~10 min
	maxClips   = 200
	maxNameLen = 60
)

var idRe = regexp.MustCompile(`^bd-[0-9]{10,16}$`)

// Recorder holds the rolling buffer and the clip library.
type Recorder struct {
	mu       sync.Mutex
	dir      string
	buf      []Sample
	clips    []Clip              // metadata only
	mem      map[string][]Sample // clip samples when dir == ""
	recStart int64
}

// New opens (or creates) a library in dir. An empty dir keeps clips in memory.
func New(dir string) *Recorder {
	r := &Recorder{dir: dir, mem: map[string][]Sample{}}
	if dir == "" {
		return r
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		r.dir = ""
		return r
	}
	files, _ := filepath.Glob(filepath.Join(dir, "bd-*.json"))
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var c Clip
		if json.Unmarshal(b, &c) == nil && idRe.MatchString(c.ID) {
			c.Samples = nil
			r.clips = append(r.clips, c)
		}
	}
	return r
}

// Dir reports where clips are stored ("" = memory only).
func (r *Recorder) Dir() string { return r.dir }

// Add appends a sample to the rolling buffer.
func (r *Recorder) Add(s Sample) {
	r.mu.Lock()
	r.buf = append(r.buf, s)
	if len(r.buf) > bufferCap {
		r.buf = r.buf[len(r.buf)-bufferCap:]
	}
	r.mu.Unlock()
}

// BufferSeconds is how far back the rolling buffer reaches.
func (r *Recorder) BufferSeconds() float64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.buf) < 2 {
		return 0
	}
	return float64(r.buf[len(r.buf)-1].T-r.buf[0].T) / 1000
}

// StartRec marks the beginning of a manual recording.
func (r *Recorder) StartRec(now time.Time) {
	r.mu.Lock()
	r.recStart = now.UnixMilli()
	r.mu.Unlock()
}

// RecordingSince returns the start of the manual recording in unix ms, or 0.
func (r *Recorder) RecordingSince() int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.recStart
}

// StopRec ends the manual recording and saves it as a clip.
func (r *Recorder) StopRec(name string, now time.Time, layout map[string]Point) (Clip, error) {
	r.mu.Lock()
	from := r.recStart
	r.recStart = 0
	r.mu.Unlock()
	if from == 0 {
		return Clip{}, errors.New("not recording")
	}
	return r.Save(name, from, now.UnixMilli(), layout, false)
}

// Save cuts [from, to] (unix ms) out of the rolling buffer into a new clip.
func (r *Recorder) Save(name string, from, to int64, layout map[string]Point, auto bool) (Clip, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var cut []Sample
	for _, s := range r.buf {
		if s.T >= from && s.T <= to {
			cut = append(cut, s)
		}
	}
	if len(cut) < 2 {
		return Clip{}, errors.New("nothing was recorded in that span")
	}
	if len(cut) > clipCap {
		cut = cut[len(cut)-clipCap:]
	}
	c := Clip{ID: fmt.Sprintf("bd-%d", cut[0].T), Start: cut[0].T, End: cut[len(cut)-1].T, Auto: auto, Layout: layout}
	for _, existing := range r.clips {
		if existing.ID == c.ID {
			return Clip{}, errors.New("a clip starting at that moment already exists")
		}
	}
	for _, s := range cut {
		for _, n := range s.Nodes {
			if n.Score > c.Peak {
				c.Peak = n.Score
			}
		}
	}
	if len(name) > maxNameLen {
		name = name[:maxNameLen]
	}
	if name == "" {
		name = time.UnixMilli(c.Start).Format("Jan 2 15:04:05")
	}
	c.Name = name
	if r.dir != "" {
		full := c
		full.Samples = cut
		b, err := json.Marshal(full)
		if err != nil {
			return Clip{}, err
		}
		if err := os.WriteFile(filepath.Join(r.dir, c.ID+".json"), b, 0o600); err != nil {
			return Clip{}, err
		}
	} else {
		r.mem[c.ID] = cut
	}
	r.clips = append(r.clips, c)
	for len(r.clips) > maxClips { // drop the oldest
		r.removeLocked(r.clips[0].ID)
	}
	return c, nil
}

// List returns clip metadata, newest first.
func (r *Recorder) List() []Clip {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := append([]Clip(nil), r.clips...)
	sort.Slice(out, func(i, j int) bool { return out[i].Start > out[j].Start })
	return out
}

// Load returns a clip with its samples.
func (r *Recorder) Load(id string) (Clip, bool) {
	if !idRe.MatchString(id) {
		return Clip{}, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, c := range r.clips {
		if c.ID != id {
			continue
		}
		if r.dir == "" {
			c.Samples = r.mem[id]
			return c, true
		}
		b, err := os.ReadFile(filepath.Join(r.dir, id+".json"))
		if err != nil {
			return Clip{}, false
		}
		var full Clip
		if json.Unmarshal(b, &full) != nil {
			return Clip{}, false
		}
		return full, true
	}
	return Clip{}, false
}

// Delete removes a clip.
func (r *Recorder) Delete(id string) bool {
	if !idRe.MatchString(id) {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.removeLocked(id)
}

func (r *Recorder) removeLocked(id string) bool {
	for i, c := range r.clips {
		if c.ID == id {
			r.clips = append(r.clips[:i], r.clips[i+1:]...)
			delete(r.mem, id)
			if r.dir != "" {
				os.Remove(filepath.Join(r.dir, id+".json"))
			}
			return true
		}
	}
	return false
}
