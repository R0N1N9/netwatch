package sonar

import (
	"math"
	"sort"
	"strings"
	"sync"
	"time"
)

// Pt is a position on the room map (map units; 1 unit = MetresPerUnit).
type Pt struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// MetresPerUnit is the scale of the room map.
const MetresPerUnit = 4.4

// APKey is the layout key of the access point.
const APKey = "@ap"

// Wire is a virtual tripwire or a wall: a line segment on the map.
type Wire struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	A    Pt     `json:"a"`
	B    Pt     `json:"b"`
}

// NodePos returns where a node sits: its layout position, pulled to its
// measured range from the access point when it has one.
func NodePos(n NodeState, layout map[string]Pt) (Pt, bool) {
	base, ok := layout[n.Node]
	if !ok {
		return Pt{}, false
	}
	ap, hasAP := layout[APKey]
	if n.DistM <= 0 || !hasAP {
		return base, true
	}
	dx, dy := base.X-ap.X, base.Y-ap.Y
	l := math.Hypot(dx, dy)
	if l < 1e-3 {
		dx, dy, l = 1, 0, 1
	}
	r := n.DistM / MetresPerUnit
	return Pt{ap.X + dx/l*r, ap.Y + dy/l*r}, true
}

// Estimate blends the midpoints of the disturbed links, weighted by score, into
// one rough position for the movement.
func Estimate(nodes []NodeState, layout map[string]Pt) (Pt, bool) {
	ap, ok := layout[APKey]
	if !ok {
		return Pt{}, false
	}
	var wx, wy, w float64
	for _, n := range nodes {
		if n.State != "motion" {
			continue
		}
		p, ok := NodePos(n, layout)
		if !ok {
			continue
		}
		wx += (ap.X + p.X) / 2 * n.Score
		wy += (ap.Y + p.Y) / 2 * n.Score
		w += n.Score
	}
	if w <= 0 {
		return Pt{}, false
	}
	return Pt{wx / w, wy / w}, true
}

// People is a rough head count: links that are disturbed or carry breathing,
// grouped when they point the same way from the access point.
func People(nodes []NodeState, layout map[string]Pt) int {
	ap, hasAP := layout[APKey]
	var bearings []float64
	loose := 0
	for _, n := range nodes {
		if n.State != "motion" && n.Breath <= 0 {
			continue
		}
		p, ok := NodePos(n, layout)
		if !ok || !hasAP {
			loose++
			continue
		}
		bearings = append(bearings, math.Atan2(p.Y-ap.Y, p.X-ap.X)*180/math.Pi)
	}
	if len(bearings) == 0 {
		return loose
	}
	sort.Float64s(bearings)
	groups := 1
	for i := 1; i < len(bearings); i++ {
		if bearings[i]-bearings[i-1] > 35 {
			groups++
		}
	}
	if len(bearings) > 1 && groups > 1 && bearings[0]+360-bearings[len(bearings)-1] <= 35 {
		groups-- // first and last wrap around to the same direction
	}
	return groups + loose
}

// Crossed reports whether moving from p to q crosses the segment w.
func Crossed(p, q Pt, w Wire) bool {
	d := func(a, b, c Pt) float64 { return (b.X-a.X)*(c.Y-a.Y) - (b.Y-a.Y)*(c.X-a.X) }
	d1, d2, d3, d4 := d(w.A, w.B, p), d(w.A, w.B, q), d(p, q, w.A), d(p, q, w.B)
	return ((d1 > 0 && d2 < 0) || (d1 < 0 && d2 > 0)) && ((d3 > 0 && d4 < 0) || (d3 < 0 && d4 > 0))
}

// ---- locating radios (network hosts) from the signal strength each node hears ----

// Sighting is one device a node overheard.
type Sighting struct {
	MAC  string `json:"mac"`
	RSSI int    `json:"rssi"`
}

