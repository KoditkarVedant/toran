package main

// Login: the endpoints that create the first admin, log in and out, change a password and say
// who is logged in, plus the wrappers (authed, admin) that make any other route require a user.
//
// First run. While users.json has no users, /api/session says "setup". The first admin is created
// with /api/setup, which needs a setup code that the server prints in its log (or SETUP_CODE).
// The code is what stops a stranger who reaches a brand-new server before you do from becoming
// its admin; whoever can read the server's log can.
//
// Login. A wrong name and a wrong password get the same answer and cost the same time (the
// password of a name that does not exist is checked against a dummy hash), so the form does not
// reveal which names exist. Failures are counted per visitor (throttle.go).

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// authConfig holds the settings the accounts feature needs; main reads them from the environment.
type authConfig struct {
	iterations int           // PBKDF2 iterations for new hashes
	life       time.Duration // how long a login lasts
	setupCode  string        // fixed first-run code; empty = a random one
}

func defaultAuthConfig() authConfig {
	return authConfig{iterations: defaultIterations, life: 30 * 24 * time.Hour}
}

// initAuth opens users.json and the session key. Call it once, before serving.
func (s *server) initAuth(cfg authConfig) error {
	a, err := openAccounts(filepath.Join(s.root, "users.json"))
	if err != nil {
		return err
	}
	key, err := loadSessionKey(filepath.Join(s.root, "secret.key"))
	if err != nil {
		return err
	}
	dummy, err := randomText(16)
	if err != nil {
		return err
	}
	s.dummyHash, err = hashPassword(dummy, cfg.iterations)
	if err != nil {
		return err
	}
	s.accounts, s.sessions = a, newSessions(key, cfg.life)
	s.limiter, s.gate, s.iterations = newFailureLimiter(), newHashGate(2), cfg.iterations
	s.setupCode = normalizeCode(cfg.setupCode)
	return nil
}

// ---- small helpers ----

func jsonError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

// decodeBody reads a small JSON request body, answering 400 itself when it is not one.
func decodeBody(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(v); err != nil {
		jsonError(w, http.StatusBadRequest, "expected a small JSON body")
		return false
	}
	return true
}

type publicUser struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Role string `json:"role"`
}

func (u user) public() publicUser { return publicUser{u.ID, u.Name, u.Role} }

const setupAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789" // no 0/O or 1/I: the code is read from a log and typed

func randomText(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	for i := range b {
		b[i] = setupAlphabet[int(b[i])%len(setupAlphabet)] // 32 letters: no bias
	}
	return string(b), nil
}

// normalizeCode makes a typed code comparable: upper case, without spaces and dashes.
func normalizeCode(c string) string {
	return strings.ToUpper(strings.NewReplacer(" ", "", "-", "").Replace(c))
}

// pendingSetupCode is the code the first admin must give, or "" once there is a user. It makes
// one when none exists yet (for instance after users.json was deleted) and logs it.
func (s *server) pendingSetupCode() string {
	s.setupMu.Lock()
	defer s.setupMu.Unlock()
	if s.accounts.count() > 0 {
		s.setupCode = ""
		return ""
	}
	if s.setupCode == "" {
		t, err := randomText(12)
		if err != nil {
			log.Printf("setup code: %v", err)
			return ""
		}
		s.setupCode = t
		log.Printf("first run: open Toran in a browser to create the admin account. Setup code: %s-%s-%s", t[:4], t[4:8], t[8:])
	}
	return s.setupCode
}

func (s *server) tooManyAttempts(w http.ResponseWriter, wait time.Duration) {
	secs := int(math.Ceil(wait.Seconds()))
	w.Header().Set("Retry-After", strconv.Itoa(secs))
	jsonError(w, http.StatusTooManyRequests, fmt.Sprintf("too many failed attempts; try again in %d minute(s)", (secs+59)/60))
}

// checkPasswordGated verifies a password while holding one of the few password-check slots.
// busy is true when no slot became free in time.
func (s *server) checkPasswordGated(ctx context.Context, password, stored string) (ok, upgrade, busy bool) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	release, got := s.gate.acquire(ctx)
	if !got {
		return false, false, true
	}
	defer release()
	ok, upgrade = verifyPassword(password, stored, s.iterations)
	return ok, upgrade, false
}

