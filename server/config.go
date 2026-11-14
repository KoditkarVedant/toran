package main

// The page's config file (services.json): reading paths, validation, saving.

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
)

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
