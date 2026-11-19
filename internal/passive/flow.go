package passive

import (
	"net"
	"sync"
	"time"
)

// FlowKey identifies a connection direction.
type FlowKey struct {
	Src, Dst string
	DPort    uint16
	Proto    uint8
}

// Flow aggregates traffic for one FlowKey.
type Flow struct {
	Key         FlowKey
	Bytes       uint64
	Packets     uint64
	First, Last time.Time
	Contacts    []float64 // inter-contact gaps (s), for beacon analysis
	lastContact time.Time
}

// FlowTable is a concurrent 5-tuple flow aggregator.
type FlowTable struct {
	mu    sync.Mutex
	flows map[FlowKey]*Flow
}

// NewFlowTable returns an empty table.
func NewFlowTable() *FlowTable { return &FlowTable{flows: map[FlowKey]*Flow{}} }

const idleGap = 1 * time.Second

// Observe records a packet.
func (ft *FlowTable) Observe(k FlowKey, size int, t time.Time) {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	f := ft.flows[k]
	if f == nil {
		f = &Flow{Key: k, First: t}
		ft.flows[k] = f
	}
	f.Bytes += uint64(size)
	f.Packets++
	f.Last = t
	if !f.lastContact.IsZero() && t.Sub(f.lastContact) > idleGap {
		gap := t.Sub(f.lastContact).Seconds()
		f.Contacts = append(f.Contacts, gap)
		if len(f.Contacts) > 64 {
			f.Contacts = f.Contacts[len(f.Contacts)-64:]
		}
	}
	f.lastContact = t
}

// Snapshot returns a copy of all flows.
func (ft *FlowTable) Snapshot() []Flow {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	out := make([]Flow, 0, len(ft.flows))
	for _, f := range ft.flows {
		cp := *f
		cp.Contacts = append([]float64(nil), f.Contacts...)
		out = append(out, cp)
	}
	return out
}

// Prune drops flows idle longer than maxIdle.
func (ft *FlowTable) Prune(maxIdle time.Duration, now time.Time) {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	for k, f := range ft.flows {
		if now.Sub(f.Last) > maxIdle {
			delete(ft.flows, k)
		}
	}
}

// DeviceFeatures derives per-source behavioral features for the anomaly model.
func (ft *FlowTable) DeviceFeatures(now time.Time) map[string]map[string]float64 {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	type agg struct {
		bytes  uint64
		dsts   map[string]struct{}
		dports map[uint16]struct{}
		flows  int
		first  time.Time
	}
	by := map[string]*agg{}
	for _, f := range ft.flows {
		a := by[f.Key.Src]
		if a == nil {
			a = &agg{dsts: map[string]struct{}{}, dports: map[uint16]struct{}{}, first: f.First}
			by[f.Key.Src] = a
		}
		a.bytes += f.Bytes
		a.dsts[f.Key.Dst] = struct{}{}
		a.dports[f.Key.DPort] = struct{}{}
		a.flows++
		if f.First.Before(a.first) {
			a.first = f.First
		}
	}
	out := map[string]map[string]float64{}
	for ip, a := range by {
		dur := now.Sub(a.first).Seconds()
		if dur < 1 {
			dur = 1
		}
		out[ip] = map[string]float64{
			"bps":    float64(a.bytes) / dur,
			"dsts":   float64(len(a.dsts)),
			"dports": float64(len(a.dports)),
			"flows":  float64(a.flows),
		}
	}
	return out
}

// IsPublic reports whether ip is a routable public address.
func IsPublic(ip net.IP) bool {
	return ip != nil && !ip.IsPrivate() && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() && !ip.IsMulticast() && !ip.IsUnspecified()
}