func (s *server) hashGated(ctx context.Context, password string) (string, bool) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	release, got := s.gate.acquire(ctx)
	if !got {
		return "", false
	}
	defer release()
	h, err := hashPassword(password, s.iterations)
	if err != nil {
		log.Printf("hash password: %v", err)
		return "", false
	}
	return h, true
}

// ---- who is logged in ----

// userFromRequest returns the logged-in user. A valid but old cookie is replaced by a fresh one,
// so people who use Toran never get logged out. The user's token version must match the cookie's:
// that is how a password change or a disabled account ends existing sessions.
func (s *server) userFromRequest(w http.ResponseWriter, r *http.Request) (user, bool) {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return user{}, false
	}
	cl, ok := s.sessions.parse(c.Value)
	if !ok {
		return user{}, false
	}
	u, ok := s.accounts.byID(cl.UID)
	if !ok || u.Disabled || u.TokenVersion != cl.TokenVer {
		return user{}, false
	}
	if s.sessions.stale(cl) {
		s.setSessionCookie(w, r, s.sessions.issue(u))
	}
	return u, true
}

// authed makes a route require a logged-in user and hands the user to the handler.
func (s *server) authed(h func(http.ResponseWriter, *http.Request, user)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, ok := s.userFromRequest(w, r)
		if !ok {
			jsonError(w, http.StatusUnauthorized, "login required")
			return
		}
		h(w, r, u)
	}
}

// admin is authed, and also requires the admin role.
func (s *server) admin(h func(http.ResponseWriter, *http.Request, user)) http.HandlerFunc {
	return s.authed(func(w http.ResponseWriter, r *http.Request, u user) {
		if u.Role != roleAdmin {
			jsonError(w, http.StatusForbidden, "only an admin can do this")
			return
		}
		h(w, r, u)
	})
}

// sessionInfo tells the page which screen to show: first-run setup, login, or the dashboard.
func (s *server) sessionInfo(w http.ResponseWriter, r *http.Request) {
	if s.accounts.count() == 0 {
		s.pendingSetupCode() // makes sure a code exists and is in the log
		writeJSON(w, 200, map[string]any{"state": "setup"})
		return
	}
	if u, ok := s.userFromRequest(w, r); ok {
		writeJSON(w, 200, map[string]any{"state": "ok", "user": u.public()})
		return
	}
	writeJSON(w, 200, map[string]any{"state": "login"})
}

// ---- setup, login, logout, password ----

func (s *server) setup(w http.ResponseWriter, r *http.Request) {
	if !s.clientAllowed(r) {
		jsonError(w, http.StatusForbidden, "writes are limited to the local network")
		return
	}
	var req struct{ Code, Name, Password string }
	if !decodeBody(w, r, &req) {
		return
	}
	ip, ok := s.clientIP(r)
	if !ok {
		jsonError(w, http.StatusBadRequest, "cannot tell where this request came from")
		return
	}
	addr := ip.String()

	s.setupMu.Lock() // one setup at a time: two requests must not both become "the first admin"
	defer s.setupMu.Unlock()
	if s.accounts.count() > 0 {
		jsonError(w, http.StatusConflict, "Toran is already set up")
		return
	}
	if allowed, wait := s.limiter.allow(addr, "setup"); !allowed {
		s.tooManyAttempts(w, wait)
		return
	}
	want := s.setupCode
	if want == "" {
		jsonError(w, http.StatusConflict, "open the page first; the setup code is shown in the server log")
		return
	}
	if subtle.ConstantTimeCompare([]byte(normalizeCode(req.Code)), []byte(want)) != 1 {
		s.limiter.failed(addr, "setup")
		jsonError(w, http.StatusForbidden, "wrong setup code (see the server log)")
		return
	}
	name := normalizeName(req.Name)
	if !userNameRegex.MatchString(name) {
		jsonError(w, http.StatusBadRequest, errBadName.Error())
		return
	}
	if err := checkPassword(name, req.Password); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	hash, ok := s.hashGated(r.Context(), req.Password)
	if !ok {
		jsonError(w, http.StatusServiceUnavailable, "the server is busy; try again")
		return
	}
	u, err := s.accounts.create(name, roleAdmin, hash)
	if err != nil {
		log.Printf("setup: %v", err)
		jsonError(w, http.StatusInternalServerError, "could not save the account")
		return
	}
	s.setupCode = ""
	log.Printf("first admin %q created", u.Name)
	s.adoptLegacyData(u) // an earlier single-user dashboard becomes theirs
	s.setSessionCookie(w, r, s.sessions.issue(u))
	writeJSON(w, 200, map[string]any{"user": u.public()})
}

