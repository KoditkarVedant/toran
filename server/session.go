package main

// Login sessions: a signed cookie, nothing stored on the server.
//
// After a successful login the browser gets a cookie holding the user's id, the user's
// token version and an expiry, signed with HMAC-SHA256 using a key kept in secret.key. A
// cookie that was changed, or signed with another key, is rejected. Because nothing is stored,
// logging a user out everywhere means raising their token version in users.json; every cookie
// issued before that stops matching.
//
// The cookie is HttpOnly (page scripts cannot read it), SameSite=Lax (not sent with
// cross-site POSTs) and, when Toran is reached over HTTPS, Secure.

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	sessionCookie = "toran_session"
	keyLen        = 32
	refreshAfter  = 24 * time.Hour // a cookie older than this is re-issued, so active users never expire
)

// claims is what a session cookie says. Short names keep the cookie small.
type claims struct {
	UID      string `json:"u"`
	TokenVer int    `json:"v"`
	Issued   int64  `json:"i"` // unix seconds
	Expires  int64  `json:"e"`
}

type sessions struct {
	key  []byte
	life time.Duration
	now  func() time.Time
}

func newSessions(key []byte, life time.Duration) *sessions {
	return &sessions{key: key, life: life, now: time.Now}
}

// loadSessionKey reads the signing key, creating it (readable only by its owner) on first start.
// A key that is too short is refused rather than silently replaced: replacing it would log
// everyone out, and a truncated file usually means something else is wrong.
func loadSessionKey(path string) ([]byte, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		key := make([]byte, keyLen)
		if _, err := rand.Read(key); err != nil {
			return nil, err
		}
		if err := atomicWriteMode(path, key, 0o600); err != nil {
			return nil, fmt.Errorf("create %s: %w", path, err)
		}
		return key, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if len(b) < keyLen {
		return nil, fmt.Errorf("%s is %d bytes, expected at least %d; delete it to create a new key (this logs everyone out)", path, len(b), keyLen)
	}
	return b, nil
}

var tokenEnc = base64.RawURLEncoding

// sign returns the MAC of a payload. The fixed prefix keeps a signature made here from being
// valid for any other purpose the same key might later be used for.
func (s *sessions) sign(payload []byte) []byte {
	m := hmac.New(sha256.New, s.key)
	m.Write([]byte("toran-session-v1\x00"))
	m.Write(payload)
	return m.Sum(nil)
}

// issue makes a cookie value for the user.
func (s *sessions) issue(u user) string {
	now := s.now()
	payload, _ := json.Marshal(claims{UID: u.ID, TokenVer: u.TokenVersion, Issued: now.Unix(), Expires: now.Add(s.life).Unix()})
	return tokenEnc.EncodeToString(payload) + "." + tokenEnc.EncodeToString(s.sign(payload))
}

// parse checks a cookie value's signature and expiry. It does not know whether the user still
// exists or has been logged out; the caller compares the user's current token version.
func (s *sessions) parse(token string) (claims, bool) {
	p, m, ok := strings.Cut(token, ".")
	if !ok || strings.Contains(m, ".") {
		return claims{}, false
	}
	payload, err1 := tokenEnc.DecodeString(p)
	mac, err2 := tokenEnc.DecodeString(m)
	if err1 != nil || err2 != nil || !hmac.Equal(mac, s.sign(payload)) {
		return claims{}, false
	}
	var c claims
	if json.Unmarshal(payload, &c) != nil || c.UID == "" || s.now().Unix() >= c.Expires {
		return claims{}, false
	}
	return c, true
}

// stale reports whether a still-valid cookie is old enough to be re-issued.
func (s *sessions) stale(c claims) bool {
	return s.now().Unix()-c.Issued >= int64(refreshAfter/time.Second)
}

// isHTTPS reports whether the visitor is using HTTPS: directly, or through a proxy we trust to say so.
func (s *server) isHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	peer, ok := parseAddr(r.RemoteAddr)
	if !ok || !s.isTrustedProxy(peer) {
		return false
	}
	vals := r.Header.Values("X-Forwarded-Proto")
	if len(vals) == 0 {
		return false
	}
	parts := strings.Split(vals[len(vals)-1], ",") // the last entry is the one our own proxy wrote
	return strings.EqualFold(strings.TrimSpace(parts[len(parts)-1]), "https")
}

func (s *server) setSessionCookie(w http.ResponseWriter, r *http.Request, token string) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: token, Path: "/", MaxAge: int(s.sessions.life / time.Second),
		HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: s.isHTTPS(r),
	})
}

func (s *server) clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: s.isHTTPS(r),
	})
}
