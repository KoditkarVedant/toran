package main

// Static files, served from an allow-list only: the page itself and what it needs before
// anyone has logged in. Everything that belongs to a person (services.json, wallpapers, icons) is
// served by authenticated routes instead (dashboard.go, uploads.go).

import (
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

var staticFiles = map[string]bool{"/index.html": true, "/custom.css": true, "/favicon.ico": true}
var staticDirs = []string{"/wallpapers/"} // the wallpapers that ship with Toran

// imageCSP is sent with every image that was shipped or uploaded: it may be drawn but never run.
const imageCSP = "default-src 'none'; style-src 'unsafe-inline'; sandbox"

func (s *server) static(w http.ResponseWriter, r *http.Request) {
	p := path.Clean("/" + r.URL.Path)
	if p == "/" {
		p = "/index.html"
	}
	dir := false
	for _, d := range staticDirs {
		if strings.HasPrefix(p, d) {
			dir = true
		}
	}
	if (!staticFiles[p] && !dir) || strings.Contains(p, "/.") {
		http.NotFound(w, r)
		return
	}
	f, err := os.Open(filepath.Join(s.root, filepath.FromSlash(p)))
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
	if dir { // bundled images: cacheable, and never allowed to run script even if opened directly
		w.Header().Set("Cache-Control", "public, max-age=86400")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", imageCSP)
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}
	http.ServeContent(w, r, fi.Name(), fi.ModTime(), f)
}
