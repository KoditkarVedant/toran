package main

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// admin API tests: "tester" (admin) is logged in by default through newTest.
func asCookie(c string) map[string]string {
	return map[string]string{"Cookie": sessionCookie + "=" + c}
}

func TestUsersAPIIsAdminOnly(t *testing.T) {
	s, h, _ := newTest(t)
	kid, kidCookie := addUser(t, s, "kid", roleUser)
	calls := []struct{ method, path string }{
		{"GET", "/api/users"}, {"POST", "/api/users"}, {"PATCH", "/api/users/" + kid.ID}, {"DELETE", "/api/users/" + kid.ID},
	}
	body := jbody(map[string]string{"name": "x", "role": "user", "password": goodPassword})
	for _, c := range calls {
		if r := anon(h, c.method, c.path, body); r.Code != 401 {
			t.Errorf("%s %s with no login: %d, want 401", c.method, c.path, r.Code)
		}
		if r := doWith(h, c.method, c.path, lan, body, asCookie(kidCookie)); r.Code != 403 {
			t.Errorf("%s %s as a plain user: %d, want 403", c.method, c.path, r.Code)
		}
	}
	if _, ok := s.accounts.byID(kid.ID); !ok || s.accounts.count() != 2 {
		t.Error("a refused request must change nothing")
	}
	if r := do(h, "POST", "/api/users", "8.8.8.8:1", body); r.Code != 403 {
		t.Errorf("an admin from outside the network: %d, want 403 (LAN_ONLY)", r.Code)
	}
	cross := map[string]string{"Sec-Fetch-Site": "cross-site"}
	cross["Cookie"] = sessionCookie + "=" + s.sessions.issue(testUser(s))
	if r := doWith(h, "POST", "/api/users", lan, body, cross); r.Code != 403 {
		t.Errorf("a cross-site request: %d, want 403", r.Code)
	}
}

func TestListUsersNeverShowsSecrets(t *testing.T) {
	s, h, _ := newTest(t)
	addUser(t, s, "kid", roleUser)
	r := do(h, "GET", "/api/users", lan, nil)
	if r.Code != 200 {
		t.Fatal(r.Code)
	}
	for _, secret := range []string{"pbkdf2", "hash", "tokenVersion"} {
		if strings.Contains(strings.ToLower(r.Body.String()), strings.ToLower(secret)) {
			t.Errorf("the list leaks %q: %s", secret, r.Body)
		}
	}
	var list []adminUserView
	if err := json.Unmarshal(r.Body.Bytes(), &list); err != nil || len(list) != 2 || list[0].Name != testUserName || list[1].Name != "kid" {
		t.Errorf("list: %+v %v", list, err)
	}
}

func TestCreateUser(t *testing.T) {
	s, h, _ := newTest(t)
	create := func(name, role, pw string) *httptest.ResponseRecorder {
		return do(h, "POST", "/api/users", lan, jbody(map[string]string{"name": name, "role": role, "password": pw}))
	}
	r := create(" Asha ", "user", goodPassword)
	if r.Code != 201 {
		t.Fatalf("create: %d %s", r.Code, r.Body)
	}
	if strings.Contains(r.Body.String(), "pbkdf2") {
		t.Error("the answer must not contain the hash")
	}
	if l := login(h, lan2, "asha", goodPassword); l.Code != 200 {
		t.Errorf("the new user must be able to log in: %d", l.Code)
	}
	if u, _ := s.accounts.byName("asha"); u.Role != roleUser {
		t.Errorf("role %q", u.Role)
	}
	if r := create("asha", "user", goodPassword); r.Code != 409 {
		t.Errorf("duplicate: %d, want 409", r.Code)
	}
	if r := create("ASHA", "admin", goodPassword); r.Code != 409 {
		t.Errorf("duplicate in another case: %d, want 409", r.Code)
	}
	for name, c := range map[string]*httptest.ResponseRecorder{
		"bad name":     create("../x", "user", goodPassword),
		"bad role":     create("bob", "root", goodPassword),
		"empty role":   create("bob", "", goodPassword),
		"weak":         create("bob", "user", "short"),
		"same as name": create("bobbybobby", "user", "BobbyBobby"),
	} {
		if c.Code != 400 {
			t.Errorf("%s: %d, want 400 (%s)", name, c.Code, c.Body)
		}
	}
	if s.accounts.count() != 2 {
		t.Errorf("only the one valid user must exist, got %d", s.accounts.count())
	}
	if r := do(h, "POST", "/api/users", lan, []byte("nope")); r.Code != 400 {
		t.Errorf("a bad body: %d", r.Code)
	}
}

