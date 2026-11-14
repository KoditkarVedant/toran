package main

// Dated backups of a dashboard's services.json: create, list, restore.

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"
)

var backupName = regexp.MustCompile(`^services-\d{8}-\d{6}-\d{3}\.json$`)

// backupLocked stores the given (previous) content, at most once per backupEvery unless forced.
// The caller holds d.mu.
func (d *dashboard) backupLocked(content []byte, force bool) {
	if !force && time.Since(d.lastBackup) < d.s.backupEvery {
		return
	}
	t := time.Now().UTC()
	name := fmt.Sprintf("services-%s-%03d.json", t.Format("20060102-150405"), t.Nanosecond()/1e6)
	if err := atomicWrite(filepath.Join(d.backupDir(), name), content); err != nil {
		log.Printf("backup: %v", err)
		return
	}
	d.lastBackup = time.Now()
	names := d.backupNames()
	for i := d.s.maxBackups; i < len(names); i++ {
		_ = os.Remove(filepath.Join(d.backupDir(), names[i]))
	}
}

// newest first
func (d *dashboard) backupNames() []string {
	es, _ := os.ReadDir(d.backupDir())
	var names []string
	for _, e := range es {
		if backupName.MatchString(e.Name()) {
			names = append(names, e.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	return names
}

func (d *dashboard) listBackups(w http.ResponseWriter, r *http.Request) {
	type item struct {
		Name string `json:"name"`
		Time string `json:"time"`
		Size int64  `json:"size"`
	}
	out := []item{}
	for _, n := range d.backupNames() {
		t, err := time.Parse("20060102-150405", n[len("services-"):len("services-")+15])
		fi, ferr := os.Stat(filepath.Join(d.backupDir(), n))
		if err != nil || ferr != nil {
			continue
		}
		out = append(out, item{n, t.UTC().Format(time.RFC3339), fi.Size()})
	}
	writeJSON(w, 200, out)
}

func (d *dashboard) restore(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req) != nil || !backupName.MatchString(req.Name) {
		http.Error(w, "bad backup name", http.StatusBadRequest)
		return
	}
	data, err := os.ReadFile(filepath.Join(d.backupDir(), req.Name))
	if err != nil || !validConfig(data) {
		http.Error(w, "backup not found or not valid", http.StatusNotFound)
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if cur, err := os.ReadFile(d.configPath()); err == nil {
		d.backupLocked(cur, true) // never lose what is being replaced
	}
	if err := atomicWrite(d.configPath(), data); err != nil {
		http.Error(w, "could not restore", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
