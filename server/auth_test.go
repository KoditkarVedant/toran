package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	setupCodeForTests = "ABCD-EFGH-JKLM"
	goodPassword      = "correct horse battery"
	lan2              = "192.168.1.21:7777"
)

func newAuthTestN(t *testing.T, iterations int) (*server, http.Handler) {
	t.Helper()
	s := newBare(t)
	if err := s.initAuth(authConfig{iterations: iterations, life: 30 * 24 * time.Hour, setupCode: setupCodeForTests}); err != nil {
		t.Fatal(err)
	}
	return s, s.routes()
}

func newAuthTest(t *testing.T) (*server, http.Handler) { return newAuthTestN(t, testIterations) }

func jbody(v any) []byte { b, _ := json.Marshal(v); return b }

// send makes a request with an optional JSON body and session cookie.
func send(h http.Handler, method, path, remote string, body any, cookie string) *httptest.ResponseRecorder {
	var raw []byte
	if body != nil {
		raw = jbody(body)
	}
	hdr := map[string]string{}
	if cookie != "" {
		hdr["Cookie"] = sessionCookie + "=" + cookie
	}
	return doWith(h, method, path, remote, raw, hdr)
}

func cookieOf(r *httptest.ResponseRecorder) string {
	for _, c := range r.Result().Cookies() {
		if c.Name == sessionCookie && c.MaxAge >= 0 {
			return c.Value
		}
	}
	return ""
}

func decode(t *testing.T, r *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(r.Body.Bytes(), &m); err != nil {
		t.Fatalf("not JSON: %q", r.Body.String())
	}
	return m
}

// setupAdmin runs first-run setup and returns the admin's cookie.
func setupAdmin(t *testing.T, h http.Handler, name string) string {
	t.Helper()
	r := send(h, "POST", "/api/setup", lan, map[string]string{"code": setupCodeForTests, "name": name, "password": goodPassword}, "")
	if r.Code != 200 {
		t.Fatalf("setup: %d %s", r.Code, r.Body)
	}
	return cookieOf(r)
}

func login(h http.Handler, remote, name, password string) *httptest.ResponseRecorder {
	return send(h, "POST", "/api/login", remote, map[string]string{"name": name, "password": password}, "")
}

func TestSessionStates(t *testing.T) {
	_, h := newAuthTest(t)
	if m := decode(t, send(h, "GET", "/api/session", lan, nil, "")); m["state"] != "setup" {
		t.Errorf("fresh server: %v", m)
	}
	cookie := setupAdmin(t, h, "vedant")
	m := decode(t, send(h, "GET", "/api/session", lan, nil, cookie))
	user, _ := m["user"].(map[string]any)
	if m["state"] != "ok" || user["name"] != "vedant" || user["role"] != "admin" || user["id"] == "" {
		t.Errorf("logged in: %v", m)
	}
	if m := decode(t, send(h, "GET", "/api/session", lan, nil, "")); m["state"] != "login" {
		t.Errorf("no cookie: %v", m)
	}
	if m := decode(t, send(h, "GET", "/api/session", lan, nil, cookie+"x")); m["state"] != "login" {
		t.Errorf("tampered cookie: %v", m)
	}
}

func TestSetupNeedsTheCode(t *testing.T) {
	s, h := newAuthTest(t)
	body := func(code string) map[string]string {
		return map[string]string{"code": code, "name": "vedant", "password": goodPassword}
	}
	for _, bad := range []string{"", "WRONG-WRONG-WRON", "abcd-efgh-jklm-x"} {
		if r := send(h, "POST", "/api/setup", lan, body(bad), ""); r.Code != 403 {
			t.Errorf("code %q: status %d, want 403", bad, r.Code)
		}
	}
	if s.accounts.count() != 0 {
		t.Fatal("no account may be created with a wrong code")
	}
	// the code is typed by a person: case, spaces and dashes do not matter
	if r := send(h, "POST", "/api/setup", lan, body(" abcd efgh-jklm "), ""); r.Code != 200 {
		t.Errorf("typed code: %d %s", r.Code, r.Body)
	}
}

