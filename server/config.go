package main

// The page's config file (services.json): reading paths, validation, saving.

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
)

func validConfig(b []byte) bool {
	var probe struct {
		Groups []json.RawMessage `json:"groups"`
	}
	return json.Unmarshal(b, &probe) == nil && probe.Groups != nil
}

func (d *dashboard) putConfig(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		http.Error(w, "config too large or unreadable", http.StatusRequestEntityTooLarge)
		return
	}
	if !validConfig(body) {
		http.Error(w, "expected a JSON object with a groups array", http.StatusBadRequest)
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if old, err := os.ReadFile(d.configPath()); err == nil && !bytes.Equal(old, body) {
		d.backupLocked(old, false)
	}
	if err := atomicWrite(d.configPath(), body); err != nil {
		log.Printf("write services.json: %v", err)
		http.Error(w, "could not write the config on the server", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
