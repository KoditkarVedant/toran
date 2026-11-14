package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTest(t *testing.T) (*server, http.Handler, string) {
	t.Helper()
	dir := t.TempDir()
	s := newServer(dir)
	s.lanOnly = true
	s.backupEvery = 0 // back up on every change in tests
	return s, s.routes(), dir
}

func do(h http.Handler, method, target, remote string, body []byte) *httptest.ResponseRecorder {
	return doWith(h, method, target, remote, body, nil)
}

// doWith is do plus request headers. The key "Host" sets the request's Host instead.
func doWith(h http.Handler, method, target, remote string, body []byte, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, bytes.NewReader(body))
	req.RemoteAddr = remote
	for k, v := range headers {
		if k == "Host" {
			req.Host = v
		} else {
			req.Header.Set(k, v)
		}
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

const lan = "192.168.1.20:5555"

var png1x1 = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89\x00\x00\x00\rIDATx\x9cc\xf8\xff\xff?\x00\x05\xfe\x02\xfe\xa7\x9a\xa0\xa1\x00\x00\x00\x00IEND\xaeB`\x82")

func TestConfigSaveBackupRestore(t *testing.T) {
	_, h, dir := newTest(t)
	v1 := []byte(`{"groups":[{"name":"A","items":[]}]}`)
	v2 := []byte(`{"groups":[{"name":"B","items":[]}]}`)

	if r := do(h, "PUT", "/services.json", lan, v1); r.Code != 204 {
		t.Fatalf("first save: %d %s", r.Code, r.Body)
	}
	if n := len((&server{root: dir}).backupNames()); n != 0 {
		t.Fatalf("first save has nothing to back up, got %d backups", n)
	}
	time.Sleep(3 * time.Millisecond)
	if r := do(h, "PUT", "/services.json", lan, v2); r.Code != 204 {
		t.Fatalf("second save: %d", r.Code)
	}
	got, _ := os.ReadFile(filepath.Join(dir, "services.json"))
	if !bytes.Equal(got, v2) {
		t.Fatalf("config not updated: %s", got)
	}
	// saving identical content must not create another backup
	do(h, "PUT", "/services.json", lan, v2)
	names := (&server{root: dir}).backupNames()
	if len(names) != 1 {
		t.Fatalf("want 1 backup (the previous version), got %v", names)
	}
	old, _ := os.ReadFile(filepath.Join(dir, "backups", names[0]))
	if !bytes.Equal(old, v1) {
		t.Fatalf("backup should hold the previous config, got %s", old)
	}

	// list + restore
	r := do(h, "GET", "/api/backups", lan, nil)
	var list []struct{ Name, Time string }
	if err := json.Unmarshal(r.Body.Bytes(), &list); err != nil || len(list) != 1 || list[0].Time == "" {
		t.Fatalf("list: %v %s", err, r.Body)
	}
	time.Sleep(3 * time.Millisecond)
	if r := do(h, "POST", "/api/backups/restore", lan, []byte(`{"name":"`+list[0].Name+`"}`)); r.Code != 204 {
		t.Fatalf("restore: %d %s", r.Code, r.Body)
	}
	got, _ = os.ReadFile(filepath.Join(dir, "services.json"))
	if !bytes.Equal(got, v1) {
		t.Fatalf("restore did not bring back v1: %s", got)
	}
	if n := len((&server{root: dir}).backupNames()); n != 2 {
		t.Fatalf("restoring must back up what it replaces; backups=%d", n)
	}
	if r := do(h, "POST", "/api/backups/restore", lan, []byte(`{"name":"../../etc/passwd"}`)); r.Code != 400 {
		t.Fatalf("path in restore name must be rejected, got %d", r.Code)
	}
}

func TestConfigValidationAndLimits(t *testing.T) {
	_, h, _ := newTest(t)
	for name, body := range map[string]string{"not json": "hello", "no groups": `{"x":1}`, "null groups": `{"groups":null}`} {
		if r := do(h, "PUT", "/services.json", lan, []byte(body)); r.Code != 400 {
			t.Errorf("%s: want 400, got %d", name, r.Code)
		}
	}
	big := `{"groups":[],"pad":"` + strings.Repeat("x", 2<<20) + `"}`
	if r := do(h, "PUT", "/services.json", lan, []byte(big)); r.Code != 413 {
		t.Errorf("oversize config: want 413, got %d", r.Code)
	}
}

func TestBackupPruning(t *testing.T) {
	s, h, dir := newTest(t)
	s.maxBackups = 3
	for i := 0; i < 8; i++ {
		body := []byte(`{"groups":[],"n":` + string(rune('0'+i)) + `}`)
		do(h, "PUT", "/services.json", lan, body)
		time.Sleep(3 * time.Millisecond)
	}
	if n := len(s.backupNames()); n != 3 {
		t.Fatalf("want 3 backups kept, got %d (%s)", n, dir)
	}
}

func TestLanOnlyWrites(t *testing.T) {
	s, h, _ := newTest(t)
	body := []byte(`{"groups":[]}`)
	if r := do(h, "PUT", "/services.json", "8.8.8.8:1234", body); r.Code != 403 {
		t.Fatalf("public client must not write, got %d", r.Code)
	}
	if r := do(h, "PUT", "/services.json", "127.0.0.1:1", body); r.Code != 204 {
		t.Fatalf("loopback may write, got %d", r.Code)
	}
	if r := do(h, "PUT", "/services.json", "172.18.0.1:1", body); r.Code != 204 {
		t.Fatalf("docker bridge gateway may write, got %d", r.Code)
	}
	if r := do(h, "PUT", "/icons/a.png", "8.8.8.8:1", png1x1); r.Code != 403 {
		t.Fatalf("public client must not upload, got %d", r.Code)
	}
	s.lanOnly = false
	if r := do(h, "PUT", "/services.json", "8.8.8.8:1234", body); r.Code != 204 {
		t.Fatalf("LAN_ONLY=false should allow, got %d", r.Code)
	}
}

func TestUploads(t *testing.T) {
	_, h, dir := newTest(t)
	if r := do(h, "PUT", "/wallpaper/bg-1.jpg", lan, png1x1); r.Code != 204 {
		t.Fatalf("upload: %d %s", r.Code, r.Body)
	}
	if _, err := os.Stat(filepath.Join(dir, "wallpaper", "bg-1.jpg")); err != nil {
		t.Fatal("file not stored")
	}
	g := do(h, "GET", "/wallpaper/bg-1.jpg", lan, nil)
	if g.Code != 200 || !strings.Contains(g.Header().Get("Content-Security-Policy"), "sandbox") || g.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("uploaded files must be served sandboxed: %d %v", g.Code, g.Header())
	}
	cases := map[string]struct {
		path string
		body []byte
		want int
	}{
		"wrong extension":  {"/wallpaper/x.exe", png1x1, 400},
		"traversal":        {"/wallpaper/..%2f..%2fx.png", png1x1, 400},
		"dotdot":           {"/wallpaper/a..b.png", png1x1, 400},
		"not an image":     {"/wallpaper/x.png", []byte("hello world"), 415},
		"empty":            {"/wallpaper/x.png", nil, 415},
		"svg without svg":  {"/icons/x.svg", []byte("<html></html>"), 415},
		"wallpaper as svg": {"/wallpaper/x.svg", []byte("<svg/>"), 400},
		"oversize icon":    {"/icons/big.png", append(append([]byte{}, png1x1...), make([]byte, 3<<20)...), 413},
		"leading dot name": {"/icons/.hidden.png", png1x1, 400},
	}
	for name, c := range cases {
		if r := do(h, "PUT", c.path, lan, c.body); r.Code != c.want {
			t.Errorf("%s: want %d, got %d (%s)", name, c.want, r.Code, strings.TrimSpace(r.Body.String()))
		}
	}
	if r := do(h, "PUT", "/icons/jellyfin.svg", lan, []byte(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`)); r.Code != 204 {
		t.Errorf("valid svg icon: %d", r.Code)
	}
	if r := do(h, "DELETE", "/wallpaper/bg-1.jpg", lan, nil); r.Code != 204 {
		t.Errorf("delete: %d", r.Code)
	}
	if _, err := os.Stat(filepath.Join(dir, "wallpaper", "bg-1.jpg")); err == nil {
		t.Error("delete did not remove the file")
	}
	if r := do(h, "DELETE", "/wallpaper/..%2f..%2fservices.json", lan, nil); r.Code != 400 && r.Code != 404 {
		t.Errorf("delete traversal must be rejected, got %d", r.Code)
	}
}

