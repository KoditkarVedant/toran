package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// addUser creates a user and returns them with a ready-made session cookie.
func addUser(t *testing.T, s *server, name, role string) (user, string) {
	t.Helper()
	hash, err := hashPassword(goodPassword, testIterations)
	if err != nil {
		t.Fatal(err)
	}
	u, err := s.accounts.create(name, role, hash)
	if err != nil {
		t.Fatal(err)
	}
	return u, s.sessions.issue(u)
}

func anon(h http.Handler, method, path string, body []byte) *httptest.ResponseRecorder {
	return doWith(h, method, path, lan, body, map[string]string{"X-Test-Anon": "1"})
}

func TestEverythingPersonalNeedsALogin(t *testing.T) {
	s, h, _ := newTest(t)
	hash := []byte(`{"groups":[]}`)
	// give the tester some data, then ask for it without logging in
	do(h, "PUT", "/services.json", lan, hash)
	do(h, "PUT", "/wallpaper/bg-1.jpg", lan, png1x1)
	do(h, "PUT", "/icons/a.png", lan, png1x1)

	protected := []struct{ method, path string }{
		{"GET", "/services.json"}, {"PUT", "/services.json"},
		{"GET", "/wallpaper/bg-1.jpg"}, {"PUT", "/wallpaper/bg-2.jpg"}, {"DELETE", "/wallpaper/bg-1.jpg"},
		{"GET", "/icons/a.png"}, {"PUT", "/icons/b.png"},
		{"GET", "/api/status"}, {"GET", "/api/backups"}, {"POST", "/api/backups/restore"},
		{"POST", "/api/password"},
	}
	for _, p := range protected {
		body := []byte(nil)
		if p.method == "PUT" || p.method == "POST" {
			body = png1x1
			if strings.HasSuffix(p.path, ".json") || strings.Contains(p.path, "/api/") {
				body = hash
			}
		}
		r := anon(h, p.method, p.path, body)
		if r.Code != 401 {
			t.Errorf("%s %s without a login: %d, want 401", p.method, p.path, r.Code)
		}
		if strings.Contains(r.Body.String(), "groups") || strings.Contains(r.Body.String(), "PNG") {
			t.Errorf("%s %s leaked data to a visitor: %q", p.method, p.path, r.Body.String())
		}
	}

	// an anonymous visitor still gets what the login screen needs, and nothing else
	_ = os.WriteFile(filepath.Join(s.root, "index.html"), []byte("<html>login</html>"), 0o644)
	_ = os.WriteFile(filepath.Join(s.root, "custom.css"), []byte("a{}"), 0o644)
	_ = os.MkdirAll(filepath.Join(s.root, "wallpapers"), 0o755)
	_ = os.WriteFile(filepath.Join(s.root, "wallpapers", "a.jpg"), []byte("x"), 0o644)
	for _, p := range []string{"/", "/index.html", "/custom.css", "/wallpapers/a.jpg", "/api/health", "/api/session"} {
		if r := anon(h, "GET", p, nil); r.Code != 200 {
			t.Errorf("GET %s should stay public: %d", p, r.Code)
		}
	}
}

func TestLANOnlyStillComesFirst(t *testing.T) {
	_, h, _ := newTest(t)
	// a visitor outside the network is refused before anything about their login is considered
	if r := doWith(h, "PUT", "/services.json", "8.8.8.8:1", []byte(`{"groups":[]}`), map[string]string{"X-Test-Anon": "1"}); r.Code != 403 {
		t.Errorf("outside visitor without login: %d, want 403", r.Code)
	}
	if r := do(h, "PUT", "/services.json", "8.8.8.8:1", []byte(`{"groups":[]}`)); r.Code != 403 {
		t.Errorf("outside visitor with a valid login: %d, want 403 (LAN_ONLY is a second lock)", r.Code)
	}
}

