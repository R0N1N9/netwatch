// Package spectrum keeps an inventory of the radios the sensor nodes can hear —
// Wi-Fi and Bluetooth — and notices the ones that should not be there: a radio
// that lingers without ever joining the network, a tag that could be tracking
// you, a flood of disconnect frames, an access point imitating yours.
package spectrum

import (
	"sort"
	"strings"
	"sync"
	"time"
)

// WiFi is a Wi-Fi transmitter a node overheard.
type WiFi struct {
	MAC  string `json:"mac"`
	RSSI int    `json:"rssi"`
}

// BLE is a Bluetooth advertisement a node heard.
type BLE struct {
	Addr    string `json:"addr"`
	RSSI    int    `json:"rssi"`
	Name    string `json:"name,omitempty"`
	Company int    `json:"company,omitempty"` // Bluetooth company identifier
	Type    int    `json:"type,omitempty"`    // first byte of the maker-specific data
}

// Beacon is an access point announcement.
type Beacon struct {
	SSID  string `json:"ssid"`
	BSSID string `json:"bssid"`
	RSSI  int    `json:"rssi"`
}

// Report is what one node heard in one interval.
type Report struct {
	Node    string
	WiFi    []WiFi
	BLE     []BLE
	Beacons []Beacon
	Deauth  int // disconnect frames counted since the last report
}

// Radio is one transmitter in the inventory.
type Radio struct {
	ID      string  `json:"id"`
	Kind    string  `json:"kind"` // wifi | ble
	Name    string  `json:"name"`
	Tag     string  `json:"tag"` // network host | mine | unknown radio | lingering | bluetooth | tracker?
	First   int64   `json:"first"`
	Last    int64   `json:"last"`
	RSSI    int     `json:"rssi"`
	History []int   `json:"history"` // signal strength over time, oldest first
	Minutes float64 `json:"minutes"` // how long it has been around
}

// AP is an access point heard on the air.
type AP struct {
	SSID    string `json:"ssid"`
	BSSID   string `json:"bssid"`
	RSSI    int    `json:"rssi"`
	Trusted bool   `json:"trusted"`
}

// Event is something worth an alert.
type Event struct {
	Kind   string // rogue-radio | tracker | deauth | twin
	Source string
	Detail string
}

type radio struct {
	kind, name    string
	first, last   time.Time
	rssi          int
	hist          []int
	lastHist      time.Time
	tracker       bool
	flaggedLinger bool
	flaggedTrack  bool
}

// Engine is the radio inventory.
type Engine struct {
	mu      sync.Mutex
	radios  map[string]*radio
	aps     map[string]*AP  // by BSSID
	trusted map[string]bool // BSSIDs the operator accepted
	ssids   map[string]bool // network names those belong to
	deauth  []time.Time     // recent disconnect frames
	twins   map[string]time.Time
	mine    map[string]bool // radios the operator said are theirs
	lastDe  time.Time

	LingerAfter time.Duration // an unknown radio present this long is reported
	LingerRSSI  int           // ...if it is at least this strong; weaker ones are the neighbours'
	OnEvent     func(Event)
}

// New returns an empty inventory.
func New() *Engine {
	return &Engine{radios: map[string]*radio{}, aps: map[string]*AP{}, trusted: map[string]bool{}, ssids: map[string]bool{},
		twins: map[string]time.Time{}, mine: map[string]bool{}, LingerAfter: 5 * time.Minute, LingerRSSI: -70}
}

const (
	maxRadios = 400
	histLen   = 60
	histStep  = 5 * time.Second
	forget    = 3 * time.Minute
	// Apple's company identifier and the advertisement type its Find My network uses.
	appleID, findMy = 0x004C, 0x12
)

func (e *Engine) touch(id, kind, name string, rssi int, now time.Time) *radio {
	id = strings.ToLower(id)
	r := e.radios[id]
	if r == nil {
		if len(e.radios) >= maxRadios || len(id) < 11 || len(id) > 17 {
			return nil
		}
		r = &radio{kind: kind, first: now, rssi: rssi}
		e.radios[id] = r
	}
	r.last = now
	r.rssi = (r.rssi*2 + rssi) / 3
	if name != "" {
		r.name = name
	}
	if now.Sub(r.lastHist) >= histStep {
		r.lastHist = now
		r.hist = append(r.hist, r.rssi)
		if len(r.hist) > histLen {
			r.hist = r.hist[1:]
		}
	}
	return r
}

