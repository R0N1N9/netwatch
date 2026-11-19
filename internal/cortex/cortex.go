// Package cortex investigates an alert the way an analyst would: it gathers
// what is known, weighs each piece for and against the alert being a real
// threat, argues both sides, and proposes one action for a person to approve.
// Nothing here acts on its own.
package cortex

import (
	"fmt"
	"strings"
)

// Host is what is known about the device behind an alert.
type Host struct {
	IP          string
	Name        string
	Vendor      string
	Role        string // self | gateway | trusted | unverified
	OS          string
	Anomaly     float64
	Beacon      string
	Quarantined bool
}

// Evidence is everything gathered for one alert.
type Evidence struct {
	Kind, Severity, Source, Message string
	Host                            *Host    // nil if the source is not a network host
	Related                         []string // other alerts from the same source
	Programs                        []string // local programs talking to that address
	DecoyTrips                      int      // decoys this source has touched
	WardDeviations                  int      // steps outside its learned behaviour
	Room                            string   // what SONAR sensed around the moment of the alert, if anything
}

// Step is one line of reasoning. Weight is positive toward "real threat".
type Step struct {
	Title   string `json:"title"`
	Finding string `json:"finding"`
	Weight  int    `json:"weight"`
}

// Proposal is the single action put to the operator.
type Proposal struct {
	Action string `json:"action"` // quarantine | trust | verify | watch
	Target string `json:"target"`
	Why    string `json:"why"`
}

// Case is the finished investigation.
type Case struct {
	Steps      []Step   `json:"steps"`
	For        []string `json:"for"`
	Against    []string `json:"against"`
	Score      int      `json:"score"`
	Verdict    string   `json:"verdict"` // likely threat | needs a look | likely benign
	Confidence int      `json:"confidence"`
	Proposal   Proposal `json:"proposal"`
}

var base = map[string]struct {
	weight       int
	what, benign string
}{
	"arp-spoof":  {45, "The gateway's hardware address changed, which is how traffic gets intercepted.", "A router reboot, failover or a mesh node taking over also changes it."},
	"beacon":     {35, "Regular callbacks to one outside address are how remote-control software checks in.", "Telemetry, update checks and smart-home hubs also call home on a timer."},
	"dns":        {35, "The name looks machine-generated or carries encoded data.", "Content networks and some apps use long random-looking names."},
	"anomaly":    {20, "The device is behaving unlike its own history.", "A software update, a backup or a new app can change behaviour for a while."},
	"new-device": {10, "A device nobody has vouched for joined the network.", "Guests' phones and new gadgets join networks all the time."},
	"decoy":      {45, "Something touched a decoy that has no legitimate use.", "A backup or indexing tool can open a canary file; a scanner you ran can hit a honeyport."},
	"ward":       {25, "The device did something outside its learned profile.", "The profile may simply be incomplete."},
	"usb":        {30, "A USB device appeared, possibly one that types.", "You may have plugged it in yourself."},
	"tripwire":   {20, "Movement crossed a line you drew while the system was armed.", "Someone you live or work with, or a pet."},
	"presence":   {15, "Movement was sensed while the system was armed.", "Someone you live or work with, or a pet."},
	"spectrum":   {25, "An unexpected radio or radio event was seen.", "A neighbour's or visitor's device is in range."},
	"pulse":      {35, "The typing rhythm at the keyboard does not match the owner.", "Typing one-handed, tired, or on a different keyboard changes rhythm."},
}

