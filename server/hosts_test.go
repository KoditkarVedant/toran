package main

import (
	"strings"
	"testing"
)

func TestHostOf(t *testing.T) {
	for in, want := range map[string]string{
		"toran.home.lan":      "toran.home.lan",
		"Toran.Home.LAN:8080": "toran.home.lan",
		"toran.home.lan.":     "toran.home.lan", // fully-qualified form
		"192.168.1.5:8080":    "192.168.1.5",
		"[::1]:8080":          "::1",
		"[::1]":               "::1",
		"::1":                 "::1",
		"[::ffff:10.0.0.1]":   "10.0.0.1",
		"[2001:DB8::1]:80":    "2001:db8::1",
	} {
		if got := hostOf(in); got != want {
			t.Errorf("hostOf(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAllowHostsRejectsMistakes(t *testing.T) {
	for _, bad := range []string{"http://toran.lan", "toran.lan:8080", "*.home.lan", "toran.lan/x", "user@toran.lan", "two words"} {
		s, _, _ := newTest(t)
		if err := s.allowHosts(bad); err == nil || !strings.Contains(err.Error(), "ALLOWED_HOSTS") {
			t.Errorf("%q should stop startup with a clear message, got %v", bad, err)
		}
	}
	s, _, _ := newTest(t)
	if err := s.allowHosts(" toran.home.lan , Pi , 2001:db8::1 ,"); err != nil {
		t.Errorf("valid list refused: %v", err)
	}
}

func TestHostAllowed(t *testing.T) {
	s, _, _ := newTest(t)
	if !s.hostAllowed("anything.example") {
		t.Fatal("with no list configured every host must be served (the check is off)")
	}
	if err := s.allowHosts("toran.home.lan,pi"); err != nil {
		t.Fatal(err)
	}
	for _, h := range []string{
		"toran.home.lan", "TORAN.home.lan:8080", "pi", "pi:9090", // listed
		"localhost", "localhost:8080", "127.0.0.1:8080", "[::1]:8080", "192.168.1.5", "10.0.0.2:80", // numbers and localhost
	} {
		if !s.hostAllowed(h) {
			t.Errorf("%q should be allowed", h)
		}
	}
	for _, h := range []string{
		"evil.example", "evil.example:8080", "toran.home.lan.evil.example", "evil.toran.home.lan", // no suffix tricks, no subdomains
		"", "pi.evil.example", "localhost.evil.example",
	} {
		if s.hostAllowed(h) {
			t.Errorf("%q should be refused", h)
		}
	}
}

// The attack in full: the page's own name points at the Pi, so Origin matches Host and the
// request comes from the LAN. Only the host check stops it.
func TestRebindingIsStopped(t *testing.T) {
	s, h, _ := newTest(t)
	if err := s.allowHosts("toran.home.lan"); err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"groups":[]}`)
	rebound := map[string]string{"Host": "evil.example:8080", "Origin": "http://evil.example:8080"}

	if r := doWith(h, "PUT", "/services.json", lan, body, rebound); r.Code != 403 {
		t.Errorf("a rebound save: status %d, want 403", r.Code)
	}
	if r := doWith(h, "GET", "/services.json", lan, nil, rebound); r.Code != 403 {
		t.Errorf("a rebound read: status %d, want 403", r.Code)
	}
	checkHeaders(t, "a refused host", doWith(h, "GET", "/", lan, nil, rebound).Header())

	// the same request under the real name works, as does reaching the Pi by number
	ok := map[string]string{"Host": "toran.home.lan:8080", "Origin": "http://toran.home.lan:8080"}
	if r := doWith(h, "PUT", "/services.json", lan, body, ok); r.Code != 204 {
		t.Errorf("a save under the listed name: status %d, want 204", r.Code)
	}
	byIP := map[string]string{"Host": "192.168.1.5:8080", "Origin": "http://192.168.1.5:8080"}
	if r := doWith(h, "PUT", "/services.json", lan, body, byIP); r.Code != 204 {
		t.Errorf("a save by IP address: status %d, want 204", r.Code)
	}
	if r := doWith(h, "GET", "/api/health", "127.0.0.1:5000", nil, map[string]string{"Host": "127.0.0.1:8080"}); r.Code != 200 {
		t.Errorf("the container health check: status %d, want 200", r.Code)
	}
}

func TestHostCheckIsOffByDefault(t *testing.T) {
	_, h, _ := newTest(t)
	if r := doWith(h, "GET", "/api/health", lan, nil, map[string]string{"Host": "whatever.example"}); r.Code != 200 {
		t.Errorf("status %d, want 200", r.Code)
	}
}
