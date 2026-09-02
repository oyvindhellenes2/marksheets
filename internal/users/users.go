// Package users remembers who has logged in, so that a task can be given to
// somebody who is not currently sitting in front of the app.
//
// It is a small JSON file kept *outside* the page folder. Two reasons: the page
// folder is a git repository that gets pushed to a public remote, and an email
// address is not something to publish by accident; and a file in there ending
// in `.json` would be counted as a page.
package users

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"marksheets/internal/doc"
)

// User is a person. Sub is the identity provider's own id and is the only
// field that is truly stable — a name and an email can both change, and Login
// is derived, so anything that has to survive a rename hangs off Sub.
type User struct {
	Sub   string `json:"sub"`
	Login string `json:"login"`
	Name  string `json:"name"`
	Email string `json:"email,omitempty"`
}

// Label is what to show: the name if there is one, the login otherwise.
func (u User) Label() string {
	if strings.TrimSpace(u.Name) != "" {
		return u.Name
	}
	return u.Login
}

// Store is the file, kept in memory and written on every change.
type Store struct {
	path string

	mu   sync.RWMutex
	list []User
}

// Open reads the file, making an empty one in memory if it is not there yet.
func Open(path string) (*Store, error) {
	s := &Store{path: path}
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, &s.list); err != nil {
		return nil, err
	}
	return s, nil
}

// Path is the file the users are kept in.
func (s *Store) Path() string { return s.path }

// Upsert records somebody who has just logged in, and hands back the stored
// user — which may differ from what was passed in, since the login is settled
// here.
//
// Matching is by Sub. A name or an email that changes at the identity provider
// follows; the login does not, because it is an address — somebody's page, and
// the owner written on every task they have.
func (s *Store) Upsert(u User) (User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.list {
		if s.list[i].Sub != u.Sub {
			continue
		}
		s.list[i].Name, s.list[i].Email = u.Name, u.Email
		out := s.list[i]
		return out, s.write()
	}

	u.Login = s.free(u.Login)
	s.list = append(s.list, u)
	return u, s.write()
}

// free finds an unused login, since two people may well have the same name.
func (s *Store) free(want string) string {
	base := doc.Slug(want)
	if base == "" {
		base = "brukar"
	}
	login := base
	for attempt := 2; ; attempt++ {
		taken := false
		for _, u := range s.list {
			if u.Login == login {
				taken = true
				break
			}
		}
		if !taken {
			return login
		}
		login = base + "-" + strconv.Itoa(attempt)
	}
}

// List is everybody, by name.
func (s *Store) List() []User {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := append([]User(nil), s.list...)
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Label()) < strings.ToLower(out[j].Label()) })
	return out
}

// Get finds somebody by the name in their address.
func (s *Store) Get(login string) (User, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, u := range s.list {
		if u.Login == login {
			return u, true
		}
	}
	return User{}, false
}

// write saves the file. Written whole through a temporary file and renamed, so
// a crash mid-write cannot leave half a list.
func (s *Store) write() error {
	raw, err := json.MarshalIndent(s.list, "", " ")
	if err != nil {
		return err
	}
	if dir := filepath.Dir(s.path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, append(raw, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
