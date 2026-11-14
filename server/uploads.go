package main

// Wallpaper and icon uploads.

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

func (s *server) putFile(dir string, max int64, exts ...string) http.HandlerFunc {
	return s.guard(func(w http.ResponseWriter, r *http.Request) {
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
		if err := atomicWrite(filepath.Join(s.root, dir, name), body); err != nil {
			log.Printf("write %s/%s: %v", dir, name, err)
			http.Error(w, "could not store the file", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

func (s *server) delFile(dir string) http.HandlerFunc {
	return s.guard(func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		if !fileName.MatchString(name) || strings.Contains(name, "..") {
			http.Error(w, "bad file name", http.StatusBadRequest)
			return
		}
		_ = os.Remove(filepath.Join(s.root, dir, name))
		w.WriteHeader(http.StatusNoContent)
	})
}
