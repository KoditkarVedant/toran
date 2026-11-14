package main

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Every route that changes something. okCode is what the route answers when the request is let through.
// (restore answers 404 because no backup exists in these tests; what matters is that it is not 403.)
var writeRoutes = []struct {
	name, method, path string
	body               []byte
	okCode             int
}{
	{"save config", "PUT", "/services.json", []byte(`{"groups":[]}`), 204},
	{"upload wallpaper", "PUT", "/wallpaper/a.jpg", png1x1, 204},
	{"delete wallpaper", "DELETE", "/wallpaper/a.jpg", nil, 204},
	{"upload icon", "PUT", "/icons/a.png", png1x1, 204},
	{"restore backup", "POST", "/api/backups/restore", []byte(`{"name":"services-20240101-000000-000.json"}`), 404},
}

// httptest requests default to Host "example.com".
const testHost = "http://example.com"

func TestCrossOriginWrites(t *testing.T) {
	cases := []struct {
		name    string
		headers map[string]string
		blocked bool
	}{
		{"another site, Sec-Fetch-Site", map[string]string{"Sec-Fetch-Site": "cross-site"}, true},
		{"same site, other origin", map[string]string{"Sec-Fetch-Site": "same-site"}, true},
		{"same origin", map[string]string{"Sec-Fetch-Site": "same-origin"}, false},
		{"typed in the address bar", map[string]string{"Sec-Fetch-Site": "none"}, false},
		{"plain HTTP, Origin matches Host", map[string]string{"Origin": testHost}, false},
		{"plain HTTP, Origin of another site", map[string]string{"Origin": "http://evil.example"}, true},
		{"plain HTTP, Origin of another port", map[string]string{"Origin": testHost + ":9999"}, true},
		{"Origin null (sandboxed frame)", map[string]string{"Origin": "null"}, true},
		{"no browser headers (curl, scripts)", nil, false},
	}
	for _, rt := range writeRoutes {
		for _, c := range cases {
			t.Run(rt.name+"/"+c.name, func(t *testing.T) {
				_, h, _ := newTest(t)
				r := doWith(h, rt.method, rt.path, lan, rt.body, c.headers)
				switch {
				case c.blocked && r.Code != http.StatusForbidden:
					t.Errorf("want 403, got %d", r.Code)
				case !c.blocked && r.Code != rt.okCode:
					t.Errorf("want %d, got %d (%s)", rt.okCode, r.Code, strings.TrimSpace(r.Body.String()))
				}
			})
		}
	}
}

func TestBlockedWriteChangesNothing(t *testing.T) {
	s, h, _ := newTest(t)
	r := doWith(h, "PUT", "/services.json", lan, []byte(`{"groups":[]}`), map[string]string{"Sec-Fetch-Site": "cross-site"})
	if r.Code != 403 {
		t.Fatalf("want 403, got %d", r.Code)
	}
	if _, err := os.ReadFile(filepath.Join(testUserDir(s), "services.json")); err == nil {
		t.Error("a blocked request must not write the config")
	}
	if !strings.Contains(r.Body.String(), "TRUSTED_ORIGINS") {
		t.Errorf("the refusal should tell proxy users what to set, got %q", r.Body.String())
	}
}

func TestSafeMethodsAreNotChecked(t *testing.T) {
	_, h, _ := newTest(t)
	for _, m := range []string{"GET", "HEAD"} {
		if r := doWith(h, m, "/api/health", lan, nil, map[string]string{"Sec-Fetch-Site": "cross-site"}); r.Code != 200 {
			t.Errorf("%s with Sec-Fetch-Site: cross-site: want 200, got %d", m, r.Code)
		}
	}
}

func TestTrustedOrigins(t *testing.T) {
	// behind a proxy the Host is the internal one, and the browser's Origin is the public one
	proxied := map[string]string{"Host": "toran:8080", "Origin": "https://toran.example.org"}

	s, h, _ := newTest(t)
	if r := doWith(h, "PUT", "/services.json", lan, []byte(`{"groups":[]}`), proxied); r.Code != 403 {
		t.Fatalf("before trusting the origin: want 403, got %d", r.Code)
	}
	if err := s.trustOrigins(" https://toran.example.org , http://192.168.1.5:9090 "); err != nil {
		t.Fatal(err)
	}
	if r := doWith(h, "PUT", "/services.json", lan, []byte(`{"groups":[]}`), proxied); r.Code != 204 {
		t.Errorf("trusted origin: want 204, got %d", r.Code)
	}
	if r := doWith(h, "PUT", "/services.json", lan, []byte(`{"groups":[]}`), map[string]string{"Host": "toran:8080", "Origin": "http://192.168.1.5:9090"}); r.Code != 204 {
		t.Errorf("second trusted origin: want 204, got %d", r.Code)
	}
	if r := doWith(h, "PUT", "/services.json", lan, []byte(`{"groups":[]}`), map[string]string{"Host": "toran:8080", "Origin": "https://evil.example"}); r.Code != 403 {
		t.Errorf("an origin that was not listed: want 403, got %d", r.Code)
	}
	// trusting an origin does not switch off the LAN rule
	if r := doWith(h, "PUT", "/services.json", "8.8.8.8:1234", []byte(`{"groups":[]}`), proxied); r.Code != 403 {
		t.Errorf("trusted origin from a public address: want 403 from LAN_ONLY, got %d", r.Code)
	}
}

func TestTrustedOriginsRejectsBadValues(t *testing.T) {
	for _, bad := range []string{"example.com", "https://example.com/app", "https://", "//example.com"} {
		s, _, _ := newTest(t)
		if err := s.trustOrigins(bad); err == nil {
			t.Errorf("TRUSTED_ORIGINS=%q should be rejected", bad)
		} else if !strings.Contains(err.Error(), "TRUSTED_ORIGINS") {
			t.Errorf("the error should name the setting, got %v", err)
		}
	}
	s, _, _ := newTest(t)
	if err := s.trustOrigins(""); err != nil {
		t.Errorf("an empty list is fine, got %v", err)
	}
}

// The server must never answer with CORS headers: that would let other sites read or write.
func TestNoCORSHeaders(t *testing.T) {
	_, h, _ := newTest(t)
	reqs := []struct {
		method, path string
		headers      map[string]string
	}{
		{"GET", "/api/health", map[string]string{"Origin": "http://evil.example"}},
		{"PUT", "/services.json", map[string]string{"Origin": "http://evil.example"}}, // blocked
		{"PUT", "/services.json", map[string]string{"Origin": testHost}},              // allowed
		{"OPTIONS", "/services.json", map[string]string{"Origin": "http://evil.example", "Access-Control-Request-Method": "PUT"}},
	}
	for _, q := range reqs {
		r := doWith(h, q.method, q.path, lan, []byte(`{"groups":[]}`), q.headers)
		for k := range r.Header() {
			if strings.HasPrefix(strings.ToLower(k), "access-control-") {
				t.Errorf("%s %s (%v) answered with %s", q.method, q.path, q.headers, k)
			}
		}
	}
}