func TestStaticAllowList(t *testing.T) {
	_, h, dir := newTest(t)
	write := func(rel, content string) {
		p := filepath.Join(dir, rel)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		_ = os.WriteFile(p, []byte(content), 0o644)
	}
	write("index.html", "<html>hi</html>")
	write("services.json", `{"groups":[]}`)
	write("custom.css", "a{}")
	write("wallpapers/a.jpg", "x")
	write("backups/services-20240101-000000-000.json", `{"groups":[]}`)
	write("bin/toran-x86_64", "binary")
	write("secrets.txt", "nope")
	write(".env", "nope")

	ok := []string{"/", "/index.html", "/services.json", "/custom.css", "/wallpapers/a.jpg"}
	for _, p := range ok {
		if r := do(h, "GET", p, lan, nil); r.Code != 200 {
			t.Errorf("GET %s: want 200, got %d", p, r.Code)
		}
	}
	if r := do(h, "GET", "/index.html", lan, nil); r.Header().Get("Cache-Control") != "no-cache" {
		t.Errorf("html must revalidate, got %q", r.Header().Get("Cache-Control"))
	}
	bad := []string{"/backups/services-20240101-000000-000.json", "/bin/toran-x86_64", "/secrets.txt", "/.env", "/../secrets.txt",
		"/wallpapers/../secrets.txt", "/wallpapers/", "/icons/missing.svg", "/%2e%2e/secrets.txt", "/wallpapers/%2e%2e/%2e%2e/secrets.txt"}
	for _, p := range bad {
		r := do(h, "GET", p, lan, nil)
		if r.Code >= 300 && r.Code < 400 { // ServeMux redirects unclean paths (301 or 307 by Go version); the cleaned target must not serve the file
			r = do(h, "GET", r.Header().Get("Location"), lan, nil)
		}
		if r.Code != 404 {
			t.Errorf("GET %s should end in 404, got %d", p, r.Code)
		}
	}
}

