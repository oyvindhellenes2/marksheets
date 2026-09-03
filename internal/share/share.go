// Package share keeps the public links: which page each one opens, and nothing
// else.
//
// A share link is the one address in this wiki that needs no account, so the
// token *is* the credential and everything follows from that. It is 32 bytes of
// crypto/rand, which is not guessable; it is the whole secret, so anybody the
// link reaches can read that page; and it is written down, because a link that
// stopped working when the service restarted would be worse than no link at
// all — somebody would have sent it to a room full of people an hour earlier.
//
// One token per page, minted once and handed back afterwards. Pressing the
// button twice should give the same address: a page that grew a second link
// every time somebody copied it would be a page nobody could ever un-share.
package share

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Link is one shared page.
type Link struct {
	Token string    `json:"token"`
	Slug  string    `json:"slug"`
	By    string    `json:"by,omitempty"`
	At    time.Time `json:"at"`
}

type Store struct {
	path string

	mu   sync.RWMutex
	list []Link
}

// Open reads the file. A missing one is the ordinary state of a wiki that has
// never shared anything.
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

func (s *Store) Path() string { return s.path }

// For returns the link for a page, making one the first time. `by` is who
// pressed the button, kept so the file can answer "who shared this" without
// anybody having to remember.
func (s *Store) For(slug, by string) (Link, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, l := range s.list {
		if l.Slug == slug {
			return l, nil
		}
	}
	token, err := newToken()
	if err != nil {
		return Link{}, err
	}
	l := Link{Token: token, Slug: slug, By: by, At: time.Now()}
	s.list = append(s.list, l)
	if err := s.write(); err != nil {
		// The link is not real until it is on disk: handing back a token that
		// a restart would forget is how somebody ends up sending a dead address
		// to a room full of people.
		s.list = s.list[:len(s.list)-1]
		return Link{}, err
	}
	return l, nil
}

// Slug is which page a token opens. The second return is false for a token
// that was never minted or has since been revoked, which is the same answer
// from the outside and should be.
func (s *Store) Slug(token string) (string, bool) {
	if token == "" {
		return "", false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, l := range s.list {
		if l.Token == token {
			return l.Slug, true
		}
	}
	return "", false
}

// Shared reports whether a page has a link out in the world, so the button can
// say so.
func (s *Store) Shared(slug string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, l := range s.list {
		if l.Slug == slug {
			return true
		}
	}
	return false
}

// Slugs is every page currently shared. Used to work out which attachments may
// be served to somebody with no account: the ones on a page they can already
// read in full.
func (s *Store) Slugs() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, 0, len(s.list))
	for _, l := range s.list {
		out = append(out, l.Slug)
	}
	return out
}

// Revoke takes a page's link back. The address stops working at once and a new
// press of the button mints a different one — an old link is never resurrected,
// which is the whole point of revoking it.
func (s *Store) Revoke(slug string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := s.list[:0]
	for _, l := range s.list {
		if l.Slug != slug {
			kept = append(kept, l)
		}
	}
	s.list = kept
	return s.write()
}

// Forget drops the link for a page that no longer exists. Called when a page is
// deleted, so a token cannot outlive what it pointed at.
func (s *Store) Forget(slug string) { _ = s.Revoke(slug) }

// write saves the file whole, through a temp file and a rename, 0600. The same
// shape as the user list and the sessions beside it: a half-written file here
// would break every link at once.
func (s *Store) write() error {
	sort.Slice(s.list, func(i, j int) bool { return s.list[i].Slug < s.list[j].Slug })
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

// newToken is 32 bytes of randomness in the URL-safe alphabet, so it survives
// being pasted anywhere a link goes.
func newToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
