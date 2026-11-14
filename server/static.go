package main

// Static files, served from an allow-list only.

import (
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

var staticFiles = map[string]bool{"/index.html": true, "/custom.css": true, "/services.json": true, "/favicon.ico": true}
var staticDirs = []string{"/wallpapers/", "/wallpaper/", "/icons/"}

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
	if dir { // uploaded or bundled images: cacheable, and never allowed to run script even if opened directly
		w.Header().Set("Cache-Control", "public, max-age=86400")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; sandbox")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}
	http.ServeContent(w, r, fi.Name(), fi.ModTime(), f)
}
