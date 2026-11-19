package sysmon

import "testing"

func TestParseCPU(t *testing.T) {
	idle, total, ok := parseCPU("cpu  100 0 50 800 50 0 0 0 0 0")
	if !ok || idle != 850 || total != 1000 {
		t.Fatalf("got idle=%d total=%d ok=%v, want 850/1000/true", idle, total, ok)
	}
	if _, _, ok := parseCPU("cpu0 1 2 3 4"); ok {
		t.Error("per-core line accepted as aggregate")
	}
}

func TestParseMem(t *testing.T) {
	total, used := parseMem("MemTotal:       16000000 kB\nMemFree:         1000000 kB\nMemAvailable:    6000000 kB\n")
	if total != 16000000*1024 || used != 10000000*1024 {
		t.Fatalf("got total=%d used=%d", total, used)
	}
}