func (s *server) login(w http.ResponseWriter, r *http.Request) {
	var req struct{ Name, Password string }
	if !decodeBody(w, r, &req) {
		return
	}
	ip, ok := s.clientIP(r)
	if !ok {
		jsonError(w, http.StatusBadRequest, "cannot tell where this request came from")
		return
	}
	addr, name := ip.String(), normalizeName(req.Name)
	if allowed, wait := s.limiter.allow(addr, name); !allowed {
		s.tooManyAttempts(w, wait)
		return
	}

	u, found := s.accounts.byName(name)
	stored := s.dummyHash // same work whether or not the name exists
	if found {
		stored = u.Hash
	}
	match, upgrade, busy := s.checkPasswordGated(r.Context(), req.Password, stored)
	if busy {
		jsonError(w, http.StatusServiceUnavailable, "the server is busy; try again")
		return
	}
	if !found || !match || u.Disabled {
		s.limiter.failed(addr, name)
		jsonError(w, http.StatusUnauthorized, "wrong name or password")
		return
	}
	s.limiter.succeeded(addr, name)

	if upgrade { // the hash was made with fewer iterations than we use now
		if h, ok := s.hashGated(r.Context(), req.Password); ok {
			if nu, err := s.accounts.update(u.ID, func(x *user) error { x.Hash = h; return nil }); err == nil {
				u = nu
			}
		}
	}
	s.setSessionCookie(w, r, s.sessions.issue(u))
	writeJSON(w, 200, map[string]any{"user": u.public()})
}

func (s *server) logout(w http.ResponseWriter, r *http.Request) {
	s.clearSessionCookie(w, r)
	w.WriteHeader(http.StatusNoContent)
}

// changePassword sets a new password for the logged-in user. Raising the token version logs the
// account out everywhere else; this browser gets a fresh cookie so it stays logged in.
func (s *server) changePassword(w http.ResponseWriter, r *http.Request, u user) {
	var req struct{ Current, New string }
	if !decodeBody(w, r, &req) {
		return
	}
	ip, ok := s.clientIP(r)
	if !ok {
		jsonError(w, http.StatusBadRequest, "cannot tell where this request came from")
		return
	}
	addr := ip.String()
	if allowed, wait := s.limiter.allow(addr, u.Name); !allowed {
		s.tooManyAttempts(w, wait)
		return
	}
	match, _, busy := s.checkPasswordGated(r.Context(), req.Current, u.Hash)
	if busy {
		jsonError(w, http.StatusServiceUnavailable, "the server is busy; try again")
		return
	}
	if !match {
		s.limiter.failed(addr, u.Name)
		// 403, not 401: the page treats 401 as "your session ended"
		jsonError(w, http.StatusForbidden, "the current password is wrong")
		return
	}
	s.limiter.succeeded(addr, u.Name)
	if err := checkPassword(u.Name, req.New); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	hash, ok := s.hashGated(r.Context(), req.New)
	if !ok {
		jsonError(w, http.StatusServiceUnavailable, "the server is busy; try again")
		return
	}
	nu, err := s.accounts.update(u.ID, func(x *user) error { x.Hash = hash; x.TokenVersion++; return nil })
	if err != nil {
		log.Printf("change password: %v", err)
		jsonError(w, http.StatusInternalServerError, "could not save the new password")
		return
	}
	s.setSessionCookie(w, r, s.sessions.issue(nu))
	w.WriteHeader(http.StatusNoContent)
}
