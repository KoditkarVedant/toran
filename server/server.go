package main

// The server type, its settings and construction.

import (
	"net/http"
	"net/netip"
	"os"
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

	// accounts and login; all set by initAuth (auth.go)
	accounts   *accounts
	sessions   *sessions // signs login cookies
	limiter    *failureLimiter
	gate       hashGate // caps concurrent password checks
	iterations int      // PBKDF2 iterations for new hashes
	dummyHash  string   // checked when a login names nobody, so timing does not reveal which names exist
	setupMu    sync.Mutex
	setupCode  string // first-run code, empty once an admin exists

	dashMu     sync.Mutex
	dashboards map[string]*dashboard // one per user id, created on first use (dashboard.go)

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
		dashboards: map[string]*dashboard{},
		checkHost:  envOr("CHECK_HOST", "127.0.0.1"), stCache: map[string]check{}, stTTL: 15 * time.Second,
		client: &http.Client{
			Timeout:       2500 * time.Millisecond,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
			Transport:     newCheckTransport(), // see safedial.go
		},
	}
	return s
}

func (s *server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"ok": true, "version": version, "lanOnly": s.lanOnly})
}
