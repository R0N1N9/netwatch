// Package oracle models who talks to whom, so you can see what a host reaches
// and ask "what if" before changing anything.
package oracle

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Edge is one observed conversation direction.
type Edge struct {
	Src      string `json:"src"`
	Dst      string `json:"dst"`
	Port     uint16 `json:"port"`
	Bytes    uint64 `json:"bytes"`
	External bool   `json:"external"` // the destination is outside the local network
	Name     string `json:"name"`     // server name, if one was seen
}

// Reach summarises what one host talks to.
type Reach struct {
	Host      string `json:"host"`
	Internal  []Edge `json:"internal"`
	External  []Edge `json:"external"`
	Inbound   []Edge `json:"inbound"`
	Bytes     uint64 `json:"bytes"`
	ToThis    int    `json:"toThis"` // conversations with the machine running NetWatch
	Isolating string `json:"isolating"`
}

// ReachOf lists a host's conversations and what cutting it off would stop.
func ReachOf(host, self string, edges []Edge) Reach {
	r := Reach{Host: host, Internal: []Edge{}, External: []Edge{}, Inbound: []Edge{}}
	for _, e := range edges {
		switch {
		case e.Src == host && e.External:
			r.External = append(r.External, e)
		case e.Src == host:
			r.Internal = append(r.Internal, e)
		case e.Dst == host:
			r.Inbound = append(r.Inbound, e)
		default:
			continue
		}
		r.Bytes += e.Bytes
		if (e.Src == host && e.Dst == self) || (e.Dst == host && e.Src == self) {
			r.ToThis++
		}
	}
	total := len(r.Internal) + len(r.External) + len(r.Inbound)
	r.Isolating = fmt.Sprintf("BLACKWALL on this machine would stop %d of its %d conversations (only those with this machine). Isolating it at the router would stop all %d.", r.ToThis, total, total)
	return r
}

// Rule is a simple what-if condition over observed conversations.
type Rule struct {
	Field string // port | dst | src | bytes | fanout | unnamed
	Value string
	N     uint64
}

// ParseRule understands: "port 8443", "dst 1.2.3.4", "src 192.168.1.99",
// "bytes > 100000", "fanout > 5", "unnamed".
func ParseRule(s string) (Rule, error) {
	f := strings.Fields(strings.ToLower(strings.TrimSpace(s)))
	if len(f) == 0 {
		return Rule{}, fmt.Errorf("empty rule")
	}
	num := func(v string) (uint64, error) { return strconv.ParseUint(strings.ReplaceAll(v, ",", ""), 10, 64) }
	switch {
	case f[0] == "unnamed" && len(f) == 1:
		return Rule{Field: "unnamed"}, nil
	case (f[0] == "port") && len(f) == 2:
		n, err := num(f[1])
		if err != nil || n > 65535 {
			return Rule{}, fmt.Errorf("port must be a number up to 65535")
		}
		return Rule{Field: "port", N: n}, nil
	case (f[0] == "dst" || f[0] == "src") && len(f) == 2:
		return Rule{Field: f[0], Value: f[1]}, nil
	case (f[0] == "bytes" || f[0] == "fanout") && len(f) == 3 && f[1] == ">":
		n, err := num(f[2])
		if err != nil {
			return Rule{}, fmt.Errorf("%s needs a number", f[0])
		}
		return Rule{Field: f[0], N: n}, nil
	}
	return Rule{}, fmt.Errorf(`try: "port 8443", "dst 1.2.3.4", "src 192.168.1.99", "bytes > 100000", "fanout > 5" or "unnamed"`)
}

// WhatIf reports which observed conversations a rule would have matched.
type WhatIf struct {
	Rule    string   `json:"rule"`
	Matches []Edge   `json:"matches"`
	Hosts   []string `json:"hosts"`
	Bytes   uint64   `json:"bytes"`
	Of      int      `json:"of"`
}

// Evaluate runs a rule over the observed conversations.
func Evaluate(text string, edges []Edge) (WhatIf, error) {
	r, err := ParseRule(text)
	if err != nil {
		return WhatIf{}, err
	}
	out := WhatIf{Rule: text, Matches: []Edge{}, Hosts: []string{}, Of: len(edges)}
	fan := map[string]map[string]bool{}
	for _, e := range edges {
		if fan[e.Src] == nil {
			fan[e.Src] = map[string]bool{}
		}
		fan[e.Src][e.Dst] = true
	}
	hosts := map[string]bool{}
	for _, e := range edges {
		var hit bool
		switch r.Field {
		case "port":
			hit = uint64(e.Port) == r.N
		case "dst":
			hit = e.Dst == r.Value || strings.EqualFold(e.Name, r.Value)
		case "src":
			hit = e.Src == r.Value
		case "bytes":
			hit = e.Bytes > r.N
		case "fanout":
			hit = uint64(len(fan[e.Src])) > r.N
		case "unnamed":
			hit = e.External && e.Name == ""
		}
		if hit {
			out.Matches = append(out.Matches, e)
			out.Bytes += e.Bytes
			hosts[e.Src] = true
		}
	}
	for h := range hosts {
		out.Hosts = append(out.Hosts, h)
	}
	sort.Strings(out.Hosts)
	return out, nil
}