func TestSetupGuessingIsThrottled(t *testing.T) {
	_, h := newAuthTest(t)
	for i := 0; i < 5; i++ {
		send(h, "POST", "/api/setup", lan, map[string]string{"code": "GUESS-NUMBER-" + string(rune('A'+i)), "name": "x", "password": goodPassword}, "")
	}
	r := send(h, "POST", "/api/setup", lan, map[string]string{"code": setupCodeForTests, "name": "vedant", "password": goodPassword}, "")
	if r.Code != 429 || r.Header().Get("Retry-After") == "" {
		t.Errorf("after five wrong codes even the right one waits: %d %s", r.Code, r.Body)
	}
}

func TestSetupValidatesNameAndPassword(t *testing.T) {
	s, h := newAuthTest(t)
	for name, body := range map[string]map[string]string{
		"bad name":      {"code": setupCodeForTests, "name": "../x", "password": goodPassword},
		"short":         {"code": setupCodeForTests, "name": "vedant", "password": "short"},
		"password=name": {"code": setupCodeForTests, "name": "vedantvedant", "password": "VedantVedant"},
	} {
		if r := send(h, "POST", "/api/setup", lan, body, ""); r.Code != 400 {
			t.Errorf("%s: status %d, want 400 (%s)", name, r.Code, r.Body)
		}
	}
	if s.accounts.count() != 0 {
		t.Error("refused setups must not create anything")
	}
}

func TestSetupOnlyOnceAndOnlyFromTheLAN(t *testing.T) {
	s, h := newAuthTest(t)
	ok := map[string]string{"code": setupCodeForTests, "name": "vedant", "password": goodPassword}
	if r := send(h, "POST", "/api/setup", "8.8.8.8:1", ok, ""); r.Code != 403 {
		t.Errorf("setup from outside the network: %d, want 403", r.Code)
	}
	cookie := setupAdmin(t, h, "vedant")
	if r := send(h, "POST", "/api/setup", lan, ok, ""); r.Code != 409 {
		t.Errorf("second setup: %d, want 409", r.Code)
	}
	if r := send(h, "POST", "/api/setup", lan, map[string]string{"code": setupCodeForTests, "name": "evil", "password": goodPassword}, cookie); r.Code != 409 {
		t.Errorf("setup while logged in: %d, want 409", r.Code)
	}
	if s.accounts.count() != 1 || s.setupCode != "" {
		t.Errorf("%d users, setupCode %q", s.accounts.count(), s.setupCode)
	}
}

func TestSetupRace(t *testing.T) {
	s, h := newAuthTest(t)
	var wg sync.WaitGroup
	codes := make(chan int, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			name := "admin" + string(rune('a'+i))
			codes <- send(h, "POST", "/api/setup", lan, map[string]string{"code": setupCodeForTests, "name": name, "password": goodPassword}, "").Code
		}(i)
	}
	wg.Wait()
	close(codes)
	won := 0
	for c := range codes {
		if c == 200 {
			won++
		}
	}
	if won != 1 || s.accounts.count() != 1 {
		t.Errorf("%d setups succeeded, %d users; want exactly one", won, s.accounts.count())
	}
}

func TestSetupSetsASafeCookie(t *testing.T) {
	_, h := newAuthTest(t)
	r := send(h, "POST", "/api/setup", lan, map[string]string{"code": setupCodeForTests, "name": "vedant", "password": goodPassword}, "")
	var c *http.Cookie
	for _, x := range r.Result().Cookies() {
		if x.Name == sessionCookie {
			c = x
		}
	}
	if c == nil || !c.HttpOnly || c.SameSite != http.SameSiteLaxMode || c.Value == "" {
		t.Errorf("cookie %+v", c)
	}
	if strings.Contains(r.Body.String(), "pbkdf2") || strings.Contains(r.Body.String(), goodPassword) {
		t.Errorf("a response must never contain a hash or password: %s", r.Body)
	}
}

