// Package sonar turns Wi-Fi Channel State Information (CSI) from ESP32-S3 nodes
// into presence sensing. A body moving through the path between an access
// point and a node disturbs the per-subcarrier amplitudes; the engine measures
// that turbulence against a calm baseline and reports motion per node.
//
// It is zone-level sensing (which link is disturbed), not coordinates.
package sonar

import (
	"context"
	"encoding/json"
	"math"
	"math/rand"
	"net"
	"sort"
	"sync"
	"time"
)

// Frame is one CSI sample from a node: the amplitude of each subcarrier.
type Frame struct {
	Node   string    `json:"node"`
	RSSI   int       `json:"rssi"`
	Amp    []float64 `json:"amp"`
	DistCM float64   `json:"dist_cm,omitempty"` // Wi-Fi FTM range to the access point, if the node measured one
}

// NodeState is the current reading for one node.
type NodeState struct {
	Node  string    `json:"node"`
	State string    `json:"state"` // calibrating | clear | motion | offline
	Score float64   `json:"score"` // 0-100 turbulence above baseline
	RSSI  int       `json:"rssi"`
	FPS   float64   `json:"fps"`
	AgeMS int64     `json:"ageMs"`
	Trace []float64 `json:"trace"` // recent scores, oldest first

	DistM      float64 `json:"distM"`      // measured range to the access point (0 = not measured)
	Breath     float64 `json:"breath"`     // breaths per minute while the link is otherwise still (0 = none found)
	BreathConf float64 `json:"breathConf"` // spectral peak over the band median
}

const (
	window    = 40  // frames in the turbulence window
	calibN    = 50  // turbulence samples used to set the baseline
	enterAt   = 2.5 // ratio to baseline that starts a motion event
	exitAt    = 1.6 // ratio that ends it (hysteresis)
	traceLen  = 90
	traceStep = 200 * time.Millisecond
	maxNodes  = 32
	offlineIn = 5 * time.Second

	slowStep   = 200 * time.Millisecond // breathing analysis runs on a ~5 Hz series
	slowRows   = 150                    // 30 s
	slowMin    = 100                    // need 20 s of stillness before looking
	breathConf = 6.0
)

type node struct {
	ring      [][]float64
	calib     []float64
	baseline  float64
	motion    bool
	score     float64
	rssi      int
	last      time.Time
	lastTrace time.Time
	fps       float64
	trace     []float64

	dists []float64 // recent FTM ranges (cm)

	acc        []float64 // frames being averaged into the next slow row
	accN       int
	accStart   time.Time
	slow       [][]float64
	slowT      []time.Time // when each slow row closed, so the real sample rate is known
	lastBreath time.Time
	breath     float64
	bConf      float64
}

// Engine tracks every reporting node.
type Engine struct {
	mu    sync.Mutex
	nodes map[string]*node

	// OnMotion fires on the rising edge of a motion event (called without the lock).
	OnMotion func(node string, score float64)
}

// New returns an empty engine.
func New() *Engine { return &Engine{nodes: map[string]*node{}} }

