package main

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func newAccounts(t *testing.T) (*accounts, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "users.json")
	a, err := openAccounts(path)
	if err != nil {
		t.Fatal(err)
	}
	return a, path
}

func mustCreate(t *testing.T, a *accounts, name, role string) user {
	t.Helper()
	u, err := a.create(name, role, "hash-"+name)
	if err != nil {
		t.Fatalf("create %s: %v", name, err)
	}
	return u
}

func TestAccountsStartEmptyAndPersist(t *testing.T) {
	a, path := newAccounts(t)
	if a.count() != 0 {
		t.Fatal("a new store must be empty")
	}
	if _, err := os.Stat(path); err == nil {
		t.Error("opening must not create the file")
	}
	u := mustCreate(t, a, "Vedant", roleAdmin)
	if u.Name != "vedant" || len(u.ID) != 16 || u.Role != roleAdmin || u.Hash != "hash-Vedant" {
		t.Errorf("created user: %+v", u)
	}
	mustCreate(t, a, "asha", roleUser)

	b, err := openAccounts(path) // a fresh start reads what was saved
	if err != nil {
		t.Fatal(err)
	}
	if b.count() != 2 {
		t.Fatalf("reloaded %d users", b.count())
	}
	got, ok := b.byName("  VEDANT ")
	if !ok || got.ID != u.ID || got.Hash != u.Hash || !got.Created.Equal(u.Created) {
		t.Errorf("reloaded user: %+v ok=%v, want %+v", got, ok, u)
	}
	if byID, ok := b.byID(u.ID); !ok || byID.Name != "vedant" {
		t.Error("lookup by id failed")
	}
	if _, ok := b.byName("nobody"); ok {
		t.Error("an unknown name must not be found")
	}
}

func TestUsersFileIsPrivate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file modes are not meaningful on Windows")
	}
	a, path := newAccounts(t)
	mustCreate(t, a, "vedant", roleAdmin)
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("users.json mode %v, want 0600 (it holds password hashes)", fi.Mode().Perm())
	}
}

func TestNamesAreValidatedAndUnique(t *testing.T) {
	a, _ := newAccounts(t)
	mustCreate(t, a, "vedant", roleAdmin)
	if _, err := a.create("VEDANT", roleUser, "h"); !errors.Is(err, errNameTaken) {
		t.Errorf("same name in another case: %v", err)
	}
	for _, bad := range []string{"", " ", "-lead", ".dot", "has space", "a/b", "../x", "ü", strings.Repeat("a", 33), "a\nb"} {
		if _, err := a.create(bad, roleUser, "h"); !errors.Is(err, errBadName) {
			t.Errorf("name %q: %v, want errBadName", bad, err)
		}
	}
	for _, ok := range []string{"a", "0", "asha.k", "kid_1", "x-y", strings.Repeat("a", 32)} {
		if _, err := a.create(ok, roleUser, "h"); err != nil {
			t.Errorf("name %q refused: %v", ok, err)
		}
	}
	if _, err := a.create("someone", "root", "h"); !errors.Is(err, errBadRole) {
		t.Errorf("bad role: %v", err)
	}
}

func TestFirstUserMustBeAnAdmin(t *testing.T) {
	a, _ := newAccounts(t)
	if _, err := a.create("kid", roleUser, "h"); !errors.Is(err, errLastAdmin) {
		t.Errorf("a store with only a plain user: %v", err)
	}
	if a.count() != 0 {
		t.Error("the refused user must not be stored")
	}
}