func TestLogin(t *testing.T) {
	s, h := newAuthTest(t)
	setupAdmin(t, h, "vedant")

	r := login(h, lan, "Vedant", goodPassword) // the name is case-insensitive
	if r.Code != 200 || cookieOf(r) == "" {
		t.Fatalf("login: %d %s", r.Code, r.Body)
	}
	if m := decode(t, send(h, "GET", "/api/session", lan, nil, cookieOf(r))); m["state"] != "ok" {
		t.Errorf("the login cookie must work: %v", m)
	}

	wrong := login(h, lan, "vedant", "not the password")
	unknown := login(h, lan, "nobody", goodPassword)
	if wrong.Code != 401 || unknown.Code != 401 || wrong.Body.String() != unknown.Body.String() {
		t.Errorf("wrong password and unknown name must look the same: %d %q / %d %q", wrong.Code, wrong.Body, unknown.Code, unknown.Body)
	}
	if cookieOf(wrong) != "" || cookieOf(unknown) != "" {
		t.Error("a failed login must not set a session")
	}

	hash, _ := hashPassword(goodPassword, testIterations)
	kid, _ := s.accounts.create("kid", roleUser, hash)
	if r := login(h, lan2, "kid", goodPassword); r.Code != 200 {
		t.Fatalf("an enabled user: %d", r.Code)
	}
	if _, err := s.accounts.update(kid.ID, func(x *user) error { x.Disabled = true; return nil }); err != nil {
		t.Fatal(err)
	}
	if r := login(h, lan2, "kid", goodPassword); r.Code != 401 || r.Body.String() != wrong.Body.String() {
		t.Errorf("a disabled account: %d %q (the right password must not reveal that the account exists)", r.Code, r.Body)
	}
}

func TestLoginLockout(t *testing.T) {
	_, h := newAuthTest(t)
	setupAdmin(t, h, "vedant")
	for i := 0; i < 5; i++ {
		if r := login(h, lan, "vedant", "wrong password!"); r.Code != 401 {
			t.Fatalf("attempt %d: %d", i+1, r.Code)
		}
	}
	r := login(h, lan, "vedant", goodPassword)
	if r.Code != 429 || r.Header().Get("Retry-After") == "" {
		t.Errorf("the right password after five failures must still wait: %d", r.Code)
	}
	if cookieOf(r) != "" {
		t.Error("a throttled login must not set a session")
	}
	if r := login(h, lan2, "vedant", goodPassword); r.Code != 200 {
		t.Errorf("another address is not locked out: %d", r.Code)
	}
}

func TestSuccessResetsTheCount(t *testing.T) {
	_, h := newAuthTest(t)
	setupAdmin(t, h, "vedant")
	for round := 0; round < 3; round++ {
		for i := 0; i < 4; i++ {
			login(h, lan, "vedant", "wrong password!")
		}
		if r := login(h, lan, "vedant", goodPassword); r.Code != 200 {
			t.Fatalf("round %d: %d", round, r.Code)
		}
	}
}

// An unknown name must cost as much as a wrong password, or response times reveal which names
// exist. The floor is low enough to hold on any machine and far above "no hash was computed".
func TestUnknownNameStillCostsAHash(t *testing.T) {
	_, h := newAuthTestN(t, 300_000)
	setupAdmin(t, h, "vedant")
	start := time.Now()
	r := login(h, lan, "nobody-here", goodPassword)
	elapsed := time.Since(start)
	if r.Code != 401 {
		t.Fatalf("status %d", r.Code)
	}
	if elapsed < 5*time.Millisecond {
		t.Errorf("an unknown name answered in %v: it skipped the password check", elapsed)
	}
}

func TestLoginUpgradesOldHashes(t *testing.T) {
	s, h := newAuthTest(t)
	setupAdmin(t, h, "vedant")
	u, _ := s.accounts.byName("vedant")
	old, _ := hashPassword(goodPassword, 500)
	_, _ = s.accounts.update(u.ID, func(x *user) error { x.Hash = old; return nil })

	if r := login(h, lan, "vedant", goodPassword); r.Code != 200 {
		t.Fatalf("login with an older hash: %d", r.Code)
	}
	u, _ = s.accounts.byName("vedant")
	if !strings.HasPrefix(u.Hash, "pbkdf2-sha256$1000$") {
		t.Errorf("the stored hash should have been upgraded: %q", u.Hash)
	}
	if r := login(h, lan, "vedant", goodPassword); r.Code != 200 {
		t.Error("the upgraded hash must still verify")
	}
}