// Radio is a located device.
type Radio struct {
	MAC   string  `json:"mac"`
	X     float64 `json:"x"`
	Y     float64 `json:"y"`
	Nodes int     `json:"nodes"` // how many nodes hear it; 3+ gives a real fix
	RSSI  int     `json:"rssi"`  // strongest reading
}

type rssiObs struct {
	ewma float64
	at   time.Time
}

type radioBook struct {
	mu   sync.Mutex
	seen map[string]map[string]*rssiObs // mac -> node -> reading
}

const (
	maxRadios   = 256
	radioFresh  = 30 * time.Second
	rssiAt1m    = -40.0 // typical reading one metre from a transmitter
	pathLossExp = 2.5   // indoor path-loss exponent
)

func (b *radioBook) add(node string, s []Sighting, now time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.seen == nil {
		b.seen = map[string]map[string]*rssiObs{}
	}
	for _, x := range s {
		x.MAC = strings.ToLower(x.MAC)
		if len(x.MAC) != 17 || x.RSSI >= 0 || x.RSSI < -100 {
			continue
		}
		m := b.seen[x.MAC]
		if m == nil {
			if len(b.seen) >= maxRadios {
				continue
			}
			m = map[string]*rssiObs{}
			b.seen[x.MAC] = m
		}
		o := m[node]
		if o == nil {
			m[node] = &rssiObs{ewma: float64(x.RSSI), at: now}
			continue
		}
		o.ewma, o.at = o.ewma*0.7+float64(x.RSSI)*0.3, now
	}
}

// RSSIToMetres converts a reading to an approximate distance.
func RSSIToMetres(rssi float64) float64 {
	return math.Pow(10, (rssiAt1m-rssi)/(10*pathLossExp))
}

// Radios locates every recently heard device against the node layout.
func (e *Engine) Radios(now time.Time, nodes []NodeState, layout map[string]Pt) []Radio {
	pos := map[string]Pt{}
	for _, n := range nodes {
		if p, ok := NodePos(n, layout); ok {
			pos[n.Node] = p
		}
	}
	e.radios.mu.Lock()
	defer e.radios.mu.Unlock()
	var out []Radio
	for mac, byNode := range e.radios.seen {
		var anchors []Pt
		var dists []float64
		best := -200.0
		for node, o := range byNode {
			if now.Sub(o.at) > radioFresh {
				delete(byNode, node)
				continue
			}
			p, ok := pos[node]
			if !ok {
				continue
			}
			anchors = append(anchors, p)
			dists = append(dists, RSSIToMetres(o.ewma)/MetresPerUnit)
			best = math.Max(best, o.ewma)
		}
		if len(byNode) == 0 {
			delete(e.radios.seen, mac)
			continue
		}
		if len(anchors) == 0 {
			continue
		}
		p := locate(anchors, dists)
		out = append(out, Radio{MAC: mac, X: p.X, Y: p.Y, Nodes: len(anchors), RSSI: int(math.Round(best))})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].MAC < out[j].MAC })
	return out
}

// locate finds the point whose distances to the anchors best match the ranges.
func locate(anchors []Pt, dists []float64) Pt {
	var p Pt
	var w float64
	for i, a := range anchors { // start from the range-weighted centre
		k := 1 / (dists[i]*dists[i] + 1e-3)
		p.X, p.Y, w = p.X+a.X*k, p.Y+a.Y*k, w+k
	}
	p.X, p.Y = p.X/w, p.Y/w
	if len(anchors) < 3 {
		return p
	}
	step := 0.2
	for it := 0; it < 80; it++ { // descend on the squared range error
		var gx, gy float64
		for i, a := range anchors {
			dx, dy := p.X-a.X, p.Y-a.Y
			r := math.Hypot(dx, dy) + 1e-6
			k := (r - dists[i]) / r
			gx, gy = gx+k*dx, gy+k*dy
		}
		g := math.Hypot(gx, gy)
		if g < 1e-6 {
			break
		}
		p.X, p.Y = p.X-step*gx/g*math.Min(1, g), p.Y-step*gy/g*math.Min(1, g)
		step *= 0.96
	}
	return p
}