// Ingest folds in one node's report. known maps the hardware addresses of
// devices on the network to their names.
func (e *Engine) Ingest(rep Report, known map[string]string, now time.Time) {
	var events []Event
	e.mu.Lock()
	for _, w := range rep.WiFi {
		if w.RSSI >= 0 || w.RSSI < -100 {
			continue
		}
		r := e.touch(w.MAC, "wifi", "", w.RSSI, now)
		if r == nil {
			continue
		}
		if _, ok := known[strings.ToLower(w.MAC)]; !ok && !e.mine[strings.ToLower(w.MAC)] && !r.flaggedLinger && r.rssi >= e.LingerRSSI && now.Sub(r.first) >= e.LingerAfter {
			r.flaggedLinger = true
			events = append(events, Event{"rogue-radio", strings.ToLower(w.MAC), "a Wi-Fi radio has been in range for " + now.Sub(r.first).Round(time.Minute).String() + " without ever joining the network"})
		}
	}
	for _, b := range rep.BLE {
		if b.RSSI >= 0 || b.RSSI < -110 {
			continue
		}
		r := e.touch(b.Addr, "ble", b.Name, b.RSSI, now)
		if r == nil {
			continue
		}
		if b.Company == appleID && b.Type == findMy {
			r.tracker = true
		}
		if r.tracker && !e.mine[strings.ToLower(b.Addr)] && !r.flaggedTrack && now.Sub(r.first) >= e.LingerAfter {
			r.flaggedTrack = true
			events = append(events, Event{"tracker", strings.ToLower(b.Addr), "a location-tracker style Bluetooth tag has stayed in range for " + now.Sub(r.first).Round(time.Minute).String()})
		}
	}
	for _, b := range rep.Beacons {
		bssid := strings.ToLower(b.BSSID)
		if len(bssid) != 17 || len(b.SSID) > 32 {
			continue
		}
		ap := e.aps[bssid]
		if ap == nil {
			if len(e.aps) >= 200 {
				continue
			}
			ap = &AP{BSSID: bssid}
			e.aps[bssid] = ap
		}
		ap.SSID, ap.RSSI, ap.Trusted = b.SSID, b.RSSI, e.trusted[bssid]
		if e.ssids[b.SSID] && !e.trusted[bssid] {
			if last, ok := e.twins[bssid]; !ok || now.Sub(last) > 10*time.Minute {
				e.twins[bssid] = now
				events = append(events, Event{"twin", bssid, "an access point is announcing your network name \"" + b.SSID + "\" from an address you have not approved"})
			}
		}
	}
	for i := 0; i < rep.Deauth && i < 500; i++ {
		e.deauth = append(e.deauth, now)
	}
	cut := 0
	for cut < len(e.deauth) && now.Sub(e.deauth[cut]) > 10*time.Second {
		cut++
	}
	e.deauth = e.deauth[cut:]
	if len(e.deauth) >= 20 && now.Sub(e.lastDe) > time.Minute {
		e.lastDe = now
		events = append(events, Event{"deauth", rep.Node, "a burst of disconnect frames is in the air — devices are being forced off the network"})
	}
	for id, r := range e.radios {
		if now.Sub(r.last) > forget {
			delete(e.radios, id)
		}
	}
	cb := e.OnEvent
	e.mu.Unlock()
	if cb != nil {
		for _, ev := range events {
			cb(ev)
		}
	}
}

// TrustCurrent accepts every access point currently heard for the given
// network names. A later access point using one of those names is reported.
func (e *Engine) TrustCurrent(ssids []string) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	want := map[string]bool{}
	for _, s := range ssids {
		want[s] = true
	}
	n := 0
	for bssid, ap := range e.aps {
		if want[ap.SSID] {
			e.trusted[bssid], e.ssids[ap.SSID], ap.Trusted = true, true, true
			n++
		}
	}
	return n
}

// Snapshot returns the inventory, strongest signal first, and the access points.
func (e *Engine) Snapshot(known map[string]string, now time.Time) ([]Radio, []AP, int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	radios := make([]Radio, 0, len(e.radios))
	for id, r := range e.radios {
		out := Radio{ID: id, Kind: r.kind, Name: r.name, First: r.first.UnixMilli(), Last: r.last.UnixMilli(), RSSI: r.rssi,
			History: append([]int(nil), r.hist...), Minutes: now.Sub(r.first).Minutes()}
		switch name, ok := known[id]; {
		case ok:
			out.Tag, out.Name = "network host", name
		case e.mine[id]:
			out.Tag = "mine"
		case r.tracker:
			out.Tag = "tracker?"
		case r.kind == "ble":
			out.Tag = "bluetooth"
		case r.rssi >= e.LingerRSSI && now.Sub(r.first) >= e.LingerAfter:
			out.Tag = "lingering"
		default:
			out.Tag = "unknown radio"
		}
		radios = append(radios, out)
	}
	sort.Slice(radios, func(i, j int) bool {
		if radios[i].RSSI != radios[j].RSSI {
			return radios[i].RSSI > radios[j].RSSI
		}
		return radios[i].ID < radios[j].ID
	})
	aps := make([]AP, 0, len(e.aps))
	for _, ap := range e.aps {
		aps = append(aps, *ap)
	}
	sort.Slice(aps, func(i, j int) bool {
		return aps[i].RSSI > aps[j].RSSI || (aps[i].RSSI == aps[j].RSSI && aps[i].BSSID < aps[j].BSSID)
	})
	return radios, aps, len(e.deauth)
}

// Trusted returns the approved access points and the network names they carry.
func (e *Engine) Trusted() (bssids, ssids []string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for b := range e.trusted {
		bssids = append(bssids, b)
	}
	for s := range e.ssids {
		ssids = append(ssids, s)
	}
	sort.Strings(bssids)
	sort.Strings(ssids)
	return
}

// Restore loads a saved set of approved access points.
func (e *Engine) Restore(bssids, ssids []string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, b := range bssids {
		if len(b) == 17 && len(e.trusted) < 200 {
			e.trusted[strings.ToLower(b)] = true
		}
	}
	for _, s := range ssids {
		if len(s) <= 32 && len(e.ssids) < 200 {
			e.ssids[s] = true
		}
	}
}

// Distrust forgets every approved access point.
func (e *Engine) Distrust() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.trusted, e.ssids = map[string]bool{}, map[string]bool{}
	for _, ap := range e.aps {
		ap.Trusted = false
	}
}

// SetMine marks a radio as the operator's own (or not), so it is never reported.
func (e *Engine) SetMine(id string, mine bool) {
	id = strings.ToLower(id)
	e.mu.Lock()
	defer e.mu.Unlock()
	if !mine {
		delete(e.mine, id)
	} else if len(id) == 17 && len(e.mine) < 500 {
		e.mine[id] = true
	}
}

// Mine lists the radios the operator said are theirs.
func (e *Engine) Mine() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]string, 0, len(e.mine))
	for id := range e.mine {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}