func TestLogout(t *testing.T) {
	_, h := newAuthTest(t)
	setupAdmin(t, h, "vedant")
	r := send(h, "POST", "/api/logout", lan, nil, "")
	var cleared bool
	for _, c := range r.Result().Cookies() {
		cleared = cleared || (c.Name == sessionCookie && c.MaxAge < 0)
	}
	if r.Code != 204 || !cleared {
		t.Errorf("logout: %d, cleared=%v", r.Code, cleared)
	}
}

func TestChangePassword(t *testing.T) {
	_, h := newAuthTest(t)
	setupAdmin(t, h, "vedant")
	phone := cookieOf(login(h, lan2, "vedant", goodPassword)) // a second device
	laptop := cookieOf(login(h, lan, "vedant", goodPassword))
	newPw := "a brand new password"

	change := func(cur, next, cookie string) *httptest.ResponseRecorder {
		return send(h, "POST", "/api/password", lan, map[string]string{"current": cur, "new": next}, cookie)
	}
	if r := change(goodPassword, newPw, ""); r.Code != 401 {
		t.Errorf("not logged in: %d", r.Code)
	}
	if r := change("wrong password", newPw, laptop); r.Code != 403 {
		t.Errorf("wrong current password: %d, want 403 (401 would look like an expired session)", r.Code)
	}
	if r := change(goodPassword, "short", laptop); r.Code != 400 {
		t.Errorf("weak new password: %d, want 400", r.Code)
	}
	r := change(goodPassword, newPw, laptop)
	if r.Code != 204 {
		t.Fatalf("change: %d %s", r.Code, r.Body)
	}
	fresh := cookieOf(r)
	if fresh == "" {
		t.Fatal("the browser that changed the password must stay logged in")
	}
	if m := decode(t, send(h, "GET", "/api/session", lan, nil, fresh)); m["state"] != "ok" {
		t.Errorf("the re-issued cookie: %v", m)
	}
	for name, c := range map[string]string{"the old cookie of this browser": laptop, "the other device": phone} {
		if m := decode(t, send(h, "GET", "/api/session", lan, nil, c)); m["state"] != "login" {
			t.Errorf("%s must be logged out: %v", name, m)
		}
	}
	if r := login(h, lan, "vedant", goodPassword); r.Code != 401 {
		t.Errorf("the old password must stop working: %d", r.Code)
	}
	if r := login(h, lan, "vedant", newPw); r.Code != 200 {
		t.Errorf("the new password must work: %d", r.Code)
	}
}

func TestChangePasswordGuessingIsThrottled(t *testing.T) {
	_, h := newAuthTest(t)
	cookie := setupAdmin(t, h, "vedant")
	for i := 0; i < 5; i++ {
		send(h, "POST", "/api/password", lan, map[string]string{"current": "nope nope nope", "new": "another password"}, cookie)
	}
	r := send(h, "POST", "/api/password", lan, map[string]string{"current": goodPassword, "new": "another password"}, cookie)
	if r.Code != 429 {
		t.Errorf("a stolen session must not be able to guess the current password: %d", r.Code)
	}
}