// Ingest folds one frame into the node's state.
func (e *Engine) Ingest(f Frame, now time.Time) {
	if f.Node == "" || len(f.Node) > 32 || len(f.Amp) < 8 || len(f.Amp) > 512 {
		return
	}
	var sum float64
	for _, a := range f.Amp {
		if math.IsNaN(a) || math.IsInf(a, 0) || a < 0 {
			return
		}
		sum += a
	}
	mean := sum / float64(len(f.Amp))
	if mean <= 0 {
		return
	}
	// Normalise by the frame mean so automatic gain changes don't read as motion.
	norm := make([]float64, len(f.Amp))
	for i, a := range f.Amp {
		norm[i] = a / mean
	}

	var fire bool
	var fireScore float64

	e.mu.Lock()
	n := e.nodes[f.Node]
	if n == nil {
		if len(e.nodes) >= maxNodes {
			e.mu.Unlock()
			return
		}
		n = &node{}
		e.nodes[f.Node] = n
	}
	if len(n.ring) > 0 && len(n.ring[0]) != len(norm) {
		n.ring, n.calib, n.baseline, n.motion = nil, nil, 0, false
	}
	if !n.last.IsZero() {
		if dt := now.Sub(n.last).Seconds(); dt > 0 {
			n.fps = n.fps*0.9 + (1/dt)*0.1
		}
	}
	n.last, n.rssi = now, f.RSSI
	if f.DistCM > 0 && f.DistCM < 20000 {
		n.dists = append(n.dists, f.DistCM)
		if len(n.dists) > 9 {
			n.dists = n.dists[1:]
		}
	}
	n.slowAdd(norm, now)
	n.ring = append(n.ring, norm)
	if len(n.ring) > window {
		n.ring = n.ring[1:]
	}
	if len(n.ring) == window {
		v := turbulence(n.ring)
		switch {
		case len(n.calib) < calibN:
			n.calib = append(n.calib, v)
			if len(n.calib) == calibN {
				n.baseline = median(n.calib)
			}
		default:
			r := v / math.Max(n.baseline, 1e-6)
			if !n.motion && r > enterAt {
				n.motion, fire = true, true
				n.slow, n.slowT, n.breath, n.bConf = nil, nil, 0, 0 // movement swamps breathing; start over
			} else if n.motion && r < exitAt {
				n.motion = false
			}
			if r < exitAt { // adapt slowly, and only while calm
				n.baseline = n.baseline*0.98 + v*0.02
			}
			n.score = math.Max(0, math.Min(100, (r-1)*45))
			fireScore = n.score
		}
		if now.Sub(n.lastTrace) >= traceStep {
			n.lastTrace = now
			n.trace = append(n.trace, n.score)
			if len(n.trace) > traceLen {
				n.trace = n.trace[1:]
			}
		}
	}
	cb := e.OnMotion
	e.mu.Unlock()

	if fire && cb != nil {
		cb(f.Node, fireScore)
	}
}

// Snapshot returns every node's state, sorted by name.
func (e *Engine) Snapshot(now time.Time) []NodeState {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]NodeState, 0, len(e.nodes))
	for name, n := range e.nodes {
		st := NodeState{
			Node: name, Score: math.Round(n.score*10) / 10, RSSI: n.rssi,
			FPS: math.Round(n.fps*10) / 10, AgeMS: now.Sub(n.last).Milliseconds(),
			Trace:  append([]float64(nil), n.trace...),
			Breath: n.breath, BreathConf: math.Round(n.bConf*10) / 10,
		}
		if len(n.dists) > 0 {
			st.DistM = math.Round(median(n.dists)) / 100
		}
		switch {
		case now.Sub(n.last) > offlineIn:
			st.State, st.Score = "offline", 0
		case n.baseline == 0:
			st.State = "calibrating"
		case n.motion:
			st.State = "motion"
		default:
			st.State = "clear"
		}
		out = append(out, st)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Node < out[j].Node })
	return out
}

// ListenUDP ingests JSON frames (one per datagram) until ctx ends.
func (e *Engine) ListenUDP(ctx context.Context, addr string) error {
	pc, err := net.ListenPacket("udp", addr)
	if err != nil {
		return err
	}
	go func() { <-ctx.Done(); pc.Close() }()
	buf := make([]byte, 8192)
	for {
		n, _, err := pc.ReadFrom(buf)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			continue
		}
		var f Frame
		if json.Unmarshal(buf[:n], &f) == nil {
			e.Ingest(f, time.Now())
		}
	}
}

// slowAdd averages frames into a 5 Hz series and looks for breathing in it.
func (n *node) slowAdd(norm []float64, now time.Time) {
	if len(n.acc) != len(norm) {
		n.acc, n.accN, n.slow, n.slowT = make([]float64, len(norm)), 0, nil, nil
	}
	if n.accN == 0 {
		n.accStart = now
	}
	for i, v := range norm {
		n.acc[i] += v
	}
	n.accN++
	if now.Sub(n.accStart) < slowStep {
		return
	}
	row := make([]float64, len(norm))
	for i := range row {
		row[i] = n.acc[i] / float64(n.accN)
		n.acc[i] = 0
	}
	n.accN = 0
	if n.motion {
		return
	}
	n.slow, n.slowT = append(n.slow, row), append(n.slowT, now)
	if len(n.slow) > slowRows {
		n.slow, n.slowT = n.slow[1:], n.slowT[1:]
	}
	if len(n.slow) >= slowMin && now.Sub(n.lastBreath) >= 2*time.Second {
		n.lastBreath = now
		span := n.slowT[len(n.slowT)-1].Sub(n.slowT[0]).Seconds()
		if span <= 0 {
			return
		}
		bpm, conf := Breathing(n.slow, float64(len(n.slow)-1)/span)
		n.bConf = conf
		if conf >= breathConf {
			n.breath = math.Round(bpm)
		} else {
			n.breath = 0
		}
	}
}