func TestLastAdminIsProtected(t *testing.T) {
	a, _ := newAccounts(t)
	admin := mustCreate(t, a, "vedant", roleAdmin)
	kid := mustCreate(t, a, "kid", roleUser)

	if _, err := a.update(admin.ID, func(u *user) error { u.Role = roleUser; return nil }); !errors.Is(err, errLastAdmin) {
		t.Errorf("demoting the only admin: %v", err)
	}
	if _, err := a.update(admin.ID, func(u *user) error { u.Disabled = true; return nil }); !errors.Is(err, errLastAdmin) {
		t.Errorf("disabling the only admin: %v", err)
	}
	if err := a.remove(admin.ID); !errors.Is(err, errLastAdmin) {
		t.Errorf("removing the only admin: %v", err)
	}
	if got, _ := a.byID(admin.ID); got.Role != roleAdmin || got.Disabled {
		t.Errorf("the refused changes must leave the admin alone: %+v", got)
	}

	// a second admin makes the first replaceable
	if _, err := a.update(kid.ID, func(u *user) error { u.Role = roleAdmin; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := a.remove(admin.ID); err != nil {
		t.Errorf("removing one of two admins: %v", err)
	}
	if err := a.remove(kid.ID); !errors.Is(err, errLastAdmin) {
		t.Errorf("removing the remaining admin: %v", err)
	}
}

func TestOnlyUserCannotBeRemoved(t *testing.T) {
	a, _ := newAccounts(t)
	u := mustCreate(t, a, "vedant", roleAdmin)
	if err := a.remove(u.ID); !errors.Is(err, errLastAdmin) {
		t.Errorf("removing the only user: %v (the store must never go back to 'first run' by itself)", err)
	}
}

func TestUpdateAndRemove(t *testing.T) {
	a, path := newAccounts(t)
	mustCreate(t, a, "vedant", roleAdmin)
	kid := mustCreate(t, a, "kid", roleUser)

	got, err := a.update(kid.ID, func(u *user) error { u.TokenVersion++; u.Hash = "new"; return nil })
	if err != nil || got.TokenVersion != 1 || got.Hash != "new" {
		t.Fatalf("update: %+v %v", got, err)
	}
	boom := errors.New("boom")
	if _, err := a.update(kid.ID, func(u *user) error { u.Hash = "lost"; return boom }); !errors.Is(err, boom) {
		t.Errorf("a failing update must report its error: %v", err)
	}
	if again, _ := a.byID(kid.ID); again.Hash != "new" {
		t.Errorf("a cancelled update must not stick: %+v", again)
	}
	if _, err := a.update("nope", func(u *user) error { return nil }); !errors.Is(err, errNoUser) {
		t.Errorf("unknown id: %v", err)
	}
	if err := a.remove(kid.ID); err != nil {
		t.Fatal(err)
	}
	if err := a.remove(kid.ID); !errors.Is(err, errNoUser) {
		t.Errorf("removing twice: %v", err)
	}
	b, _ := openAccounts(path)
	if b.count() != 1 {
		t.Errorf("after reload %d users, want 1", b.count())
	}
}

func TestFailedSaveChangesNothing(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := &accounts{path: filepath.Join(blocker, "users.json")} // a folder cannot be made under a file
	if _, err := a.create("vedant", roleAdmin, "h"); err == nil {
		t.Fatal("saving must fail")
	}
	if a.count() != 0 {
		t.Error("a user whose save failed must not appear")
	}
}

func TestDamagedFilesAreRefusedAndKept(t *testing.T) {
	good := `{"version":1,"users":[{"id":"aaaaaaaaaaaaaaaa","name":"vedant","role":"admin","hash":"h","tokenVersion":0,"created":"2026-01-01T00:00:00Z"}]}`
	for name, content := range map[string]string{
		"not json":       "{oops",
		"newer version":  `{"version":2,"users":[]}`,
		"no version":     `{"users":[]}`,
		"repeated id":    strings.Replace(good, `]}`, `,{"id":"aaaaaaaaaaaaaaaa","name":"b","role":"user","hash":"h","tokenVersion":0,"created":"2026-01-01T00:00:00Z"}]}`, 1),
		"repeated name":  strings.Replace(good, `]}`, `,{"id":"bbbbbbbbbbbbbbbb","name":"vedant","role":"user","hash":"h","tokenVersion":0,"created":"2026-01-01T00:00:00Z"}]}`, 1),
		"bad name":       strings.Replace(good, `"vedant"`, `"../x"`, 1),
		"unknown role":   strings.Replace(good, `"admin"`, `"root"`, 1),
		"missing id":     strings.Replace(good, `"aaaaaaaaaaaaaaaa"`, `""`, 1),
		"uppercase name": strings.Replace(good, `"vedant"`, `"Vedant"`, 1),
	} {
		path := filepath.Join(t.TempDir(), "users.json")
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := openAccounts(path); err == nil {
			t.Errorf("%s: must be refused", name)
		}
		if b, _ := os.ReadFile(path); string(b) != content {
			t.Errorf("%s: the file must be left exactly as it was", name)
		}
	}
	path := filepath.Join(t.TempDir(), "users.json")
	_ = os.WriteFile(path, []byte(good), 0o600)
	if a, err := openAccounts(path); err != nil || a.count() != 1 {
		t.Errorf("the good file: %v", err)
	}
}

func TestConcurrentCreatesOfOneName(t *testing.T) {
	a, _ := newAccounts(t)
	mustCreate(t, a, "vedant", roleAdmin)
	var wg sync.WaitGroup
	results := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := a.create("race", roleUser, "h")
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	won := 0
	for err := range results {
		if err == nil {
			won++
		} else if !errors.Is(err, errNameTaken) {
			t.Errorf("unexpected error %v", err)
		}
	}
	if won != 1 || a.count() != 2 {
		t.Errorf("%d creations won, %d users stored; want 1 and 2", won, a.count())
	}
}

func TestListIsACopy(t *testing.T) {
	a, _ := newAccounts(t)
	mustCreate(t, a, "vedant", roleAdmin)
	l := a.all()
	l[0].Role = roleUser
	if got, _ := a.byName("vedant"); got.Role != roleAdmin {
		t.Error("changing the returned list must not change the store")
	}
}
