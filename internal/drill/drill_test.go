package drill

import "testing"

func TestEveryDetectorResponds(t *testing.T) {
	rs := Run()
	if len(rs) < 10 {
		t.Fatalf("only %d drills ran", len(rs))
	}
	for _, r := range rs {
		if !r.OK {
			t.Errorf("%s did not respond: %s", r.Name, r.Detail)
		}
	}
	if lines := Lines(rs); len(lines) != len(rs)+1 {
		t.Errorf("rendered %d lines for %d results", len(lines), len(rs))
	}
}
