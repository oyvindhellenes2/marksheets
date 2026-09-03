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
// One token per page. Pressing the button twice gives the same address, with
// its week started again: a page that grew a second link every time somebody
// copied it would be a page nobody could ever un-share. An expired link is
// never revived — that address died, and somebody may have been told so.
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

// Life is how long a link works for. A week: long enough to send a page to
// somebody and have them read it after the weekend, short enough that a link
// pasted into a chat two years ago is not still open.
//
// Renewed by sharing again rather than counted from the first press. Somebody
// who copies the address today means for it to work from today; a link that
// quietly had two days left because it was first made on Monday is the sort of
// thing that fails in front of an audience.
const Life = 7 * 24 * time.Hour

// Link is one shared page.
type Link struct {
	Token string    `json:"token"`
	Slug  string    `json:"slug"`
	By    string    `json:"by,omitempty"`
	At    time.Time `json:"at"`
	Till  time.Time `json:"till"`
}

// Live reports whether a link still works.
func (l Link) Live() bool { return time.Now().Before(l.Till) }

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
//
// A live link is handed back with its week started again. An expired one is not
// revived: a new token is minted instead, which is the same rule revoking
// follows.
func (s *Store) For(slug, by string) (Link, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	before := append([]Link(nil), s.list...)
	var out Link
	found := false
	for i, l := range s.list {
		if l.Slug != slug {
			continue
		}
		if !l.Live() {
			break // expired: fall through and mint a new one
		}
		s.list[i].Till = time.Now().Add(Life)
		out, found = s.list[i], true
		break
	}
	if !found {
		token, err := newToken()
		if err != nil {
			return Link{}, err
		}
		out = Link{Token: token, Slug: slug, By: by, At: time.Now(), Till: time.Now().Add(Life)}
		s.list = append(dropSlug(s.list, slug), out)
	}
	if err := s.write(); err != nil {
		// The link is not real until it is on disk: handing back a token that
		// a restart would forget is how somebody ends up sending a dead address
		// to a room full of people.
		s.list = before
		return Link{}, err
	}
	return out, nil
}

func dropSlug(list []Link, slug string) []Link {
	kept := make([]Link, 0, len(list))
	for _, l := range list {
		if l.Slug != slug {
			kept = append(kept, l)
		}
	}
	return kept
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
		// An expired link answers exactly as a made-up one does. The page it
		// pointed at is not the caller's business any more.
		if l.Token == token && l.Live() {
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
		if l.Slug == slug && l.Live() {
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
		// Expired links grant nothing, attachments included.
		if l.Live() {
			out = append(out, l.Slug)
		}
	}
	return out
}

// Revoke takes a page's link back. The address stops working at once and a new
// press of the button mints a different one — an old link is never resurrected,
// which is the whole point of revoking it.
func (s *Store) Revoke(slug string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.list = dropSlug(s.list, slug)
	return s.write()
}

// Forget drops the link for a page that no longer exists. Called when a page is
// deleted, so a token cannot outlive what it pointed at.
func (s *Store) Forget(slug string) { _ = s.Revoke(slug) }

// write saves the file whole, through a temp file and a rename, 0600. The same
// shape as the user list and the sessions beside it: a half-written file here
// would break every link at once.
func (s *Store) write() error {
	// Expired links are dropped on the way out. Nothing else prunes them, and a
	// file that only grows is a file that eventually says who shared what two
	// years ago for no reason at all.
	now := time.Now()
	live := make([]Link, 0, len(s.list))
	for _, l := range s.list {
		if now.Before(l.Till) {
			live = append(live, l)
		}
	}
	s.list = live
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
