package main

// Password hashing and the password rules.
//
// Passwords are stored as PBKDF2-HMAC-SHA256 (the standard library's crypto/pbkdf2) with a
// random salt per password. The number of iterations is written into each stored hash,
// so it can be raised later: a login with an older, weaker hash succeeds and the caller
// stores a fresh one. The stored form is
//
//	pbkdf2-sha256$<iterations>$<salt, base64>$<hash, base64>
//
// PBKDF2 needs no memory, which suits a small server; argon2id would be stronger but wants
// about 19 MiB per attempt and a third-party package.

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

const (
	hashScheme        = "pbkdf2-sha256"
	defaultIterations = 600_000 // OWASP's figure for PBKDF2-HMAC-SHA256
	maxIterations     = 10_000_000
	saltBytes         = 16
	keyBytes          = 32

	minPassword = 10
	maxPassword = 256 // bytes; a cap on the work one request can cause
)

var b64 = base64.RawStdEncoding

// hashPassword returns the stored form of a password.
func hashPassword(password string, iterations int) (string, error) {
	salt := make([]byte, saltBytes)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key, err := pbkdf2.Key(sha256.New, password, salt, iterations, keyBytes)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s$%d$%s$%s", hashScheme, iterations, b64.EncodeToString(salt), b64.EncodeToString(key)), nil
}

// verifyPassword checks a password against a stored hash. upgrade is true when it matched but
// the stored hash uses fewer iterations than wantIterations, so the caller should store a new
// one. A malformed hash never matches.
func verifyPassword(password, stored string, wantIterations int) (ok, upgrade bool) {
	if len(password) > maxPassword {
		return false, false
	}
	parts := strings.Split(stored, "$")
	if len(parts) != 4 || parts[0] != hashScheme {
		return false, false
	}
	iterations, err := strconv.Atoi(parts[1])
	if err != nil || iterations < 1 || iterations > maxIterations {
		return false, false // also stops a tampered file from making a login take forever
	}
	salt, err1 := b64.DecodeString(parts[2])
	want, err2 := b64.DecodeString(parts[3])
	if err1 != nil || err2 != nil || len(salt) == 0 || len(want) != keyBytes {
		return false, false
	}
	got, err := pbkdf2.Key(sha256.New, password, salt, iterations, keyBytes)
	if err != nil || subtle.ConstantTimeCompare(got, want) != 1 {
		return false, false
	}
	return true, iterations < wantIterations
}

// checkPassword applies the password rules: a sensible length and not the user's name. There
// are no "one capital letter and a symbol" rules; length is what matters.
func checkPassword(name, password string) error {
	switch {
	case len(password) < minPassword:
		return fmt.Errorf("the password must be at least %d characters", minPassword)
	case len(password) > maxPassword:
		return fmt.Errorf("the password must be at most %d characters", maxPassword)
	case strings.EqualFold(password, name):
		return errors.New("the password must not be the same as the user name")
	}
	return nil
}