func TestStatusChecks(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(401) })) // 401 still means "alive"
	defer up.Close()
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(502) }))
	defer bad.Close()
	s, h, dir := newTest(t)
	s.checkHost = "127.0.0.1"
	port := func(u string) string { return u[strings.LastIndex(u, ":"):] }
	cfg := map[string]any{"groups": []any{map[string]any{"name": "g", "items": []any{
		map[string]string{"name": "up", "url": port(up.URL)},
		map[string]string{"name": "bad", "url": bad.URL},
		map[string]string{"name": "dead", "url": ":1"},
		map[string]string{"name": "dup", "url": port(up.URL)},
	}}}}
	b, _ := json.Marshal(cfg)
	_ = os.WriteFile(filepath.Join(dir, "services.json"), b, 0o644)

	r := do(h, "GET", "/api/status", lan, nil)
	var res map[string]check
	if err := json.Unmarshal(r.Body.Bytes(), &res); err != nil {
		t.Fatalf("%v: %s", err, r.Body)
	}
	if len(res) != 3 {
		t.Fatalf("duplicate urls are checked once; got %d entries: %v", len(res), res)
	}
	if res[port(up.URL)].State != "up" || res[port(up.URL)].Code != 401 {
		t.Errorf("401 should count as up: %+v", res[port(up.URL)])
	}
	if res[bad.URL].State != "warn" {
		t.Errorf("5xx should be warn: %+v", res[bad.URL])
	}
	if res[":1"].State != "down" {
		t.Errorf("closed port should be down: %+v", res[":1"])
	}
	// second call within the TTL is served from cache (works even if the target goes away)
	up.Close()
	r = do(h, "GET", "/api/status", lan, nil)
	_ = json.Unmarshal(r.Body.Bytes(), &res)
	if res[port(up.URL)].State != "up" {
		t.Errorf("cached result expected within TTL: %+v", res[port(up.URL)])
	}
}

func TestTarget(t *testing.T) {
	s, _, _ := newTest(t)
	cases := map[string]string{
		":8096": "http://127.0.0.1:8096", "8096": "http://127.0.0.1:8096",
		"http://pi:81": "http://pi:81", "https://x.lan/path": "https://x.lan/path", "pi.lan:9000": "http://pi.lan:9000",
	}
	for in, want := range cases {
		if got := s.target(in); got != want {
			t.Errorf("target(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestHealth(t *testing.T) {
	_, h, _ := newTest(t)
	r := do(h, "GET", "/api/health", lan, nil)
	var got map[string]any
	if r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &got) != nil || got["ok"] != true || got["version"] == "" || got["lanOnly"] != true {
		t.Fatalf("health: %d %s", r.Code, r.Body)
	}
}
