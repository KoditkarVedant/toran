package main

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var wantHeaders = map[string]string{
	"X-Content-Type-Options": "nosniff",
	"X-Frame-Options":        "DENY",
	"Referrer-Policy":        "no-referrer",
	"Permissions-Policy":     "camera=(), microphone=(), geolocation=()",
}

func checkHeaders(t *testing.T, what string, h http.Header) {
	t.Helper()
	for k, v := range wantHeaders {
		if got := h.Get(k); got != v {
			t.Errorf("%s: %s = %q, want %q", what, k, got, v)
		}
	}
	if got := h.Get("Content-Security-Policy"); got != contentSecurityPolicy {
		t.Errorf("%s: Content-Security-Policy = %q", what, got)
	}
}

func TestSecurityHeadersOnEveryAnswer(t *testing.T) {
	_, h, dir := newTest(t)
	_ = os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html>hi</html>"), 0o644)

	cases := []struct {
		name, method, path string
		headers            map[string]string
		code               int
	}{
		{"the page", "GET", "/", nil, 200},
		{"the API", "GET", "/api/health", nil, 200},
		{"a missing file", "GET", "/nope", nil, 404},
		{"a refused path", "GET", "/.env", nil, 404},
		{"a save", "PUT", "/services.json", nil, 204},
		{"a save refused as cross-site", "PUT", "/services.json", map[string]string{"Sec-Fetch-Site": "cross-site"}, 403},
	}
	for _, c := range cases {
		r := doWith(h, c.method, c.path, lan, []byte(`{"groups":[]}`), c.headers)
		if r.Code != c.code {
			t.Errorf("%s: status %d, want %d", c.name, r.Code, c.code)
		}
		checkHeaders(t, c.name, r.Header())
	}
	r := doWith(h, "PUT", "/services.json", "8.8.8.8:1", []byte(`{"groups":[]}`), nil) // refused by LAN_ONLY
	if r.Code != 403 {
		t.Errorf("a save from outside the network: status %d, want 403", r.Code)
	}
	checkHeaders(t, "a save refused for the network", r.Header())
}

func TestUploadedFilesKeepTheirStricterPolicy(t *testing.T) {
	_, h, dir := newTest(t)
	_ = os.MkdirAll(filepath.Join(dir, "icons"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "icons", "x.svg"), []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`), 0o644)

	r := do(h, "GET", "/icons/x.svg", lan, nil)
	if r.Code != 200 {
		t.Fatalf("status %d", r.Code)
	}
	csp := r.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "sandbox") || !strings.Contains(csp, "default-src 'none'") || strings.Contains(csp, "script-src") {
		t.Errorf("an uploaded SVG must be sandboxed with no scripts, got %q", csp)
	}
	if r.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Error("nosniff missing on an uploaded file")
	}
}

// Guards against a loosening that would quietly remove the point of the policy.
func TestPolicyDirectives(t *testing.T) {
	for _, must := range []string{"default-src 'self'", "object-src 'none'", "base-uri 'self'", "form-action 'self'", "frame-ancestors 'none'"} {
		if !strings.Contains(contentSecurityPolicy, must) {
			t.Errorf("the policy lost %q", must)
		}
	}
	for _, mustNot := range []string{"unsafe-eval", " * ", "script-src *", "default-src *"} {
		if strings.Contains(contentSecurityPolicy+" ", mustNot) {
			t.Errorf("the policy must not contain %q", mustNot)
		}
	}
	// scripts may only come from this server (plus the inline ones the page still needs): no http:/https: sources
	for _, d := range strings.Split(contentSecurityPolicy, "; ") {
		if strings.HasPrefix(d, "script-src") && (strings.Contains(d, "http:") || strings.Contains(d, "https:")) {
			t.Errorf("scripts must not load from other sites: %q", d)
		}
	}
}

// HSTS belongs to whoever terminates HTTPS. Toran is usually plain HTTP on a home network,
// where the header is ignored at best and a trap at worst.
func TestNoHSTS(t *testing.T) {
	_, h, _ := newTest(t)
	if v := do(h, "GET", "/api/health", lan, nil).Header().Get("Strict-Transport-Security"); v != "" {
		t.Errorf("unexpected HSTS header %q", v)
	}
}
