// toran: a tiny server for the home dashboard.
//
//   - serves the page and saves its config (a drop-in replacement for nginx + WebDAV)
//   - keeps dated backups of services.json
//   - accepts wallpaper and icon uploads
//   - checks service health from the server side
//
// Settings come from environment variables (see envOr calls in main). No third-party packages.
package main

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

const version = "1.0"

type check struct {
	State string `json:"state"` // up | warn | down
	Code  int    `json:"code,omitempty"`
	MS    int    `json:"ms"`
	at    time.Time
}

type server struct {
	root        string // folder holding index.html, services.json, uploads, backups...
	lanOnly     bool   // only private-network clients may write
	maxBackups  int
	backupEvery time.Duration

	mu         sync.Mutex // guards services.json writes and backups
	lastBackup time.Time

	checkHost string // host used for bare-port status checks (127.0.0.1 with host networking)
	stMu      sync.Mutex
	stCache   map[string]check
	stTTL     time.Duration
	client    *http.Client
}

func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func newServer(root string) *server {
	s := &server{
		root: root, lanOnly: envOr("LAN_ONLY", "true") != "false",
		maxBackups: 30, backupEvery: 10 * time.Minute,
		checkHost: envOr("CHECK_HOST", "127.0.0.1"), stCache: map[string]check{}, stTTL: 15 * time.Second,
		client: &http.Client{
			Timeout:       2500 * time.Millisecond,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
			Transport:     &http.Transport{DisableKeepAlives: true, TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}, // LAN self-signed certs
		},
	}
	// remember when the newest backup was made so a restart doesn't trigger an extra one
	if es, err := os.ReadDir(filepath.Join(root, "backups")); err == nil {
		for _, e := range es {
			if fi, err := e.Info(); err == nil && fi.ModTime().After(s.lastBackup) {
				s.lastBackup = fi.ModTime()
			}
		}
	}
	return s
}

