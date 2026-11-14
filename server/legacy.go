package main

// Taking over a single-user installation.
//
// Before accounts existed, Toran kept one dashboard directly in ROOT: services.json, wallpaper/
// and backups/. When the first admin is created, those files are copied into that admin's own
// folder, so upgrading does not lose anything. Nothing is moved or deleted: the old files stay
// where they are (and are no longer served), and can be removed by hand once everything works.

import (
	"io"
	"log"
	"os"
	"path/filepath"
)

// adoptLegacyData copies the pre-accounts dashboard into the user's folder. Failures are logged
// and skipped: the original files are untouched, so the admin can still be given them by hand.
func (s *server) adoptLegacyData(u user) {
	dst := s.userDir(u)
	copied := 0
	for _, rel := range []string{"services.json", "wallpaper", "backups"} {
		n, err := copyTree(filepath.Join(s.root, rel), filepath.Join(dst, rel))
		if err != nil {
			log.Printf("copying %s for %q: %v (the original is untouched)", rel, u.Name, err)
		}
		copied += n
	}
	if copied > 0 {
		log.Printf("copied %d file(s) of the earlier single-user dashboard to %s; the originals in %s were left in place", copied, dst, s.root)
	}
}

// copyTree copies a file, or the regular files directly inside a folder, and returns how many.
// A missing source is not an error. It never follows or recreates symlinks and never overwrites.
func copyTree(src, dst string) (int, error) {
	fi, err := os.Lstat(src)
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	switch {
	case fi.Mode().IsRegular():
		return copyFile(src, dst)
	case fi.IsDir():
		es, err := os.ReadDir(src)
		if err != nil {
			return 0, err
		}
		total := 0
		for _, e := range es {
			if !e.Type().IsRegular() {
				continue
			}
			n, err := copyFile(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name()))
			total += n
			if err != nil {
				return total, err
			}
		}
		return total, nil
	}
	return 0, nil // a symlink or something else: not ours to follow
}

func copyFile(src, dst string) (int, error) {
	if _, err := os.Lstat(dst); err == nil {
		return 0, nil // already there: never overwrite
	}
	in, err := os.Open(src)
	if err != nil {
		return 0, err
	}
	defer in.Close()
	b, err := io.ReadAll(in)
	if err != nil {
		return 0, err
	}
	if err := atomicWrite(dst, b); err != nil {
		return 0, err
	}
	return 1, nil
}
