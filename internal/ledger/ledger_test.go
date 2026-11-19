package ledger

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestChainVerifyTamperAndReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.jsonl")
	l := Open(path)
	t0 := time.Unix(1_700_000_000, 0)
	l.Append("alert", "WARN", "192.168.1.99", "new host joined the segment", t0)
	l.Append("alert", "CRITICAL", "192.168.1.1", "gateway MAC changed", t0.Add(time.Minute))
	l.Append("action", "", "192.168.1.99", "operator quarantined 192.168.1.99", t0.Add(2*time.Minute))
	if bad := l.Verify(); bad != 0 || l.Len() != 3 {
		t.Fatalf("fresh chain: bad=%d len=%d", bad, l.Len())
	}

	// a fresh process sees the same intact chain and can extend it
	l2 := Open(path)
	if l2.Verify() != 0 || l2.Head() != l.Head() {
		t.Fatal("reloaded chain does not match")
	}
	if e := l2.Append("note", "", "", "after restart", t0.Add(3*time.Minute)); e.Seq != 4 || l2.Verify() != 0 {
		t.Fatalf("append after reload: seq=%d", e.Seq)
	}

	// editing history on disk is detected at the edited entry
	b, _ := os.ReadFile(path)
	os.WriteFile(path, []byte(strings.Replace(string(b), "gateway MAC changed", "nothing happened", 1)), 0o600)
	if bad := Open(path).Verify(); bad != 2 {
		t.Errorf("tampered entry reported as %d, want 2", bad)
	}

	rep := Report(l.Span(0, 0), true)
	for _, want := range []string{"1 critical, 1 warning", "1 operator actions and", "ACTION   operator quarantined", "chain verified"} {
		if !strings.Contains(rep, want) {
			t.Errorf("report missing %q:\n%s", want, rep)
		}
	}
	if got := l.Span(t0.Add(30*time.Second).UnixMilli(), t0.Add(90*time.Second).UnixMilli()); len(got) != 1 || got[0].Seq != 2 {
		t.Errorf("span = %+v", got)
	}
}