func TestAuthedAndAdminWrappers(t *testing.T) {
	s, _ := newAuthTest(t)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /who", s.authed(func(w http.ResponseWriter, r *http.Request, u user) { writeJSON(w, 200, u.public()) }))
	mux.HandleFunc("GET /admin", s.admin(func(w http.ResponseWriter, r *http.Request, u user) { writeJSON(w, 200, u.public()) }))
	h := mux

	hash, _ := hashPassword(goodPassword, testIterations)
	admin, _ := s.accounts.create("boss", roleAdmin, hash)
	kid, _ := s.accounts.create("kid", roleUser, hash)
	cookieFor := func(u user) string { return s.sessions.issue(u) }

	check := func(what string, got *httptest.ResponseRecorder, want int) {
		t.Helper()
		if got.Code != want {
			t.Errorf("%s: %d, want %d (%s)", what, got.Code, want, got.Body)
		}
	}
	check("no cookie", send(h, "GET", "/who", lan, nil, ""), 401)
	check("junk cookie", send(h, "GET", "/who", lan, nil, "junk"), 401)
	check("a user", send(h, "GET", "/who", lan, nil, cookieFor(kid)), 200)
	check("a user on the admin route", send(h, "GET", "/admin", lan, nil, cookieFor(kid)), 403)
	check("an admin on the admin route", send(h, "GET", "/admin", lan, nil, cookieFor(admin)), 200)
	check("no cookie on the admin route", send(h, "GET", "/admin", lan, nil, ""), 401)

	staleKid := cookieFor(kid)
	_, _ = s.accounts.update(kid.ID, func(x *user) error { x.TokenVersion++; return nil })
	check("a cookie from before a token-version bump", send(h, "GET", "/who", lan, nil, staleKid), 401)

	kid, _ = s.accounts.byID(kid.ID)
	ok := cookieFor(kid)
	_, _ = s.accounts.update(kid.ID, func(x *user) error { x.Disabled = true; return nil })
	check("a disabled user", send(h, "GET", "/who", lan, nil, ok), 401)
	_ = s.accounts.remove(kid.ID)
	check("a removed user", send(h, "GET", "/who", lan, nil, ok), 401)

	// a cookie signed with another key is worthless even for a real user
	other := newSessions([]byte(strings.Repeat("z", keyLen)), time.Hour)
	check("a cookie made with another key", send(h, "GET", "/who", lan, nil, other.issue(admin)), 401)
	// role changes take effect at once because the role is read from the store, not the cookie
	adminCookie := cookieFor(admin)
	_, _ = s.accounts.create("second", roleAdmin, hash)
	_, _ = s.accounts.update(admin.ID, func(x *user) error { x.Role = roleUser; return nil })
	check("a demoted admin with an old cookie", send(h, "GET", "/admin", lan, nil, adminCookie), 403)
}

func TestOldCookiesAreRefreshed(t *testing.T) {
	s, h := newAuthTest(t)
	cookie := setupAdmin(t, h, "vedant")
	now := time.Now()
	s.sessions.now = func() time.Time { return now.Add(2 * 24 * time.Hour) }
	r := send(h, "GET", "/api/session", lan, nil, cookie)
	if cookieOf(r) == "" || cookieOf(r) == cookie {
		t.Error("a two-day-old session should come back with a fresh cookie")
	}
	now = now.Add(31 * 24 * time.Hour)
	s.sessions.now = func() time.Time { return now.Add(2 * 24 * time.Hour) }
	if m := decode(t, send(h, "GET", "/api/session", lan, nil, cookie)); m["state"] != "login" {
		t.Errorf("the original cookie has expired by now: %v", m)
	}
}

func TestAuthEndpointsRejectCrossSiteRequests(t *testing.T) {
	_, h := newAuthTest(t)
	setupAdmin(t, h, "vedant")
	cross := map[string]string{"Sec-Fetch-Site": "cross-site"}
	body := jbody(map[string]string{"name": "vedant", "password": goodPassword})
	for _, path := range []string{"/api/login", "/api/logout", "/api/password", "/api/setup"} {
		if r := doWith(h, "POST", path, lan, body, cross); r.Code != 403 {
			t.Errorf("%s from another site: %d, want 403 (login CSRF would sign the victim in as someone else)", path, r.Code)
		}
	}
}

func TestBadBodies(t *testing.T) {
	_, h := newAuthTest(t)
	for _, path := range []string{"/api/login", "/api/setup"} {
		for _, body := range []string{"", "not json", "[]", strings.Repeat("x", 20000)} {
			if r := doWith(h, "POST", path, lan, []byte(body), nil); r.Code != 400 {
				t.Errorf("%s with %.10q: %d, want 400", path, body, r.Code)
			}
		}
	}
}

func TestSetupCodeIsMadeWhenMissing(t *testing.T) {
	s := newBare(t)
	if err := s.initAuth(authConfig{iterations: testIterations, life: time.Hour}); err != nil { // no fixed code
		t.Fatal(err)
	}
	code := s.pendingSetupCode()
	if len(code) != 12 || strings.Trim(code, setupAlphabet) != "" {
		t.Errorf("generated code %q", code)
	}
	if again := s.pendingSetupCode(); again != code {
		t.Error("the code must stay the same until used")
	}
	hash, _ := hashPassword(goodPassword, testIterations)
	_, _ = s.accounts.create("vedant", roleAdmin, hash)
	if s.pendingSetupCode() != "" {
		t.Error("once an admin exists there is no setup code")
	}
}

