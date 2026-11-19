package passive

import (
	"math"
	"strings"
)

// DNSVerdict is the result of scoring a DNS query name.
type DNSVerdict struct {
	Name    string
	Entropy float64
	DGA     float64 // 0-1 likelihood of algorithmically-generated domain
	Tunnel  bool    // looks like DNS tunneling / data exfil
	Reason  string
}

// ScoreDNS heuristically scores a DNS query name for DGA and tunneling.
func ScoreDNS(name string, qtype string) DNSVerdict {
	name = strings.TrimSuffix(strings.ToLower(name), ".")
	v := DNSVerdict{Name: name}
	if name == "" {
		return v
	}
	labels := strings.Split(name, ".")
	reg := registrable(labels) // the label we score (2nd-level)
	v.Entropy = shannon(reg)

	// --- DGA scoring ---
	var score float64
	if v.Entropy > 3.7 {
		score += 0.45
	} else if v.Entropy > 3.2 {
		score += 0.2
	}
	digits, cons, vowels := charClasses(reg)
	n := float64(len([]rune(reg)))
	if n > 0 {
		if digits/n > 0.25 {
			score += 0.2
		}
		if vowels/n < 0.2 { // consonant soup
			score += 0.2
		}
		_ = cons
	}
	if len(reg) >= 16 {
		score += 0.15
	}
	v.DGA = math.Min(1, score)

	// --- tunneling heuristics ---
	longLabels := 0
	for _, l := range labels {
		if len(l) > 40 {
			longLabels++
		}
	}
	if longLabels > 0 || len(labels) > 6 || qtype == "TXT" || qtype == "NULL" {
		if len(name) > 52 || longLabels > 0 {
			v.Tunnel = true
			v.Reason = "long/high-count labels or TXT/NULL — possible DNS tunnel"
		}
	}
	if v.DGA >= 0.6 && v.Reason == "" {
		v.Reason = "high-entropy consonant-heavy name — possible DGA"
	}
	return v
}

// registrable returns the label just below the TLD (best-effort, no PSL).
func registrable(labels []string) string {
	if len(labels) >= 2 {
		return labels[len(labels)-2]
	}
	if len(labels) == 1 {
		return labels[0]
	}
	return ""
}

func shannon(s string) float64 {
	if s == "" {
		return 0
	}
	freq := map[rune]float64{}
	for _, r := range s {
		freq[r]++
	}
	n := float64(len([]rune(s)))
	var h float64
	for _, c := range freq {
		p := c / n
		h -= p * math.Log2(p)
	}
	return h
}

func charClasses(s string) (digits, cons, vowels float64) {
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
			digits++
		case strings.ContainsRune("aeiou", r):
			vowels++
		case r >= 'a' && r <= 'z':
			cons++
		}
	}
	return
}