func TestUsersCannotSeeEachOthersData(t *testing.T) {
	s, h, _ := newTest(t) // "tester" is user A, logged in by default
	bUser, bCookie := addUser(t, s, "bea", roleUser)
	asB := func(method, path string, body []byte) *httptest.ResponseRecorder {
		return doWith(h, method, path, lan, body, map[string]string{"Cookie": sessionCookie + "=" + bCookie})
	}

	secretA := []byte(`{"groups":[{"name":"A","items":[{"name":"a-nas","url":"http://a-secret.lan:1"}]}]}`)
	secretB := []byte(`{"groups":[{"name":"B","items":[{"name":"b-nas","url":"http://b-secret.lan:2"}]}]}`)

	if r := do(h, "PUT", "/services.json", lan, secretA); r.Code != 204 {
		t.Fatal(r.Code)
	}
	if r := asB("GET", "/services.json", nil); r.Code != 404 {
		t.Errorf("B has saved nothing, so must not be served A's file: %d %s", r.Code, r.Body)
	}
	if r := asB("PUT", "/services.json", secretB); r.Code != 204 {
		t.Fatal(r.Code)
	}
	if r := do(h, "GET", "/services.json", lan, nil); !strings.Contains(r.Body.String(), "a-secret") || strings.Contains(r.Body.String(), "b-secret") {
		t.Errorf("A must get A's own config: %s", r.Body)
	}
	if r := asB("GET", "/services.json", nil); !strings.Contains(r.Body.String(), "b-secret") || strings.Contains(r.Body.String(), "a-secret") {
		t.Errorf("B must get B's own config: %s", r.Body)
	}
	if cc := do(h, "GET", "/services.json", lan, nil).Header().Get("Cache-Control"); !strings.Contains(cc, "private") {
		t.Errorf("a personal config must not be cacheable by shared caches: %q", cc)
	}

	// backups are per user: A's old versions are invisible to B, and B cannot restore them by name
	do(h, "PUT", "/services.json", lan, []byte(`{"groups":[{"name":"A2"}]}`))
	var aList []map[string]any
	_ = json.Unmarshal(do(h, "GET", "/api/backups", lan, nil).Body.Bytes(), &aList)
	if len(aList) == 0 {
		t.Fatal("A should have a backup by now")
	}
	name, _ := aList[0]["name"].(string)
	var bList []map[string]any
	_ = json.Unmarshal(asB("GET", "/api/backups", nil).Body.Bytes(), &bList)
	for _, b := range bList {
		if b["name"] == name {
			t.Errorf("B can see A's backup %s", name)
		}
	}
	if r := asB("POST", "/api/backups/restore", jbody(map[string]string{"name": name})); r.Code != 404 {
		t.Errorf("B restoring A's backup name: %d, want 404", r.Code)
	}
	if got, _ := os.ReadFile(filepath.Join(s.userDir(bUser), "services.json")); !strings.Contains(string(got), "b-secret") {
		t.Errorf("B's config was changed: %s", got)
	}

	// wallpapers are per user
	if r := do(h, "PUT", "/wallpaper/bg-1.jpg", lan, png1x1); r.Code != 204 {
		t.Fatal(r.Code)
	}
	if r := asB("GET", "/wallpaper/bg-1.jpg", nil); r.Code != 404 {
		t.Errorf("B must not be served A's wallpaper: %d", r.Code)
	}
	if r := asB("DELETE", "/wallpaper/bg-1.jpg", nil); r.Code != 204 { // quietly nothing: it is not B's
		t.Errorf("delete: %d", r.Code)
	}
	g := do(h, "GET", "/wallpaper/bg-1.jpg", lan, nil)
	if g.Code != 200 {
		t.Errorf("B deleted A's wallpaper: %d", g.Code)
	}
	if !strings.Contains(g.Header().Get("Cache-Control"), "private") || !strings.Contains(g.Header().Get("Content-Security-Policy"), "sandbox") {
		t.Errorf("wallpaper headers: %v", g.Header())
	}
	if r := asB("PUT", "/wallpaper/bg-1.jpg", png1x1); r.Code != 204 {
		t.Fatal(r.Code)
	}
	if _, err := os.Stat(filepath.Join(s.userDir(bUser), "wallpaper", "bg-1.jpg")); err != nil {
		t.Error("B's upload must land in B's folder")
	}

	// icons are a shared cache of public logos: one user's upload serves everyone, by design
	do(h, "PUT", "/icons/shared.png", lan, png1x1)
	if r := asB("GET", "/icons/shared.png", nil); r.Code != 200 {
		t.Errorf("icons are shared: %d", r.Code)
	}
}

func TestStatusChecksOnlyTheCallersServices(t *testing.T) {
	upA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer upA.Close()
	upB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer upB.Close()
	s, h, _ := newTest(t)
	_, bCookie := addUser(t, s, "bea", roleUser)

	save := func(cookie, url string) {
		hdr := map[string]string{"Cookie": sessionCookie + "=" + cookie}
		cfg := jbody(map[string]any{"groups": []any{map[string]any{"name": "g", "items": []any{map[string]string{"name": "x", "url": url}}}}})
		if r := doWith(h, "PUT", "/services.json", lan, cfg, hdr); r.Code != 204 {
			t.Fatal(r.Code)
		}
	}
	save(s.sessions.issue(testUser(s)), upA.URL)
	save(bCookie, upB.URL)

	keys := func(cookie string) map[string]check {
		var m map[string]check
		r := doWith(h, "GET", "/api/status", lan, nil, map[string]string{"Cookie": sessionCookie + "=" + cookie})
		if err := json.Unmarshal(r.Body.Bytes(), &m); err != nil {
			t.Fatalf("%v: %s", err, r.Body)
		}
		return m
	}
	a, b := keys(s.sessions.issue(testUser(s))), keys(bCookie)
	if _, ok := a[upA.URL]; !ok || len(a) != 1 {
		t.Errorf("A should see only A's service: %v", a)
	}
	if _, ok := b[upB.URL]; !ok || len(b) != 1 {
		t.Errorf("B should see only B's service: %v", b)
	}
}

