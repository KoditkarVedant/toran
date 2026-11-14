package main

// The server type, its settings and construction.

import (
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// version is the release number. Release builds override it: -ldflags "-X main.version=1.2.3".
var version = "0.1.0"

type check struct {
	State string `json:"state"` // up | warn | down
	Code  int    `json:"code,omitempty"`
	MS    int    `json:"ms"`
	at    time.Time
}

type server struct {
	root        string                      // folder holding index.html, services.json, uploads, backups...
	lanOnly     bool                        // only private-network clients may write
	origin      *http.CrossOriginProtection // rejects writes steered by another website
	proxies     []netip.Prefix              // reverse proxies whose X-Forwarded-For is believed (TRUSTED_PROXIES)
	hosts       map[string]bool             // host names Toran answers to (ALLOWED_HOSTS); nil = any
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
		root: root, lanOnly: envOr("LAN_ONLY", "true") != "false", origin: newOriginGuard(),
		maxBackups: 30, backupEvery: 10 * time.Minute,
		checkHost: envOr("CHECK_HOST", "127.0.0.1"), stCache: map[string]check{}, stTTL: 15 * time.Second,
		client: &http.Client{
			Timeout:       2500 * time.Millisecond,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
			Transport:     newCheckTransport(), // see safedial.go
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

func (s *server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"ok": true, "version": version, "lanOnly": s.lanOnly})
}
