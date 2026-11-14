package main

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

var t0 = time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

func testSessions(key string) (*sessions, *time.Time) {
	now := t0
	s := newSessions([]byte(strings.Repeat(key, keyLen)), 30*24*time.Hour)
	s.now = func() time.Time { return now }
	return s, &now
}

var alice = user{ID: "aaaaaaaaaaaaaaaa", Name: "alice", TokenVersion: 3}

func TestSessionRoundTrip(t *testing.T) {
	s, _ := testSessions("k")
	c, ok := s.parse(s.issue(alice))
	if !ok || c.UID != alice.ID || c.TokenVer != 3 {
		t.Errorf("claims %+v ok=%v", c, ok)
	}
}

func TestSessionRejectsTampering(t *testing.T) {
	s, _ := testSessions("k")
	good := s.issue(alice)
	p, m, _ := strings.Cut(good, ".")

	flip := func(x string) string { // change one character, keeping it valid base64url
		c := byte('A')
		if x[5] == 'A' {
			c = 'B'
		}
		return x[:5] + string(c) + x[6:]
	}
	other, _ := testSessions("z")
	forgedPayload := tokenEnc.EncodeToString([]byte(`{"u":"bbbbbbbbbbbbbbbb","v":3,"i":0,"e":9999999999}`))

	for name, tok := range map[string]string{
		"payload changed":        flip(p) + "." + m,
		"signature changed":      p + "." + flip(m),
		"another key":            other.issue(alice),
		"payload swapped":        forgedPayload + "." + m,
		"no signature":           p + ".",
		"only a signature":       "." + m,
		"no dot":                 p + m,
		"extra part":             good + ".x",
		"empty":                  "",
		"junk":                   "not a cookie",
		"not base64":             "!!!.!!!",
		"signed garbage payload": signedGarbage(s),
		"unsigned json":          tokenEnc.EncodeToString([]byte(`{"u":"a","v":0,"i":0,"e":9999999999}`)) + ".",
	} {
		if _, ok := s.parse(tok); ok {
			t.Errorf("%s: must be rejected", name)
		}
	}
}

// A correctly signed value that is not a session is still refused.
func signedGarbage(s *sessions) string {
	payload := []byte("this is not json")
	return tokenEnc.EncodeToString(payload) + "." + tokenEnc.EncodeToString(s.sign(payload))
}

func TestSessionExpiresAndRefreshes(t *testing.T) {
	s, now := testSessions("k")
	tok := s.issue(alice)

	*now = t0.Add(29 * 24 * time.Hour)
	c, ok := s.parse(tok)
	if !ok {
		t.Fatal("a session must still work inside its lifetime")
	}
	if !s.stale(c) {
		t.Error("an old session should be refreshed")
	}
	*now = t0.Add(30*24*time.Hour + time.Second)
	if _, ok := s.parse(tok); ok {
		t.Error("an expired session must be rejected")
	}

	*now = t0.Add(time.Hour)
	fresh, _ := s.parse(s.issue(alice))
	if s.stale(fresh) {
		t.Error("a new session is not stale")
	}
}

func TestSessionKeyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secret.key")
	k1, err := loadSessionKey(path)
	if err != nil || len(k1) != keyLen {
		t.Fatalf("create: %d bytes, %v", len(k1), err)
	}
	k2, err := loadSessionKey(path)
	if err != nil || string(k1) != string(k2) {
		t.Errorf("a second start must reuse the key: %v", err)
	}
	if runtime.GOOS != "windows" {
		if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
			t.Errorf("secret.key mode %v, want 0600", fi.Mode().Perm())
		}
	}
	if string(k1) == strings.Repeat("\x00", keyLen) {
		t.Error("the key must be random")
	}

	short := filepath.Join(t.TempDir(), "secret.key")
	_ = os.WriteFile(short, []byte("short"), 0o600)
	if _, err := loadSessionKey(short); err == nil {
		t.Error("a truncated key must be refused, not replaced")
	}
	if b, _ := os.ReadFile(short); string(b) != "short" {
		t.Error("a refused key file must be left alone")
	}
}

func TestSessionCookieFlags(t *testing.T) {
	s, _, _ := newTest(t)
	s.sessions, _ = testSessions("k")
	if err := s.trustProxies("10.0.0.1"); err != nil {
		t.Fatal(err)
	}
	cookie := func(r *http.Request) *http.Cookie {
		w := httptest.NewRecorder()
		s.setSessionCookie(w, r, "tok")
		cs := w.Result().Cookies()
		if len(cs) != 1 {
			t.Fatalf("%d cookies", len(cs))
		}
		return cs[0]
	}
	req := func(remote string, hdr map[string]string, tlsOn bool) *http.Request {
		r := httptest.NewRequest("POST", "/api/login", nil)
		r.RemoteAddr = remote
		for k, v := range hdr {
			r.Header.Set(k, v)
		}
		if tlsOn {
			r.TLS = &tls.ConnectionState{}
		}
		return r
	}

	plain := cookie(req(lan, nil, false))
	if !plain.HttpOnly || plain.SameSite != http.SameSiteLaxMode || plain.Path != "/" || plain.Secure || plain.MaxAge != 30*24*3600 || plain.Value != "tok" {
		t.Errorf("plain HTTP cookie: %+v", plain)
	}
	if !cookie(req(lan, nil, true)).Secure {
		t.Error("a cookie over TLS must be Secure")
	}
	if !cookie(req("10.0.0.1:4000", map[string]string{"X-Forwarded-Proto": "https"}, false)).Secure {
		t.Error("a trusted proxy saying https must give a Secure cookie")
	}
	if !cookie(req("10.0.0.1:4000", map[string]string{"X-Forwarded-Proto": "http, https"}, false)).Secure {
		t.Error("the last X-Forwarded-Proto entry is the proxy's")
	}
	if cookie(req("10.0.0.1:4000", map[string]string{"X-Forwarded-Proto": "https, http"}, false)).Secure {
		t.Error("the last entry says http")
	}
	if cookie(req(lan, map[string]string{"X-Forwarded-Proto": "https"}, false)).Secure {
		t.Error("X-Forwarded-Proto from an untrusted peer must be ignored")
	}

	w := httptest.NewRecorder()
	s.clearSessionCookie(w, req(lan, nil, false))
	if c := w.Result().Cookies()[0]; c.MaxAge >= 0 || c.Value != "" || c.Name != sessionCookie {
		t.Errorf("clearing cookie: %+v", c)
	}
}