func TestUpdateUser(t *testing.T) {
	s, h, _ := newTest(t)
	kid, kidCookie := addUser(t, s, "kid", roleUser)
	patch := func(id string, body map[string]any) *httptest.ResponseRecorder {
		return do(h, "PATCH", "/api/users/"+id, lan, jbody(body))
	}
	session := func(cookie string) any { return decode(t, send(h, "GET", "/api/session", lan, nil, cookie))["state"] }

	if session(kidCookie) != "ok" {
		t.Fatal("the user starts logged in")
	}
	if r := patch(kid.ID, map[string]any{"role": "admin"}); r.Code != 200 || !strings.Contains(r.Body.String(), `"role":"admin"`) {
		t.Errorf("promote: %d %s", r.Code, r.Body)
	}
	if u, _ := s.accounts.byID(kid.ID); u.Role != roleAdmin || u.Hash != kid.Hash {
		t.Errorf("only the role should change: %+v", u)
	}
	if r := do(h, "GET", "/api/users", lan, nil); r.Code != 200 {
		t.Fatal("the admin still works")
	}
	if r := doWith(h, "GET", "/api/users", lan, nil, asCookie(kidCookie)); r.Code != 200 {
		t.Errorf("a promotion takes effect on the existing login: %d", r.Code)
	}
	if r := patch(kid.ID, map[string]any{"role": "user"}); r.Code != 200 {
		t.Fatal(r.Code)
	}
	if r := doWith(h, "GET", "/api/users", lan, nil, asCookie(kidCookie)); r.Code != 403 {
		t.Errorf("a demotion takes effect at once: %d", r.Code)
	}

	if r := patch(kid.ID, map[string]any{"disabled": true}); r.Code != 200 {
		t.Fatalf("disable: %d", r.Code)
	}
	if session(kidCookie) != "login" || login(h, lan2, "kid", goodPassword).Code != 401 {
		t.Error("a disabled user is logged out and cannot log in")
	}
	if r := patch(kid.ID, map[string]any{"disabled": false}); r.Code != 200 {
		t.Fatal(r.Code)
	}
	if session(kidCookie) != "ok" || login(h, lan2, "kid", goodPassword).Code != 200 {
		t.Error("an enabled user can log in again")
	}

	if r := patch(kid.ID, map[string]any{"password": "a reset password"}); r.Code != 200 {
		t.Fatalf("reset: %d %s", r.Code, r.Body)
	}
	if session(kidCookie) != "login" {
		t.Error("a password reset must end the user's logins")
	}
	if login(h, lan2, "kid", goodPassword).Code != 401 || login(h, lan2, "kid", "a reset password").Code != 200 {
		t.Error("the old password stops working, the new one works")
	}
}

func TestUpdateUserRefusals(t *testing.T) {
	s, h, _ := newTest(t)
	kid, _ := addUser(t, s, "kid", roleUser)
	patch := func(id string, body any) int {
		return do(h, "PATCH", "/api/users/"+id, lan, jbody(body)).Code
	}
	me := testUser(s).ID
	for name, body := range map[string]map[string]any{
		"own role":     {"role": "user"},
		"own disable":  {"disabled": true},
		"own password": {"password": "another password"},
	} {
		if code := patch(me, body); code != 400 {
			t.Errorf("%s: %d, want 400", name, code)
		}
	}
	if u, _ := s.accounts.byID(me); u.Role != roleAdmin || u.Disabled {
		t.Error("the admin must be untouched")
	}
	if code := patch("0000000000000000", map[string]any{"role": "user"}); code != 404 {
		t.Errorf("unknown id: %d", code)
	}
	if code := patch(kid.ID, map[string]any{"role": "root"}); code != 400 {
		t.Errorf("bad role: %d", code)
	}
	if code := patch(kid.ID, map[string]any{"password": "short"}); code != 400 {
		t.Errorf("weak password: %d", code)
	}
	if u, _ := s.accounts.byID(kid.ID); u.Hash != kid.Hash || u.TokenVersion != 0 {
		t.Error("refused changes must leave the user untouched")
	}
	if r := do(h, "PATCH", "/api/users/"+kid.ID, lan, []byte("nope")); r.Code != 400 {
		t.Errorf("bad body: %d", r.Code)
	}
}

