package main

// A dashboard is one person's data: services.json, its dated backups and its lock.
//
// Keeping this in its own type means everything that touches a config file (saving,
// backups, restoring, the status checker) works on "a dashboard" and never on a hard-coded
// folder, so the same code serves one shared dashboard today and one per user later.

import (
	"os"
	"path/filepath"
	"sync"
	"time"
)

type dashboard struct {
	s   *server // backup policy (how many, how often) is a server-wide setting
	dir string  // folder holding services.json and backups/

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

func (d *dashboard) configPath() string { return filepath.Join(d.dir, "services.json") }
func (d *dashboard) backupDir() string  { return filepath.Join(d.dir, "backups") }
