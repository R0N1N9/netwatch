package oracle

import "testing"

var edges = []Edge{
	{Src: "192.168.1.50", Dst: "140.82.113.25", Port: 443, Bytes: 184320, External: true, Name: "github.com"},
	{Src: "192.168.1.50", Dst: "192.168.1.20", Port: 53, Bytes: 4000},
	{Src: "192.168.1.31", Dst: "52.94.236.248", Port: 443, Bytes: 33000, External: true, Name: "device-metrics-us.amazon.com"},
	{Src: "192.168.1.99", Dst: "185.220.101.44", Port: 443, Bytes: 9800, External: true},
	{Src: "192.168.1.99", Dst: "45.133.1.90", Port: 8443, Bytes: 4200, External: true},
	{Src: "192.168.1.99", Dst: "192.168.1.50", Port: 22, Bytes: 900},
	{Src: "192.168.1.99", Dst: "192.168.1.20", Port: 53, Bytes: 300},
}

func TestReach(t *testing.T) {
	r := ReachOf("192.168.1.99", "192.168.1.50", edges)
	if len(r.External) != 2 || len(r.Internal) != 2 || len(r.Inbound) != 0 || r.ToThis != 1 || r.Bytes != 15200 {
		t.Errorf("reach = %+v", r)
	}
	if in := ReachOf("192.168.1.20", "192.168.1.50", edges); len(in.Inbound) != 2 {
		t.Errorf("inbound to the resolver = %d, want 2", len(in.Inbound))
	}
}

func TestWhatIf(t *testing.T) {
	cases := []struct {
		rule    string
		matches int
		hosts   int
	}{
		{"port 8443", 1, 1},
		{"unnamed", 2, 1},
		{"dst github.com", 1, 1},
		{"src 192.168.1.99", 4, 1},
		{"bytes > 30,000", 2, 2},
		{"fanout > 3", 4, 1},
	}
	for _, c := range cases {
		w, err := Evaluate(c.rule, edges)
		if err != nil || len(w.Matches) != c.matches || len(w.Hosts) != c.hosts {
			t.Errorf("%q: matches=%d hosts=%v err=%v, want %d/%d", c.rule, len(w.Matches), w.Hosts, err, c.matches, c.hosts)
		}
	}
	for _, bad := range []string{"", "port abc", "drop everything", "bytes 5"} {
		if _, err := Evaluate(bad, edges); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
}
