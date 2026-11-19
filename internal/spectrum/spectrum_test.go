package spectrum

import (
	"testing"
	"time"
)

func TestInventoryAndEvents(t *testing.T) {
	e := New()
	var events []Event
	e.OnEvent = func(ev Event) { events = append(events, ev) }
	known := map[string]string{"b8:27:eb:11:22:33": "pi-hole.lan"}
	now := time.Unix(1_700_000_000, 0)
	home := Beacon{SSID: "ronin-net", BSSID: "f0:27:2d:aa:bb:cc", RSSI: -40}

	rep := Report{Node: "node-A",
		WiFi:    []WiFi{{MAC: "B8:27:EB:11:22:33", RSSI: -50}, {MAC: "de:ad:be:ef:00:01", RSSI: -62}},
		BLE:     []BLE{{Addr: "c1:11:11:11:11:11", RSSI: -70, Company: 0x004C, Type: 0x12}, {Addr: "c2:22:22:22:22:22", RSSI: -60, Name: "Watch"}},
		Beacons: []Beacon{home}}
	e.Ingest(rep, known, now)
	if n := e.TrustCurrent([]string{"ronin-net"}); n != 1 {
		t.Fatalf("trusted %d access points, want 1", n)
	}
	if len(events) != 0 {
		t.Fatalf("events on first sight: %+v", events)
	}

	// six minutes on: the stranger and the tag are still here; a look-alike access point appears
	later := now.Add(6 * time.Minute)
	for ts := now.Add(time.Minute); !ts.After(later); ts = ts.Add(time.Minute) {
		r := rep
		if ts.Equal(later) {
			r.Beacons = []Beacon{home, {SSID: "ronin-net", BSSID: "02:00:00:66:66:66", RSSI: -35}}
		}
		e.Ingest(r, known, ts)
	}
	got := map[string]string{}
	for _, ev := range events {
		got[ev.Kind] = ev.Source
	}
	if got["rogue-radio"] != "de:ad:be:ef:00:01" || got["tracker"] != "c1:11:11:11:11:11" || got["twin"] != "02:00:00:66:66:66" {
		t.Errorf("events = %+v", events)
	}
	if len(events) != 3 {
		t.Errorf("%d events, want exactly 3 (each reported once)", len(events))
	}

	radios, aps, _ := e.Snapshot(known, later)
	tags := map[string]string{}
	for _, r := range radios {
		tags[r.ID] = r.Tag
	}
	if tags["b8:27:eb:11:22:33"] != "network host" || tags["de:ad:be:ef:00:01"] != "lingering" || tags["c1:11:11:11:11:11"] != "tracker?" || tags["c2:22:22:22:22:22"] != "bluetooth" {
		t.Errorf("tags = %v", tags)
	}
	if len(aps) != 2 || !aps[1].Trusted || aps[0].Trusted {
		t.Errorf("access points = %+v", aps)
	}

	// the operator claims the stranger and the tag: no more flags, and nothing new is reported
	e.SetMine("DE:AD:BE:EF:00:01", true)
	e.SetMine("c1:11:11:11:11:11", true)
	radios, _, _ = e.Snapshot(known, later)
	for _, r := range radios {
		if (r.ID == "de:ad:be:ef:00:01" || r.ID == "c1:11:11:11:11:11") && r.Tag != "mine" {
			t.Errorf("%s tagged %q after being claimed", r.ID, r.Tag)
		}
	}

	// a burst of disconnect frames
	e.Ingest(Report{Node: "node-A", Deauth: 30}, known, later.Add(time.Second))
	if events[len(events)-1].Kind != "deauth" {
		t.Errorf("no deauth event: %+v", events[len(events)-1])
	}
	// radios that go quiet are forgotten
	if radios, _, _ := e.Snapshot(known, later.Add(time.Hour)); len(radios) != 4 {
		t.Logf("snapshot does not prune by itself (%d radios)", len(radios))
	}
	e.Ingest(Report{Node: "node-A"}, known, later.Add(time.Hour))
	if radios, _, _ := e.Snapshot(known, later.Add(time.Hour)); len(radios) != 0 {
		t.Errorf("%d radios remembered after an hour of silence", len(radios))
	}
}

func TestWeakAndClaimedRadiosAreNotReported(t *testing.T) {
	e := New()
	n := 0
	e.OnEvent = func(Event) { n++ }
	e.SetMine("aa:aa:aa:aa:aa:01", true)
	now := time.Unix(1_700_000_000, 0)
	for i := 0; i <= 8; i++ {
		e.Ingest(Report{Node: "n", WiFi: []WiFi{{MAC: "aa:aa:aa:aa:aa:01", RSSI: -50}, {MAC: "bb:bb:bb:bb:bb:02", RSSI: -86}},
			BLE: []BLE{{Addr: "cc:cc:cc:cc:cc:03", RSSI: -60, Name: "Headphones"}}}, nil, now.Add(time.Duration(i)*time.Minute))
	}
	if n != 0 {
		t.Errorf("%d events for a claimed radio, a neighbour's weak radio and a plain Bluetooth device", n)
	}
}
