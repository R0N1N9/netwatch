package cli

import (
	"errors"
	"strings"
	"testing"
)

func TestFindAppBrowserPrefersChromeAndHonoursTheOverride(t *testing.T) {
	have := func(names ...string) func(string) (string, error) {
		return func(n string) (string, error) {
			for _, h := range names {
				if h == n {
					return "/usr/bin/" + n, nil
				}
			}
			return "", errors.New("not found")
		}
	}
	if p, name := findAppBrowser("", have("chromium", "google-chrome", "firefox")); p != "/usr/bin/google-chrome" || name != "Google Chrome" {
		t.Errorf("Chrome should win over Chromium: %q %q", p, name)
	}
	if p, name := findAppBrowser("", have("brave-browser")); p != "/usr/bin/brave-browser" || name != "Brave" {
		t.Errorf("Brave should be found: %q %q", p, name)
	}
	if p, _ := findAppBrowser("", have("firefox")); p != "" {
		t.Errorf("Firefox has no application windows, got %q", p)
	}
	if p, name := findAppBrowser("chromium", have("chromium", "google-chrome")); p != "/usr/bin/chromium" || name != "chromium" {
		t.Errorf("NETWATCH_BROWSER should be used as given: %q %q", p, name)
	}
	if p, _ := findAppBrowser("no-such-browser", have("google-chrome")); p != "" {
		t.Errorf("an override that is not installed should not fall back silently, got %q", p)
	}
}

func TestWindowArgs(t *testing.T) {
	a := strings.Join(windowArgs("http://127.0.0.1:7979", "/data/window"), " ")
	for _, want := range []string{"--app=http://127.0.0.1:7979", "--user-data-dir=/data/window", "--class=NetWatch", "--no-first-run"} {
		if !strings.Contains(a, want) {
			t.Errorf("missing %s in %q", want, a)
		}
	}
	if a := strings.Join(windowArgs("http://x", ""), " "); strings.Contains(a, "user-data-dir") {
		t.Errorf("no profile directory was asked for: %q", a)
	}
}
