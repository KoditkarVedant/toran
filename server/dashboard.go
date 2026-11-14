package main

// A dashboard is one person's data: services.json, its dated backups, its wallpapers and
// its lock. Each user has their own, in <ROOT>/users/<user id>/.
//
// Keeping this in its own type means everything that touches a config file (saving,
// backups, restoring, the status checker) works on "a dashboard" and never on a hard-coded
// folder. The routes that need one are wrapped with forUser, which finds the logged-in user's
// dashboard, so a handler cannot reach another person's files by mistake.

import (
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type dashboard struct {
	s   *server // backup policy (how many, how often) is a server-wide setting
	dir string  // folder holding services.json, backups/ and wallpaper/

	mu         sync.Mutex // guards services.json writes and backups
	lastBackup time.Time
}

// newDashboard opens the dashboard stored in dir. It remembers when the newest backup was
// made so that a restart does not trigger an extra one.
func (s *server) newDashboard(dir string) *dashboard {
	d := &dashboard{s: s, dir: dir}
	if es, err := os.ReadDir(filepath.Join(dir, "backups")); err == nil {
		for _, e := range es {
			if fi, err := e.Info(); err == nil && fi.ModTime().After(d.lastBackup) {
				d.lastBackup = fi.ModTime()
			}
		}
	}
	return d
}

func (d *dashboard) configPath() string   { return filepath.Join(d.dir, "services.json") }
func (d *dashboard) backupDir() string    { return filepath.Join(d.dir, "backups") }
func (d *dashboard) wallpaperDir() string { return filepath.Join(d.dir, "wallpaper") }

// userDir is where a user's files live. The id comes from the accounts store (random hex), never
// from a request, so it cannot contain a path separator.
func (s *server) userDir(u user) string { return filepath.Join(s.root, "users", u.ID) }

// dashboardFor returns the user's dashboard, opening it on first use. Every request of one user
// must share one value, because it carries the lock that serialises their saves.
func (s *server) dashboardFor(u user) *dashboard {
	s.dashMu.Lock()
	defer s.dashMu.Unlock()
	d, ok := s.dashboards[u.ID]
	if !ok {
		d = s.newDashboard(s.userDir(u))
		s.dashboards[u.ID] = d
	}
	return d
}

// forUser turns a dashboard method into a route that needs a logged-in user and works on that
// user's own dashboard: s.forUser((*dashboard).putConfig).
func (s *server) forUser(f func(*dashboard, http.ResponseWriter, *http.Request)) http.HandlerFunc {
	return s.authed(func(w http.ResponseWriter, r *http.Request, u user) {
		f(s.dashboardFor(u), w, r)
	})
}

// getConfig serves the user's services.json. A user who has not saved anything yet gets a 404,
// and the page then shows its built-in starter dashboard.
func (d *dashboard) getConfig(w http.ResponseWriter, r *http.Request) {
	f, err := os.Open(d.configPath())
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
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "private, no-cache") // revalidate, and never keep it in a shared cache
	http.ServeContent(w, r, fi.Name(), fi.ModTime(), f)
}
