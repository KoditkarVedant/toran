package main

// Managing users: list, add, change and remove, for admins only.
//
// An admin cannot change their own role or state, reset their own password here or remove their
// own account: those are the easy ways to lock yourself out. Another admin can do them, and
// everyone changes their own password from the account menu (changePassword in auth.go). A removed
// user's files are never deleted; the folder is renamed so an admin can still recover or delete it.

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

type adminUserView struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Role     string    `json:"role"`
	Disabled bool      `json:"disabled"`
	Created  time.Time `json:"created"`
}

func (u user) adminView() adminUserView {
	return adminUserView{u.ID, u.Name, u.Role, u.Disabled, u.Created}
}

// userError answers with the right status for what the accounts store refused.
func userError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errBadName), errors.Is(err, errBadRole):
		jsonError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, errNameTaken), errors.Is(err, errLastAdmin):
		jsonError(w, http.StatusConflict, err.Error())
	case errors.Is(err, errNoUser):
		jsonError(w, http.StatusNotFound, err.Error())
	default:
		log.Printf("users: %v", err)
		jsonError(w, http.StatusInternalServerError, "could not save the change")
	}
}

func (s *server) listUsers(w http.ResponseWriter, r *http.Request, _ user) {
	out := []adminUserView{}
	for _, u := range s.accounts.all() {
		out = append(out, u.adminView())
	}
	writeJSON(w, 200, out)
}

func (s *server) createUser(w http.ResponseWriter, r *http.Request, _ user) {
	var req struct{ Name, Role, Password string }
	if !decodeBody(w, r, &req) {
		return
	}
	name := normalizeName(req.Name)
	if !userNameRegex.MatchString(name) {
		jsonError(w, http.StatusBadRequest, errBadName.Error())
		return
	}
	if req.Role != roleAdmin && req.Role != roleUser {
		jsonError(w, http.StatusBadRequest, errBadRole.Error())
		return
	}
	if err := checkPassword(name, req.Password); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	hash, ok := s.hashGated(r.Context(), req.Password)
	if !ok {
		jsonError(w, http.StatusServiceUnavailable, "the server is busy; try again")
		return
	}
	u, err := s.accounts.create(name, req.Role, hash)
	if err != nil {
		userError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, u.adminView())
}

// updateUser changes a role, enables or disables an account, or sets a new password. Only the
// fields present in the request are touched. Ending someone's logins happens through the token
// version (password reset) or the disabled flag, both checked on every request.
func (s *server) updateUser(w http.ResponseWriter, r *http.Request, me user) {
	id := r.PathValue("id")
	var req struct {
		Role     *string
		Disabled *bool
		Password *string
	}
	if !decodeBody(w, r, &req) {
		return
	}
	if id == me.ID {
		jsonError(w, http.StatusBadRequest, "you cannot change your own account here; ask another admin, or use the account menu for your password")
		return
	}
	if req.Role != nil && *req.Role != roleAdmin && *req.Role != roleUser {
		jsonError(w, http.StatusBadRequest, errBadRole.Error())
		return
	}
	target, ok := s.accounts.byID(id)
	if !ok {
		userError(w, errNoUser)
		return
	}
	var hash string
	if req.Password != nil {
		if err := checkPassword(target.Name, *req.Password); err != nil {
			jsonError(w, http.StatusBadRequest, err.Error())
			return
		}
		if hash, ok = s.hashGated(r.Context(), *req.Password); !ok {
			jsonError(w, http.StatusServiceUnavailable, "the server is busy; try again")
			return
		}
	}
	u, err := s.accounts.update(id, func(x *user) error {
		if req.Role != nil {
			x.Role = *req.Role
		}
		if req.Disabled != nil {
			x.Disabled = *req.Disabled
		}
		if req.Password != nil {
			x.Hash = hash
			x.TokenVersion++ // every existing login of that user ends
		}
		return nil
	})
	if err != nil {
		userError(w, err)
		return
	}
	writeJSON(w, 200, u.adminView())
}

// deleteUser removes the account and sets its files aside.
func (s *server) deleteUser(w http.ResponseWriter, r *http.Request, me user) {
	id := r.PathValue("id")
	if id == me.ID {
		jsonError(w, http.StatusBadRequest, "you cannot remove your own account")
		return
	}
	target, ok := s.accounts.byID(id)
	if !ok {
		userError(w, errNoUser)
		return
	}
	if err := s.accounts.remove(id); err != nil {
		userError(w, err)
		return
	}
	s.dashMu.Lock()
	delete(s.dashboards, id)
	s.dashMu.Unlock()

	src := s.userDir(target)
	if _, err := os.Stat(src); err == nil {
		dst := filepath.Join(s.root, "users", fmt.Sprintf(".removed-%s-%d", id, time.Now().Unix()))
		if err := os.Rename(src, dst); err != nil {
			log.Printf("set aside the files of removed user %q: %v (they are still in %s)", target.Name, err, src)
		} else {
			log.Printf("removed user %q; their files are kept in %s", target.Name, dst)
		}
	}
	w.WriteHeader(http.StatusNoContent)
}
