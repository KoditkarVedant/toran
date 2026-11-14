package main

// Command-line helpers, run beside the server with the same ROOT:
//
//	toran reset-password NAME     reads the new password from standard input
//
// This is the way back in when an admin has forgotten their password. It keeps the account (and
// so its dashboard), ends its logins everywhere and re-enables it if it was disabled. A running
// server notices the change within a second.

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// authConfigFromEnv reads PBKDF2_ITERATIONS, SESSION_DAYS and SETUP_CODE.
func authConfigFromEnv() (authConfig, error) {
	cfg := defaultAuthConfig()
	if v := os.Getenv("PBKDF2_ITERATIONS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1000 || n > maxIterations {
			return cfg, fmt.Errorf("PBKDF2_ITERATIONS must be a number from 1000 to %d, got %q", maxIterations, v)
		}
		cfg.iterations = n
	}
	if v := os.Getenv("SESSION_DAYS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 365 {
			return cfg, fmt.Errorf("SESSION_DAYS must be a number from 1 to 365, got %q", v)
		}
		cfg.life = time.Duration(n) * 24 * time.Hour
	}
	cfg.setupCode = os.Getenv("SETUP_CODE")
	if cfg.setupCode != "" && len(normalizeCode(cfg.setupCode)) < 8 {
		return cfg, fmt.Errorf("SETUP_CODE must be at least 8 characters")
	}
	return cfg, nil
}

// resetPassword sets a new password for a user, in the users file under root.
func resetPassword(root, name, password string, iterations int) error {
	name = normalizeName(name)
	if err := checkPassword(name, password); err != nil {
		return err
	}
	a, err := openAccounts(filepath.Join(root, "users.json"))
	if err != nil {
		return err
	}
	u, ok := a.byName(name)
	if !ok {
		return fmt.Errorf("there is no user %q", name)
	}
	hash, err := hashPassword(password, iterations)
	if err != nil {
		return err
	}
	_, err = a.update(u.ID, func(x *user) error {
		x.Hash, x.Disabled = hash, false
		x.TokenVersion++
		return nil
	})
	return err
}

// runCommand handles `toran <command> ...`. It reports whether it did, so main knows not to start the server.
func runCommand(args []string, stdin io.Reader) (handled bool) {
	if len(args) == 0 {
		return false
	}
	switch args[0] {
	case "reset-password":
		if len(args) != 2 {
			log.Fatal("usage: toran reset-password NAME   (the new password is read from standard input)")
		}
		root, err := filepath.Abs(envOr("ROOT", "."))
		if err != nil {
			log.Fatal(err)
		}
		cfg, err := authConfigFromEnv()
		if err != nil {
			log.Fatal(err)
		}
		line, _ := bufio.NewReader(stdin).ReadString('\n')
		if err := resetPassword(root, args[1], strings.TrimRight(line, "\r\n"), cfg.iterations); err != nil {
			log.Fatal(err)
		}
		fmt.Printf("password of %q changed; every login of that user has ended\n", normalizeName(args[1]))
		return true
	case "help", "-h", "--help":
		fmt.Println("toran [reset-password NAME]\nWith no command, starts the server. See docs/configuration.md.")
		return true
	}
	log.Fatalf("unknown command %q (try: toran help)", args[0])
	return true
}
