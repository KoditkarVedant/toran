// toran: a tiny server for the home dashboard.
//
//   - serves the page and saves its config (a drop-in replacement for nginx + WebDAV)
//   - keeps dated backups of services.json
//   - accepts wallpaper and icon uploads
//   - checks service health from the server side
//
// Settings come from environment variables (see envOr calls). No third-party packages.
//
// Layout: server.go (type + settings), access.go (who may write), config.go and
// backups.go (services.json), uploads.go, static.go, status.go, httpx.go (helpers).
package main

import (
	"log"
	"net/http"
	"path/filepath"
	"time"
)

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