/* ------------------------------------------------------------------ helpers */

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func atomicWrite(dst string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err = tmp.Write(data); err == nil {
		err = tmp.Sync()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	_ = os.Chmod(tmp.Name(), 0o644)
	return os.Rename(tmp.Name(), dst)
}

// Writes are limited to the local network (loopback + private ranges) unless LAN_ONLY=false.
func (s *server) clientAllowed(r *http.Request) bool {
	if !s.lanOnly {
		return true
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	return ip != nil && (ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast())
}

func (s *server) guard(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.clientAllowed(r) {
			http.Error(w, "writes are limited to the local network", http.StatusForbidden)
			return
		}
		h(w, r)
	}
}

/* ------------------------------------------------------------------ config + backups */

var backupName = regexp.MustCompile(`^services-\d{8}-\d{6}-\d{3}\.json$`)

func (s *server) configPath() string { return filepath.Join(s.root, "services.json") }

func validConfig(b []byte) bool {
	var probe struct {
		Groups []json.RawMessage `json:"groups"`
	}
	return json.Unmarshal(b, &probe) == nil && probe.Groups != nil
}

func (s *server) putConfig(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		http.Error(w, "config too large or unreadable", http.StatusRequestEntityTooLarge)
		return
	}
	if !validConfig(body) {
		http.Error(w, "expected a JSON object with a groups array", http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if old, err := os.ReadFile(s.configPath()); err == nil && !bytes.Equal(old, body) {
		s.backupLocked(old, false)
	}
	if err := atomicWrite(s.configPath(), body); err != nil {
		log.Printf("write services.json: %v", err)
		http.Error(w, "could not write the config on the server", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// backupLocked stores the given (previous) content, at most once per backupEvery unless forced.
func (s *server) backupLocked(content []byte, force bool) {
	if !force && time.Since(s.lastBackup) < s.backupEvery {
		return
	}
	t := time.Now().UTC()
	name := fmt.Sprintf("services-%s-%03d.json", t.Format("20060102-150405"), t.Nanosecond()/1e6)
	if err := atomicWrite(filepath.Join(s.root, "backups", name), content); err != nil {
		log.Printf("backup: %v", err)
		return
	}
	s.lastBackup = time.Now()
	names := s.backupNames()
	for i := s.maxBackups; i < len(names); i++ {
		_ = os.Remove(filepath.Join(s.root, "backups", names[i]))
	}
}

// newest first
func (s *server) backupNames() []string {
	es, _ := os.ReadDir(filepath.Join(s.root, "backups"))
	var names []string
	for _, e := range es {
		if backupName.MatchString(e.Name()) {
			names = append(names, e.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	return names
}

func (s *server) listBackups(w http.ResponseWriter, r *http.Request) {
	type item struct {
		Name string `json:"name"`
		Time string `json:"time"`
		Size int64  `json:"size"`
	}
	out := []item{}
	for _, n := range s.backupNames() {
		t, err := time.Parse("20060102-150405", n[len("services-"):len("services-")+15])
		fi, ferr := os.Stat(filepath.Join(s.root, "backups", n))
		if err != nil || ferr != nil {
			continue
		}
		out = append(out, item{n, t.UTC().Format(time.RFC3339), fi.Size()})
	}
	writeJSON(w, 200, out)
}

func (s *server) restore(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req) != nil || !backupName.MatchString(req.Name) {
		http.Error(w, "bad backup name", http.StatusBadRequest)
		return
	}
	data, err := os.ReadFile(filepath.Join(s.root, "backups", req.Name))
	if err != nil || !validConfig(data) {
		http.Error(w, "backup not found or not valid", http.StatusNotFound)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if cur, err := os.ReadFile(s.configPath()); err == nil {
		s.backupLocked(cur, true) // never lose what is being replaced
	}
	if err := atomicWrite(s.configPath(), data); err != nil {
		http.Error(w, "could not restore", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

/* ------------------------------------------------------------------ uploads (wallpapers, icons) */

var fileName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,80}$`)

func looksLikeImage(b []byte, name string) bool {
	if strings.HasSuffix(strings.ToLower(name), ".svg") {
		head := b
		if len(head) > 2048 {
			head = head[:2048]
		}
		return bytes.Contains(bytes.ToLower(head), []byte("<svg"))
	}
	return strings.HasPrefix(http.DetectContentType(b), "image/")
}

func (s *server) putFile(dir string, max int64, exts ...string) http.HandlerFunc {
	return s.guard(func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		ok := fileName.MatchString(name) && !strings.Contains(name, "..")
		extOK := false
		for _, e := range exts {
			if strings.HasSuffix(strings.ToLower(name), e) {
				extOK = true
			}
		}
		if !ok || !extOK {
			http.Error(w, "bad file name or type", http.StatusBadRequest)
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, max))
		if err != nil {
			http.Error(w, "file too large", http.StatusRequestEntityTooLarge)
			return
		}
		if len(body) == 0 || !looksLikeImage(body, name) {
			http.Error(w, "that does not look like an image", http.StatusUnsupportedMediaType)
			return
		}
		if err := atomicWrite(filepath.Join(s.root, dir, name), body); err != nil {
			log.Printf("write %s/%s: %v", dir, name, err)
			http.Error(w, "could not store the file", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

func (s *server) delFile(dir string) http.HandlerFunc {
	return s.guard(func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		if !fileName.MatchString(name) || strings.Contains(name, "..") {
			http.Error(w, "bad file name", http.StatusBadRequest)
			return
		}
		_ = os.Remove(filepath.Join(s.root, dir, name))
		w.WriteHeader(http.StatusNoContent)
	})
}

/* ------------------------------------------------------------------ static files (allow-list only) */

var staticFiles = map[string]bool{"/index.html": true, "/custom.css": true, "/services.json": true, "/favicon.ico": true}
var staticDirs = []string{"/wallpapers/", "/wallpaper/", "/icons/"}

func (s *server) static(w http.ResponseWriter, r *http.Request) {
	p := path.Clean("/" + r.URL.Path)
	if p == "/" {
		p = "/index.html"
	}
	dir := false
	for _, d := range staticDirs {
		if strings.HasPrefix(p, d) {
			dir = true
		}
	}
	if (!staticFiles[p] && !dir) || strings.Contains(p, "/.") {
		http.NotFound(w, r)
		return
	}
	f, err := os.Open(filepath.Join(s.root, filepath.FromSlash(p)))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil || fi.IsDir() {
		http.NotFound(w, r)
		return
	}
	if dir { // uploaded or bundled images: cacheable, and never allowed to run script even if opened directly
		w.Header().Set("Cache-Control", "public, max-age=86400")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; sandbox")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}
	http.ServeContent(w, r, fi.Name(), fi.ModTime(), f)
}

/* ------------------------------------------------------------------ server-side status checks */

var portOnly = regexp.MustCompile(`^:?(\d{1,5})$`)

func (s *server) target(u string) string {
	u = strings.TrimSpace(u)
	if m := portOnly.FindStringSubmatch(u); m != nil {
		return "http://" + net.JoinHostPort(s.checkHost, m[1])
	}
	if !strings.Contains(u, "://") {
		return "http://" + u
	}
	return u
}

func (s *server) configURLs() []string {
	b, err := os.ReadFile(s.configPath())
	if err != nil {
		return nil
	}
	var c struct {
		Groups []struct {
			Items []struct {
				URL string `json:"url"`
			} `json:"items"`
		} `json:"groups"`
	}
	if json.Unmarshal(b, &c) != nil {
		return nil
	}
	seen, out := map[string]bool{}, []string{}
	for _, g := range c.Groups {
		for _, i := range g.Items {
			if i.URL != "" && !seen[i.URL] {
				seen[i.URL] = true
				out = append(out, i.URL)
			}
		}
	}
	return out
}

func (s *server) checkOne(u string) check {
	start := time.Now()
	req, err := http.NewRequest(http.MethodGet, s.target(u), nil)
	if err != nil {
		return check{State: "down"}
	}
	req.Header.Set("User-Agent", "toran-status/"+version)
	resp, err := s.client.Do(req)
	ms := int(time.Since(start) / time.Millisecond)
	if err != nil {
		return check{State: "down", MS: ms}
	}
	_, _ = io.CopyN(io.Discard, resp.Body, 4096)
	resp.Body.Close()
	state := "up"
	if resp.StatusCode >= 500 {
		state = "warn"
	}
	return check{State: state, Code: resp.StatusCode, MS: ms}
}

func (s *server) status(w http.ResponseWriter, r *http.Request) {
	urls := s.configURLs()
	out := make(map[string]check, len(urls))
	var stale []string
	s.stMu.Lock()
	for _, u := range urls {
		if c, ok := s.stCache[u]; ok && time.Since(c.at) < s.stTTL {
			out[u] = c
		} else {
			stale = append(stale, u)
		}
	}
	s.stMu.Unlock()

	var wg sync.WaitGroup
	var mu sync.Mutex
	sem := make(chan struct{}, 8)
	for _, u := range stale {
		wg.Add(1)
		go func(u string) {
			defer wg.Done()
			sem <- struct{}{}
			c := s.checkOne(u)
			<-sem
			c.at = time.Now()
			mu.Lock()
			out[u] = c
			mu.Unlock()
		}(u)
	}
	wg.Wait()
	s.stMu.Lock()
	for _, u := range stale {
		s.stCache[u] = out[u]
	}
	s.stMu.Unlock()
	writeJSON(w, 200, out)
}

func (s *server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"ok": true, "version": version, "lanOnly": s.lanOnly})
}

/* ------------------------------------------------------------------ wiring */

func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", s.health)
	mux.HandleFunc("GET /api/status", s.status)
	mux.HandleFunc("GET /api/backups", s.listBackups)
	mux.HandleFunc("POST /api/backups/restore", s.guard(s.restore))
	mux.HandleFunc("PUT /services.json", s.guard(s.putConfig))
	mux.HandleFunc("PUT /wallpaper/{name}", s.putFile("wallpaper", 12<<20, ".jpg", ".jpeg", ".png", ".webp"))
	mux.HandleFunc("DELETE /wallpaper/{name}", s.delFile("wallpaper"))
	mux.HandleFunc("PUT /icons/{name}", s.putFile("icons", 2<<20, ".svg", ".png", ".webp"))
	mux.HandleFunc("GET /", s.static)
	return mux
}

func main() {
	root, err := filepath.Abs(envOr("ROOT", "."))
	if err != nil {
		log.Fatal(err)
	}
	s := newServer(root)
	addr := envOr("ADDR", ":8080")
	srv := &http.Server{
		Addr: addr, Handler: s.routes(),
		ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 2 * time.Minute, WriteTimeout: time.Minute, IdleTimeout: time.Minute,
	}
	log.Printf("toran %s on %s, root=%s, lan-only-writes=%v", version, addr, root, s.lanOnly)
	log.Fatal(srv.ListenAndServe())
}
