package main

// The accounts store: who may log in, kept in users.json.
//
// The file is small (a household has a handful of users), so it is read once at startup, held
// in memory and rewritten whole, atomically and readable only by its owner, on every change.
// It carries a version number so a later release can migrate it, for example into SQLite.
// The rest of the server only uses the methods below, never the file.

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	roleAdmin = "admin"
	roleUser  = "user"

	usersFileVersion = 1
)

var (
	errBadName    = errors.New("a user name is 1 to 32 letters, digits, dots, dashes or underscores, starting with a letter or digit")
	errBadRole    = errors.New("the role must be admin or user")
	errNameTaken  = errors.New("that user name is already taken")
	errNoUser     = errors.New("no such user")
	errLastAdmin  = errors.New("there must always be at least one active admin")
	userNameRegex = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,31}$`)
)

type user struct {
	ID           string    `json:"id"`           // random and permanent: folders and sessions refer to it, not to the name
	Name         string    `json:"name"`         // lower case, unique
	Role         string    `json:"role"`         // admin | user
	Hash         string    `json:"hash"`         // see password.go
	TokenVersion int       `json:"tokenVersion"` // raising it logs the user out everywhere
	Disabled     bool      `json:"disabled,omitempty"`
	Created      time.Time `json:"created"`
}

type usersFile struct {
	Version int    `json:"version"`
	Users   []user `json:"users"`
}

type accounts struct {
	path string
	now  func() time.Time

	mu      sync.Mutex
	list    []user    // in order of creation
	raw     []byte    // the file as last read or written, to notice changes made behind our back
	checked time.Time // when the file was last compared with raw
}

// recheckEvery bounds how often the file is looked at when only reading. A change made by another
// process, such as `toran reset-password` while the server runs, is picked up within this time.
const recheckEvery = time.Second

// openAccounts reads users.json. A missing file is an empty store; a damaged or newer file is an
// error, so that Toran stops instead of treating it as empty and overwriting it.
func openAccounts(path string) (*accounts, error) {
	a := &accounts{path: path, now: time.Now}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return a, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	list, err := parseUsers(path, b)
	if err != nil {
		return nil, err
	}
	a.list, a.raw = list, b
	return a, nil
}

func parseUsers(path string, b []byte) ([]user, error) {
	var f usersFile
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("%s is not valid JSON (%v); fix or restore it, Toran will not overwrite it", path, err)
	}
	if f.Version != usersFileVersion {
		return nil, fmt.Errorf("%s has version %d but this Toran reads version %d; use the Toran release that wrote it", path, f.Version, usersFileVersion)
	}
	seenID, seenName := map[string]bool{}, map[string]bool{}
	for _, u := range f.Users {
		switch {
		case u.ID == "" || seenID[u.ID]:
			return nil, fmt.Errorf("%s: missing or repeated user id %q", path, u.ID)
		case !userNameRegex.MatchString(u.Name) || seenName[u.Name]:
			return nil, fmt.Errorf("%s: bad or repeated user name %q", path, u.Name)
		case u.Role != roleAdmin && u.Role != roleUser:
			return nil, fmt.Errorf("%s: user %q has role %q", path, u.Name, u.Role)
		}
		seenID[u.ID], seenName[u.Name] = true, true
	}
	return f.Users, nil
}

// refresh picks up an edit made to the file by someone else. A file that has become unreadable or
// invalid is ignored (and reported) rather than trusted, so the last good state stays in force.
// force skips the rate limit. Call with mu held.
func (a *accounts) refresh(force bool) {
	now := time.Now()
	if a.now != nil { // tests use a fake clock
		now = a.now()
	}
	if !force && now.Sub(a.checked) < recheckEvery {
		return
	}
	a.checked = now
	b, err := os.ReadFile(a.path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		if a.raw != nil {
			a.list, a.raw = nil, nil
		}
	case err != nil:
		log.Printf("users: cannot re-read %s: %v", a.path, err)
	case !bytes.Equal(b, a.raw):
		list, err := parseUsers(a.path, b)
		if err != nil {
			log.Printf("users: ignoring the changed file: %v", err)
			a.raw = b // report once, not on every request
			return
		}
		a.list, a.raw = list, b
	}
}

// change applies fn to a copy of the users, checks the rules that must always hold, saves, and
// only then makes the change visible. If saving fails nothing has changed.
func (a *accounts) change(fn func(us []user) ([]user, error)) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.refresh(true)
	next, err := fn(append([]user(nil), a.list...))
	if err != nil {
		return err
	}
	// Once there is an active admin there must always be one, or nobody could manage the others.
	if (len(next) > 0 || activeAdmins(a.list) > 0) && activeAdmins(next) == 0 {
		return errLastAdmin
	}
	b, err := json.MarshalIndent(usersFile{Version: usersFileVersion, Users: next}, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	if err := atomicWriteMode(a.path, b, 0o600); err != nil {
		return fmt.Errorf("save users: %w", err)
	}
	a.list, a.raw = next, b
	return nil
}

func activeAdmins(us []user) int {
	n := 0
	for _, u := range us {
		if u.Role == roleAdmin && !u.Disabled {
			n++
		}
	}
	return n
}

func normalizeName(name string) string { return strings.ToLower(strings.TrimSpace(name)) }

// create adds a user with an already hashed password.
func (a *accounts) create(name, role, hash string) (user, error) {
	name = normalizeName(name)
	if !userNameRegex.MatchString(name) {
		return user{}, errBadName
	}
	if role != roleAdmin && role != roleUser {
		return user{}, errBadRole
	}
	id, err := newID()
	if err != nil {
		return user{}, err
	}
	u := user{ID: id, Name: name, Role: role, Hash: hash, Created: time.Now().UTC().Truncate(time.Second)}
	err = a.change(func(us []user) ([]user, error) {
		for _, o := range us {
			if o.Name == name {
				return nil, errNameTaken
			}
		}
		return append(us, u), nil
	})
	if err != nil {
		return user{}, err
	}
	return u, nil
}

// update changes one user. fn may edit the copy it is given; returning an error cancels.
func (a *accounts) update(id string, fn func(u *user) error) (user, error) {
	var out user
	err := a.change(func(us []user) ([]user, error) {
		for i := range us {
			if us[i].ID == id {
				if err := fn(&us[i]); err != nil {
					return nil, err
				}
				out = us[i]
				return us, nil
			}
		}
		return nil, errNoUser
	})
	if err != nil {
		return user{}, err
	}
	return out, nil
}

// remove deletes the account. Its files are the caller's business.
func (a *accounts) remove(id string) error {
	return a.change(func(us []user) ([]user, error) {
		for i := range us {
			if us[i].ID == id {
				return append(us[:i], us[i+1:]...), nil
			}
		}
		return nil, errNoUser
	})
}

func (a *accounts) count() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.refresh(false)
	return len(a.list)
}

func (a *accounts) byID(id string) (user, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.refresh(false)
	for _, u := range a.list {
		if u.ID == id {
			return u, true
		}
	}
	return user{}, false
}

func (a *accounts) byName(name string) (user, bool) {
	name = normalizeName(name)
	a.mu.Lock()
	defer a.mu.Unlock()
	a.refresh(false)
	for _, u := range a.list {
		if u.Name == name {
			return u, true
		}
	}
	return user{}, false
}

func (a *accounts) all() []user {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.refresh(false)
	return append([]user(nil), a.list...)
}

func newID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