func TestOneDashboardPerUser(t *testing.T) {
	s, _, _ := newTest(t)
	a := testUser(s)
	b, _ := addUser(t, s, "bea", roleUser)
	if s.dashboardFor(a) != s.dashboardFor(a) {
		t.Error("one user must always get the same dashboard, because it holds their lock")
	}
	if s.dashboardFor(a) == s.dashboardFor(b) || s.dashboardFor(a).dir == s.dashboardFor(b).dir {
		t.Error("two users must never share a dashboard")
	}
	if !strings.HasPrefix(s.dashboardFor(a).dir, filepath.Join(s.root, "users")+string(filepath.Separator)) {
		t.Errorf("user files belong under ROOT/users: %s", s.dashboardFor(a).dir)
	}
}

func TestFirstAdminAdoptsTheOldSingleUserDashboard(t *testing.T) {
	s := newBare(t)
	if err := s.initAuth(authConfig{iterations: testIterations, life: 1 << 40, setupCode: setupCodeForTests}); err != nil {
		t.Fatal(err)
	}
	h := s.routes()
	old := `{"groups":[{"name":"Media","items":[{"name":"Jellyfin","url":":8096"}]}]}`
	files := map[string]string{
		"services.json":      old,
		"wallpaper/bg-9.jpg": string(png1x1),
		"backups/services-20240101-000000-000.json": `{"groups":[]}`,
	}
	for rel, content := range files {
		p := filepath.Join(s.root, rel)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// a link inside the old folders must not be followed out of them
	secret := filepath.Join(t.TempDir(), "secret.txt")
	_ = os.WriteFile(secret, []byte("do not copy"), 0o644)
	if err := os.Symlink(secret, filepath.Join(s.root, "wallpaper", "link.jpg")); err != nil {
		t.Logf("symlinks unavailable here: %v", err)
	}

	cookie := setupAdmin(t, h, "vedant")
	admin, _ := s.accounts.byName("vedant")
	get := func(p string) *httptest.ResponseRecorder { return send(h, "GET", p, lan, nil, cookie) }

	if r := get("/services.json"); r.Code != 200 || r.Body.String() != old {
		t.Errorf("the admin's dashboard must be the old one: %d %q", r.Code, r.Body)
	}
	if r := get("/wallpaper/bg-9.jpg"); r.Code != 200 {
		t.Errorf("the old wallpaper: %d", r.Code)
	}
	if !strings.Contains(get("/api/backups").Body.String(), "services-20240101-000000-000.json") {
		t.Error("the old backups must come along")
	}
	if _, err := os.Stat(filepath.Join(s.userDir(admin), "wallpaper", "link.jpg")); err == nil {
		t.Error("a symlink must not be followed or copied")
	}
	for rel, content := range files { // nothing was moved or removed
		if got, _ := os.ReadFile(filepath.Join(s.root, rel)); string(got) != content {
			t.Errorf("the original %s must stay untouched", rel)
		}
	}

	// the old files are not served any more, and a second user does not inherit them
	_, bCookie := addUser(t, s, "bea", roleUser)
	if r := send(h, "GET", "/services.json", lan, nil, bCookie); r.Code != 404 {
		t.Errorf("a new user starts empty: %d", r.Code)
	}
}

func TestAdoptNeverOverwrites(t *testing.T) {
	s := newBare(t)
	u := user{ID: "aaaaaaaaaaaaaaaa", Name: "x"}
	dst := filepath.Join(s.userDir(u), "services.json")
	_ = os.MkdirAll(filepath.Dir(dst), 0o755)
	_ = os.WriteFile(dst, []byte("mine"), 0o644)
	_ = os.WriteFile(filepath.Join(s.root, "services.json"), []byte("legacy"), 0o644)
	s.adoptLegacyData(u)
	if got, _ := os.ReadFile(dst); string(got) != "mine" {
		t.Errorf("existing files must win: %q", got)
	}
}

func TestSetupWithNothingToAdoptIsFine(t *testing.T) {
	s := newBare(t)
	_ = s.initAuth(authConfig{iterations: testIterations, life: 1 << 40, setupCode: setupCodeForTests})
	h := s.routes()
	cookie := setupAdmin(t, h, "vedant")
	if r := send(h, "GET", "/services.json", lan, nil, cookie); r.Code != 404 {
		t.Errorf("a fresh install starts with no saved config (the page shows its starter dashboard): %d", r.Code)
	}
}
