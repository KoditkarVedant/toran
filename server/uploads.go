package main

// Wallpaper and icon uploads, and serving them back.
//
// Wallpapers belong to one user (their dashboard's wallpaper/ folder). Icons are a shared cache
// of public logos, so any logged-in user may add one and all of them see it.

import (
	"bytes"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var fileName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,80}$`)

func looksLikeImage(b []byte, name string) bool {
	if strings.HasSuffix(strings.ToLower(name), ".svg") {
		head := b
		if len(head) > 2048 {
			head = head[:2048]
		}
		return bytes.Contains(bytes.ToLower(head), []byte("<svg"))
	}
	return strings.HasPrefix(http.DetectContentType(b), "image/")
}

func (s *server) iconDir() string { return filepath.Join(s.root, "icons") }

// wallpaperDirOf and iconDirOf say where a user's uploads of each kind go.
func (s *server) wallpaperDirOf(u user) string { return s.dashboardFor(u).wallpaperDir() }
func (s *server) iconDirOf(user) string        { return s.iconDir() }

// putFile stores an uploaded image in the folder dir chooses for the user.
func (s *server) putFile(dir func(user) string, max int64, exts ...string) http.HandlerFunc {
	return s.guard(s.authed(func(w http.ResponseWriter, r *http.Request, u user) {
		name := r.PathValue("name")
		ok := fileName.MatchString(name) && !strings.Contains(name, "..")
		extOK := false
		for _, e := range exts {
			if strings.HasSuffix(strings.ToLower(name), e) {
				extOK = true
			}
		}
		if !ok || !extOK {
			http.Error(w, "bad file name or type", http.StatusBadRequest)
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, max))
		if err != nil {
			http.Error(w, "file too large", http.StatusRequestEntityTooLarge)
			return
		}
		if len(body) == 0 || !looksLikeImage(body, name) {
			http.Error(w, "that does not look like an image", http.StatusUnsupportedMediaType)
			return
		}
		if err := atomicWrite(filepath.Join(dir(u), name), body); err != nil {
			log.Printf("write upload %s: %v", name, err)
			http.Error(w, "could not store the file", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
}

func (s *server) delFile(dir func(user) string) http.HandlerFunc {
	return s.guard(s.authed(func(w http.ResponseWriter, r *http.Request, u user) {
		name := r.PathValue("name")
		if !fileName.MatchString(name) || strings.Contains(name, "..") {
			http.Error(w, "bad file name", http.StatusBadRequest)
			return
		}
		_ = os.Remove(filepath.Join(dir(u), name))
		w.WriteHeader(http.StatusNoContent)
	}))
}

// getFile serves an uploaded image from the folder dir chooses for the user. Uploaded files get a
// policy that lets them show as pictures but never run script, even if opened directly.
func (s *server) getFile(dir func(user) string) http.HandlerFunc {
	return s.authed(func(w http.ResponseWriter, r *http.Request, u user) {
		name := r.PathValue("name")
		if !fileName.MatchString(name) || strings.Contains(name, "..") {
			http.NotFound(w, r)
			return
		}
		f, err := os.Open(filepath.Join(dir(u), name))
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
		w.Header().Set("Cache-Control", "private, max-age=86400") // private: wallpapers are not for shared caches
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", imageCSP)
		http.ServeContent(w, r, fi.Name(), fi.ModTime(), f)
	})
}
