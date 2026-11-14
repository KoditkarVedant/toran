// toran: a tiny server for the home dashboard.
//
//   - serves the page and saves its config (a drop-in replacement for nginx + WebDAV)
//   - keeps dated backups of services.json
//   - accepts wallpaper and icon uploads
//   - checks service health from the server side
//
// Settings come from environment variables (see envOr calls). No third-party packages.
//
// Layout: server.go (type + settings), access.go (who may write), proxy.go (visitor address
// behind a proxy), origin.go (cross-site protection), hosts.go (host allowlist), headers.go (security headers), config.go and backups.go (services.json), uploads.go, static.go, status.go,
// httpx.go (helpers).
package main

import (
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", s.health)
	mux.HandleFunc("GET /api/session", s.sessionInfo)
	mux.HandleFunc("POST /api/setup", s.setup)
	mux.HandleFunc("POST /api/login", s.login)
	mux.HandleFunc("POST /api/logout", s.logout)
	mux.HandleFunc("POST /api/password", s.authed(s.changePassword))
	mux.HandleFunc("GET /api/status", s.status)
	mux.HandleFunc("GET /api/backups", s.dash.listBackups)
	mux.HandleFunc("POST /api/backups/restore", s.guard(s.dash.restore))
	mux.HandleFunc("PUT /services.json", s.guard(s.dash.putConfig))
	mux.HandleFunc("PUT /wallpaper/{name}", s.putFile("wallpaper", 12<<20, ".jpg", ".jpeg", ".png", ".webp"))
	mux.HandleFunc("DELETE /wallpaper/{name}", s.delFile("wallpaper"))
	mux.HandleFunc("PUT /icons/{name}", s.putFile("icons", 2<<20, ".svg", ".png", ".webp"))
	mux.HandleFunc("GET /", s.static)
	// Outermost layers, so no route can skip them: the headers go on every answer, including a
	// refusal from the host or cross-site checks.
	return securityHeaders(s.hostGuard(s.origin.Handler(mux)))
}

func main() {
	if runCommand(os.Args[1:], os.Stdin) {
		return
	}
	root, err := filepath.Abs(envOr("ROOT", "."))
	if err != nil {
		log.Fatal(err)
	}
	s := newServer(root)
	authCfg, err := authConfigFromEnv()
	if err != nil {
		log.Fatal(err)
	}
	if err := s.initAuth(authCfg); err != nil {
		log.Fatal(err)
	}
	s.pendingSetupCode() // on a fresh install, puts the first-run code in the log
	if err := s.trustOrigins(envOr("TRUSTED_ORIGINS", "")); err != nil {
		log.Fatal(err)
	}
	if err := s.trustProxies(envOr("TRUSTED_PROXIES", "")); err != nil {
		log.Fatal(err)
	}
	if err := s.allowHosts(envOr("ALLOWED_HOSTS", "")); err != nil {
		log.Fatal(err)
	}
	addr := envOr("ADDR", ":8080")
	srv := &http.Server{
		Addr: addr, Handler: s.routes(),
		ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 2 * time.Minute, WriteTimeout: time.Minute, IdleTimeout: time.Minute,
	}
	log.Printf("toran %s on %s, root=%s, lan-only-writes=%v", version, addr, root, s.lanOnly)
	log.Fatal(srv.ListenAndServe())
}