func TestAuthConfigFromEnv(t *testing.T) {
	t.Setenv("PBKDF2_ITERATIONS", "")
	t.Setenv("SESSION_DAYS", "")
	t.Setenv("SETUP_CODE", "")
	cfg, err := authConfigFromEnv()
	if err != nil || cfg.iterations != defaultIterations || cfg.life != 30*24*time.Hour || cfg.setupCode != "" {
		t.Errorf("defaults: %+v %v", cfg, err)
	}
	t.Setenv("PBKDF2_ITERATIONS", "2000")
	t.Setenv("SESSION_DAYS", "7")
	t.Setenv("SETUP_CODE", "my own code 123")
	cfg, err = authConfigFromEnv()
	if err != nil || cfg.iterations != 2000 || cfg.life != 7*24*time.Hour || cfg.setupCode != "my own code 123" {
		t.Errorf("custom: %+v %v", cfg, err)
	}
	for k, v := range map[string]string{"PBKDF2_ITERATIONS": "10", "SESSION_DAYS": "0", "SETUP_CODE": "short"} {
		t.Setenv("PBKDF2_ITERATIONS", "2000")
		t.Setenv("SESSION_DAYS", "7")
		t.Setenv("SETUP_CODE", "my own code 123")
		t.Setenv(k, v)
		if _, err := authConfigFromEnv(); err == nil {
			t.Errorf("%s=%q must be refused", k, v)
		}
	}
	t.Setenv("PBKDF2_ITERATIONS", "lots")
	if _, err := authConfigFromEnv(); err == nil {
		t.Error("a non-number must be refused")
	}
}

func TestResetPassword(t *testing.T) {
	s, h := newAuthTest(t)
	cookie := setupAdmin(t, h, "vedant")
	u, _ := s.accounts.byName("vedant")
	_, _ = s.accounts.update(u.ID, func(x *user) error { x.Disabled = false; return nil })

	if err := resetPassword(s.root, "nobody", goodPassword, testIterations); err == nil {
		t.Error("an unknown user must be an error")
	}
	if err := resetPassword(s.root, "vedant", "short", testIterations); err == nil {
		t.Error("the password rules apply here too")
	}
	if err := resetPassword(s.root, " Vedant ", "the recovered password", testIterations); err != nil {
		t.Fatal(err)
	}

	s.accounts.mu.Lock() // let the running server look at the file now instead of within a second
	s.accounts.refresh(true)
	s.accounts.mu.Unlock()
	if m := decode(t, send(h, "GET", "/api/session", lan, nil, cookie)); m["state"] != "login" {
		t.Errorf("the old session must end: %v", m)
	}
	if r := login(h, lan, "vedant", goodPassword); r.Code != 401 {
		t.Errorf("the old password: %d", r.Code)
	}
	if r := login(h, lan, "vedant", "the recovered password"); r.Code != 200 {
		t.Errorf("the new password: %d", r.Code)
	}
	if got, _ := s.accounts.byID(u.ID); got.TokenVersion != u.TokenVersion+1 || got.ID != u.ID {
		t.Errorf("the account must keep its id (and so its dashboard): %+v", got)
	}
}

func TestResetPasswordReenablesTheAccount(t *testing.T) {
	s, h := newAuthTest(t)
	setupAdmin(t, h, "vedant")
	hash, _ := hashPassword(goodPassword, testIterations)
	kid, _ := s.accounts.create("kid", roleUser, hash)
	_, _ = s.accounts.update(kid.ID, func(x *user) error { x.Disabled = true; return nil })
	if err := resetPassword(s.root, "kid", "a fresh password", testIterations); err != nil {
		t.Fatal(err)
	}
	s.accounts.mu.Lock()
	s.accounts.refresh(true)
	s.accounts.mu.Unlock()
	if r := login(h, lan, "kid", "a fresh password"); r.Code != 200 {
		t.Errorf("a reset re-enables the account: %d", r.Code)
	}
}