func TestDeleteUserKeepsTheirFiles(t *testing.T) {
	s, h, _ := newTest(t)
	kid, kidCookie := addUser(t, s, "kid", roleUser)
	save := doWith(h, "PUT", "/services.json", lan, []byte(`{"groups":[{"name":"private stuff"}]}`), asCookie(kidCookie))
	if save.Code != 204 {
		t.Fatal(save.Code)
	}
	old := s.userDir(kid)

	if r := do(h, "DELETE", "/api/users/"+kid.ID, lan, nil); r.Code != 204 {
		t.Fatalf("delete: %d %s", r.Code, r.Body)
	}
	if _, ok := s.accounts.byName("kid"); ok {
		t.Error("the account must be gone")
	}
	s.dashMu.Lock()
	_, cached := s.dashboards[kid.ID]
	s.dashMu.Unlock()
	if cached {
		t.Error("the removed user's dashboard must be dropped from memory")
	}
	if r := send(h, "GET", "/api/session", lan, nil, kidCookie); decode(t, r)["state"] != "login" {
		t.Error("their login must end")
	}
	if _, err := os.Stat(old); err == nil {
		t.Error("the live folder must be set aside")
	}
	matches, _ := filepath.Glob(filepath.Join(s.root, "users", ".removed-"+kid.ID+"-*", "services.json"))
	if len(matches) != 1 {
		t.Fatalf("their files must be kept: %v", matches)
	}
	if got, _ := os.ReadFile(matches[0]); !strings.Contains(string(got), "private stuff") {
		t.Error("the kept config must be intact")
	}

	// the same name again is a new person with an empty dashboard, not a way into the old files
	r := do(h, "POST", "/api/users", lan, jbody(map[string]string{"name": "kid", "role": "user", "password": goodPassword}))
	if r.Code != 201 {
		t.Fatal(r.Code)
	}
	again := login(h, lan2, "kid", goodPassword)
	if got := doWith(h, "GET", "/services.json", lan, nil, asCookie(cookieOf(again))); got.Code != 404 {
		t.Errorf("a new user with an old name must start empty: %d %s", got.Code, got.Body)
	}
}

func TestDeleteUserRefusals(t *testing.T) {
	s, h, _ := newTest(t)
	if r := do(h, "DELETE", "/api/users/"+testUser(s).ID, lan, nil); r.Code != 400 {
		t.Errorf("removing yourself: %d, want 400", r.Code)
	}
	if r := do(h, "DELETE", "/api/users/0000000000000000", lan, nil); r.Code != 404 {
		t.Errorf("unknown id: %d, want 404", r.Code)
	}
	if _, ok := s.accounts.byName(testUserName); !ok {
		t.Error("the admin must still exist")
	}
}

func TestUserErrorStatuses(t *testing.T) {
	for err, want := range map[error]int{
		errBadName: 400, errBadRole: 400, errNameTaken: 409, errLastAdmin: 409, errNoUser: 404, os.ErrPermission: 500,
	} {
		w := httptest.NewRecorder()
		userError(w, err)
		if w.Code != want {
			t.Errorf("%v: %d, want %d", err, w.Code, want)
		}
	}
	w := httptest.NewRecorder()
	userError(w, os.ErrPermission)
	if strings.Contains(w.Body.String(), "permission") {
		t.Error("internal errors must not be shown to the client")
	}
}
