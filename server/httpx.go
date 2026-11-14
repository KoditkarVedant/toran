package main

// Small helpers shared by the handlers.

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// splitList splits a comma-separated setting, dropping spaces and empty entries.
func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// atomicWrite replaces dst in one step, readable by everyone (page config, uploads, backups).
func atomicWrite(dst string, data []byte) error { return atomicWriteMode(dst, data, 0o644) }

// atomicWriteMode is atomicWrite with the file's permissions chosen by the caller: secrets
// (password hashes, the session key) use 0o600 so other users on the machine cannot read them.
// The file is written to a temporary name and renamed, so a crash never leaves half a file.
func atomicWriteMode(dst string, data []byte, mode os.FileMode) error {
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
	_ = os.Chmod(tmp.Name(), mode)
	return os.Rename(tmp.Name(), dst)
}