// Investigate builds a case from the evidence.
func Investigate(ev Evidence) Case {
	var c Case
	add := func(title, finding string, w int, side string) {
		c.Steps = append(c.Steps, Step{title, finding, w})
		c.Score += w
		if side == "for" {
			c.For = append(c.For, finding)
		} else if side == "against" {
			c.Against = append(c.Against, finding)
		}
	}

	if ev.Kind == "decoy-drill" || ev.Kind == "drill" {
		// The operator's own test: say so and stop.
		c.Steps = []Step{{"What the alert means", "You ran this drill yourself; the alert is the alarm answering.", -20}}
		c.For, c.Against = []string{}, []string{"It was a test touch from this machine during a drill you started."}
		c.Score, c.Verdict, c.Confidence = -20, "likely benign", 95
		c.Proposal = Proposal{"watch", ev.Source, "Nothing to do. The decoy and its alarm work."}
		return c
	}
	b, known := base[ev.Kind]
	if !known {
		b.weight, b.what, b.benign = 10, "An alert was raised.", "It may be routine."
	}
	if strings.Contains(ev.Message, "keystroke injection") {
		b.weight = 60
	}
	add("What the alert means", b.what, b.weight, "for")
	c.Against = append(c.Against, b.benign)
	if ev.Severity == "CRITICAL" {
		add("How it was rated", "The detector that raised it rates this kind of event critical.", 15, "")
	}

	if h := ev.Host; h != nil {
		who := h.IP
		if h.Name != "" && h.Name != h.IP {
			who = h.Name + " (" + h.IP + ")"
		}
		switch h.Role {
		case "trusted", "self":
			add("Who it is", who+" is a device you have marked trusted.", -30, "against")
		case "gateway":
			add("Who it is", who+" is your gateway, which you cannot do without.", -5, "")
		default:
			add("Who it is", who+" has never been vouched for.", 10, "for")
		}
		if strings.Contains(h.Vendor, "randomized") {
			add("Its identity", "It uses a randomised hardware address, so it cannot be tied to a maker.", 10, "for")
		} else if h.Vendor != "" && h.Name != "" {
			add("Its identity", "It has a name and a recognisable maker ("+h.Vendor+").", -8, "against")
		}
		if h.Anomaly >= 70 {
			add("Its behaviour", fmt.Sprintf("Its behaviour score is %.0f of 100 — far from its own baseline.", h.Anomaly), 20, "for")
		} else if h.Anomaly > 0 && h.Anomaly < 25 {
			add("Its behaviour", "Its traffic is otherwise close to its own baseline.", -8, "against")
		}
		if h.Beacon != "" && ev.Kind != "beacon" {
			add("Its connections", "It is also calling "+h.Beacon+" on a regular timer.", 25, "for")
		}
		if h.Quarantined {
			add("Current state", "It is already quarantined.", -10, "")
		}
	} else {
		add("Who it is", "The source ("+ev.Source+") is not a host on the network.", 0, "")
	}

	if n := len(ev.Related); n > 0 {
		w := n * 8
		if w > 24 {
			w = 24
		}
		add("Its history", fmt.Sprintf("%d other alert(s) came from the same source: %s.", n, strings.Join(first(ev.Related, 3), "; ")), w, "for")
	} else {
		add("Its history", "No other alerts have come from this source.", -5, "against")
	}
	if ev.DecoyTrips > 0 {
		add("Decoys", fmt.Sprintf("It has touched %d decoy(s). Nothing legitimate does that.", ev.DecoyTrips), 30, "for")
	}
	if ev.WardDeviations > 0 {
		add("Learned profile", fmt.Sprintf("It has stepped outside its learned behaviour %d time(s).", ev.WardDeviations), 12, "for")
	}
	if ev.Room != "" {
		add("In the room", ev.Room, 0, "")
	}
	if len(ev.Programs) > 0 {
		add("On this machine", "Local programs talking to it: "+strings.Join(first(ev.Programs, 3), ", ")+".", 0, "")
	}

	switch {
	case c.Score >= 60:
		c.Verdict = "likely threat"
	case c.Score >= 30:
		c.Verdict = "needs a look"
	default:
		c.Verdict = "likely benign"
	}
	// Confidence grows with distance from the undecided middle and with how much evidence there was.
	c.Confidence = 40 + abs(c.Score-45)/2 + 3*len(c.Steps)
	if c.Confidence > 95 {
		c.Confidence = 95
	}

	c.Proposal = propose(ev, c)
	return c
}

func propose(ev Evidence, c Case) Proposal {
	h := ev.Host
	switch {
	case h == nil:
		return Proposal{"watch", ev.Source, "There is no network host to act on; keep watching."}
	case h.Role == "gateway":
		return Proposal{"verify", h.IP, "Never quarantine the gateway. Check the router itself: was it rebooted or replaced, and does its address label match?"}
	case h.Role == "self":
		return Proposal{"watch", h.IP, "This is the machine NetWatch runs on."}
	case h.Quarantined:
		return Proposal{"watch", h.IP, "It is already quarantined."}
	case c.Verdict == "likely threat":
		return Proposal{"quarantine", h.IP, "The evidence points one way. Quarantine it at this machine's firewall, then find the device."}
	case c.Verdict == "likely benign" && h.Role == "unverified" && c.Score < 15:
		return Proposal{"trust", h.IP, "Nothing else is wrong with it. If you recognise it, mark it trusted so it stops raising alerts."}
	default:
		return Proposal{"watch", h.IP, "Not enough to act on. Leave it and see whether more alerts follow."}
	}
}

func first(s []string, n int) []string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