// Breathing looks for a shared periodic component between 9 and 30 cycles per
// minute across the subcarriers of a still link. It returns the rate and how
// far the spectral peak stands above the median of the search band.
func Breathing(rows [][]float64, fs float64) (bpm, conf float64) {
	n := len(rows)
	if n < 20 || len(rows[0]) == 0 {
		return 0, 0
	}
	const fLo, fHi, fStep = 0.08, 0.70, 0.01
	nf := int((fHi-fLo)/fStep) + 1
	cosT, sinT := make([][]float64, nf), make([][]float64, nf)
	for fi := 0; fi < nf; fi++ {
		f := fLo + float64(fi)*fStep
		cosT[fi], sinT[fi] = make([]float64, n), make([]float64, n)
		for i := 0; i < n; i++ {
			a := 2 * math.Pi * f * float64(i) / fs
			cosT[fi][i], sinT[fi][i] = math.Cos(a), math.Sin(a)
		}
	}
	spec := make([]float64, nf)
	x := make([]float64, n)
	for c := 0; c < len(rows[0]); c++ {
		var mean float64
		for i := 0; i < n; i++ {
			mean += rows[i][c]
		}
		mean /= float64(n)
		for i := 0; i < n; i++ {
			x[i] = rows[i][c] - mean
		}
		for fi := 0; fi < nf; fi++ {
			var re, im float64
			for i := 0; i < n; i++ {
				re += x[i] * cosT[fi][i]
				im += x[i] * sinT[fi][i]
			}
			spec[fi] += re*re + im*im
		}
	}
	best, bestF := 0.0, 0.0
	for fi, p := range spec {
		f := fLo + float64(fi)*fStep
		if f >= 0.15 && f <= 0.5 && p > best {
			best, bestF = p, f
		}
	}
	med := median(spec)
	if med <= 0 {
		return 0, 0
	}
	return bestF * 60, best / med
}

// turbulence is the mean coefficient of variation across subcarriers.
func turbulence(ring [][]float64) float64 {
	k := len(ring[0])
	var total float64
	for c := 0; c < k; c++ {
		var sum, sq float64
		for _, fr := range ring {
			sum += fr[c]
		}
		m := sum / float64(len(ring))
		for _, fr := range ring {
			d := fr[c] - m
			sq += d * d
		}
		total += math.Sqrt(sq/float64(len(ring))) / (m + 1e-9)
	}
	return total / float64(k)
}

func median(v []float64) float64 {
	s := append([]float64(nil), v...)
	sort.Float64s(s)
	return s[len(s)/2]
}

// Sim generates plausible CSI frames for demos and tests (no hardware needed).
type Sim struct {
	node string
	base []float64
	t    float64
	rng  *rand.Rand

	BreathBPM float64 // if > 0, a still person breathing at this rate sits on the link
	DistM     float64 // if > 0, report this FTM range (with jitter)
}

// NewSim returns a simulator for one node with 52 subcarriers.
func NewSim(nodeName string, seed int64) *Sim {
	s := &Sim{node: nodeName, rng: rand.New(rand.NewSource(seed))}
	for k := 0; k < 52; k++ {
		s.base = append(s.base, 20+8*math.Sin(float64(k)/6+float64(seed)))
	}
	return s
}

// Frame returns the next frame; motion is 0 (still room) to 1 (someone walking the link).
func (s *Sim) Frame(motion float64) Frame {
	s.t += 0.04
	amp := make([]float64, len(s.base))
	for k, b := range s.base {
		a := b * (1 + s.rng.NormFloat64()*0.012)
		if motion > 0 {
			a *= 1 + motion*0.3*math.Sin(2*math.Pi*1.3*s.t+float64(k)*0.35) + s.rng.NormFloat64()*0.05*motion
		}
		if s.BreathBPM > 0 && motion == 0 {
			a *= 1 + 0.012*math.Sin(2*math.Pi*(s.BreathBPM/60)*s.t+float64(k)*0.2)
		}
		amp[k] = math.Max(0.1, a)
	}
	f := Frame{Node: s.node, RSSI: -48 - s.rng.Intn(6), Amp: amp}
	if s.DistM > 0 {
		f.DistCM = s.DistM*100 + s.rng.NormFloat64()*12
	}
	return f
}
