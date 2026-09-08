// Package preview turns a bare address on a line into a card with a title, a
// description and a picture — the thing Apple Notes does when you paste a link
// on its own.
//
// It is the first and only place this app reaches outside its own folder, and
// that is worth saying plainly. Everything else here is computed from the files
// on disk: search, backlinks, the unpublished set, who is down for what. This
// fetches somebody else's website. Three things follow from that.
//
// **A render never waits for the network.** The renderer asks for what is
// cached and gets an answer immediately; a miss starts a fetch in the
// background and draws the plain card meanwhile. The rich version appears on
// the next load. A page must not take five seconds to open because a site
// somebody linked to has gone slow.
//
// **A URL on a page is not permission to knock on anything.** A page file is
// hand-editable and a link can point anywhere, including at things inside the
// network this server sits in — the router, a database, the metadata service on
// a cloud host. The dialler therefore refuses any address that resolves to a
// private, loopback, link-local or carrier-grade-NAT range, and it checks the
// *resolved* address at connect time rather than the hostname beforehand, so a
// name that answers differently the second time cannot slip past.
//
// **The picture is pointed at, not copied.** The card carries the remote image
// URL, so a reader's browser fetches it from the site itself. That is the
// deliberate trade: nothing new is written into the page folder and nothing is
// pushed to GitHub that nobody put there, at the cost of the site learning that
// somebody looked. Downloading instead is a one-line change here and a rather
// larger one for the page repository.
package preview

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Card is what is known about one address.
type Card struct {
	URL         string    `json:"url"`
	Title       string    `json:"title,omitempty"`
	Description string    `json:"description,omitempty"`
	Image       string    `json:"image,omitempty"`
	Site        string    `json:"site,omitempty"`
	FetchedAt   time.Time `json:"fetchedAt"`
	// Failed marks an address that could not be read — gone, private, slow,
	// not HTML. It is remembered so a dead link is not fetched again on every
	// single render, and retried after a day in case it was the network's
	// fault rather than the link's.
	Failed bool `json:"failed,omitempty"`
}

// OK reports whether there is enough here to draw a card with.
func (c Card) OK() bool { return !c.Failed && strings.TrimSpace(c.Title) != "" }

const (
	// How long a card is trusted before it is fetched again. Titles change
	// rarely and the cost of being a month out of date is low.
	freshFor = 30 * 24 * time.Hour
	// A failure is retried sooner than that, because most of them are the
	// network having a bad minute rather than the page being gone.
	retryFailedAfter = 24 * time.Hour
	// Enough of a page to hold its <head>. Anything that has not declared its
	// title in half a megabyte is not going to.
	maxBody = 512 << 10
	// Nothing here is worth making somebody wait for.
	fetchTimeout = 6 * time.Second
)

// Store is the cache, kept in a small JSON file beside the pages.
//
// Beside them and never inside: the page folder is a git repository that is
// pushed, and a list of every address anybody has looked at is not something to
// publish by accident. It is the same reasoning that keeps the users, the
// sessions and the share tokens out of there.
type Store struct {
	path   string
	client *http.Client

	mu    sync.Mutex
	cards map[string]Card
	// busy is the addresses being fetched right now, so that a page holding
	// the same link three times starts one fetch and not three.
	busy map[string]bool
}

// New loads the cache. A file that will not read is not fatal — the cost is
// fetching everything again, which is survivable, and a wiki that will not
// start because a cache of link titles is corrupt is not.
func New(path string) *Store {
	s := &Store{
		path:  path,
		cards: map[string]Card{},
		busy:  map[string]bool{},
		client: &http.Client{
			Timeout: fetchTimeout,
			Transport: &http.Transport{
				DialContext: (&net.Dialer{
					Timeout: fetchTimeout,
					Control: guardDial,
				}).DialContext,
				DisableKeepAlives: true,
			},
			CheckRedirect: func(r *http.Request, via []*http.Request) error {
				if len(via) >= 3 {
					return fmt.Errorf("too many redirects")
				}
				if r.URL.Scheme != "http" && r.URL.Scheme != "https" {
					return fmt.Errorf("redirect to %s", r.URL.Scheme)
				}
				return nil
			},
		},
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return s
	}
	var cards map[string]Card
	if err := json.Unmarshal(raw, &cards); err == nil && cards != nil {
		s.cards = cards
	}
	return s
}

// Get is what the renderer calls. It answers from the cache and never blocks;
// a miss or a stale card starts a fetch in the background and comes back empty,
// so this render draws the plain card and the next one draws the rich one.
func (s *Store) Get(raw string) Card {
	key, ok := normalise(raw)
	if !ok {
		return Card{}
	}
	s.mu.Lock()
	c, seen := s.cards[key]
	stale := !seen ||
		(c.Failed && time.Since(c.FetchedAt) > retryFailedAfter) ||
		(!c.Failed && time.Since(c.FetchedAt) > freshFor)
	start := stale && !s.busy[key]
	if start {
		s.busy[key] = true
	}
	s.mu.Unlock()

	if start {
		go s.fetch(key)
	}
	return c
}

// fetch reads one address and remembers what it found, including that it found
// nothing.
func (s *Store) fetch(key string) {
	defer func() {
		s.mu.Lock()
		delete(s.busy, key)
		s.mu.Unlock()
	}()

	card := Card{URL: key, FetchedAt: time.Now(), Failed: true}
	if got, err := s.read(key); err == nil {
		card = got
	}

	s.mu.Lock()
	s.cards[key] = card
	out, err := json.MarshalIndent(s.cards, "", "  ")
	s.mu.Unlock()
	if err != nil {
		return
	}
	// Written through a temp file and a rename, so a crash mid-write leaves the
	// old cache rather than half a new one.
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, append(out, '\n'), 0o600); err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(s.path), 0o755)
	_ = os.Rename(tmp, s.path)
}

func (s *Store) read(target string) (Card, error) {
	ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return Card{}, err
	}
	// Said plainly. A site that would rather not be read by a wiki can say so.
	req.Header.Set("User-Agent", "Marksheets link preview (+https://github.com/oyvindhellenes2/marksheets)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml")

	res, err := s.client.Do(req)
	if err != nil {
		return Card{}, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return Card{}, fmt.Errorf("status %d", res.StatusCode)
	}
	if ct := res.Header.Get("Content-Type"); ct != "" && !strings.Contains(strings.ToLower(ct), "html") {
		return Card{}, fmt.Errorf("not html: %s", ct)
	}

	body, err := io.ReadAll(io.LimitReader(res.Body, maxBody))
	if err != nil {
		return Card{}, err
	}
	card := parse(string(body))
	card.URL = target
	card.FetchedAt = time.Now()
	if card.Title == "" {
		return Card{}, fmt.Errorf("no title")
	}
	// A relative image is resolved against the page it came from, and anything
	// that is not http(s) after that is dropped rather than put in an <img>.
	if card.Image != "" {
		card.Image = absolute(target, card.Image)
	}
	if card.Site == "" {
		if u, err := url.Parse(target); err == nil {
			card.Site = strings.TrimPrefix(u.Host, "www.")
		}
	}
	return card, nil
}

var (
	metaTag  = regexp.MustCompile(`(?is)<meta\s+[^>]*>`)
	attrRe   = regexp.MustCompile(`(?is)([a-z:_-]+)\s*=\s*("([^"]*)"|'([^']*)'|([^\s"'>]+))`)
	titleTag = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	headEnd  = regexp.MustCompile(`(?is)</head>`)
)

// parse pulls the Open Graph tags out of a page with regular expressions rather
// than an HTML parser.
//
// That is a compromise, and a deliberate one: this module has no dependencies
// ([ADR-0002](../../adr/0002-no-dependencies.md)) and the standard library has
// no HTML parser. The job is narrow enough to survive it — a handful of `<meta>`
// tags in a `<head>`, read for two or three attributes each — and the worst a
// malformed page can do is yield no card, which is a state that already exists
// and is already handled. Nothing here is executed, and everything that comes
// out is escaped by the template before it reaches a browser.
func parse(body string) Card {
	if m := headEnd.FindStringIndex(body); m != nil {
		body = body[:m[1]]
	}
	var c Card
	for _, tag := range metaTag.FindAllString(body, -1) {
		var key, content string
		for _, a := range attrRe.FindAllStringSubmatch(tag, -1) {
			name := strings.ToLower(a[1])
			val := a[3] + a[4] + a[5]
			switch name {
			case "property", "name":
				key = strings.ToLower(val)
			case "content":
				content = val
			}
		}
		content = strings.TrimSpace(unescape(content))
		if content == "" {
			continue
		}
		switch key {
		case "og:title", "twitter:title":
			if c.Title == "" {
				c.Title = content
			}
		case "og:description", "twitter:description", "description":
			if c.Description == "" {
				c.Description = content
			}
		case "og:image", "og:image:url", "twitter:image":
			if c.Image == "" {
				c.Image = content
			}
		case "og:site_name":
			if c.Site == "" {
				c.Site = content
			}
		}
	}
	// A page with no Open Graph tags still has a title, and a title alone makes
	// a better card than a bare address does.
	if c.Title == "" {
		if m := titleTag.FindStringSubmatch(body); m != nil {
			c.Title = strings.TrimSpace(unescape(stripTags(m[1])))
		}
	}
	c.Title = clip(c.Title, 300)
	c.Description = clip(c.Description, 600)
	return c
}

func stripTags(s string) string {
	var b strings.Builder
	depth := 0
	for _, r := range s {
		switch {
		case r == '<':
			depth++
		case r == '>' && depth > 0:
			depth--
		case depth == 0:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// unescape handles the entities that actually turn up in a title. It is not a
// general HTML entity table, and does not need to be: anything it misses is
// shown as typed, which is untidy rather than wrong.
var entities = strings.NewReplacer(
	"&amp;", "&", "&lt;", "<", "&gt;", ">", "&quot;", `"`, "&#39;", "'",
	"&apos;", "'", "&nbsp;", " ", "&#x27;", "'", "&#038;", "&",
)

func unescape(s string) string { return entities.Replace(s) }

func clip(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len([]rune(s)) <= n {
		return s
	}
	return string([]rune(s)[:n]) + "…"
}

// absolute resolves a possibly relative image against the page it was found on,
// and drops anything that is not an ordinary web address afterwards.
func absolute(base, ref string) string {
	b, err := url.Parse(base)
	if err != nil {
		return ""
	}
	u, err := b.Parse(ref)
	if err != nil {
		return ""
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return ""
	}
	return u.String()
}

// normalise is the cache key: the address with its fragment removed, since two
// links differing only after the `#` are the same page. It also refuses
// anything that is not plain http(s), which is the only thing worth fetching.
func normalise(raw string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", false
	}
	u.Fragment = ""
	return u.String(), true
}

// guardDial is the whole of the "a link is not permission to knock" rule.
//
// It runs after the name has been resolved and immediately before the socket
// connects, which is the only place the check is honest: resolving the name
// first and connecting second leaves a window where the answer can change
// between the two, and a hostile name can be made to answer differently every
// time it is asked.
//
// Carrier-grade NAT (100.64/10) is in the list because this machine is on a
// Tailscale network in exactly that range, and a link pointing at a machine
// there would otherwise be reachable from here and from nowhere the reader is.
func guardDial(network, address string, _ syscall.RawConn) error {
	if network != "tcp4" && network != "tcp6" && network != "tcp" {
		return fmt.Errorf("preview: refusing %s", network)
	}
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("preview: bad address %q", address)
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return fmt.Errorf("preview: bad address %q", host)
	}
	if !allowed(ip) {
		return fmt.Errorf("preview: refusing to reach %s", ip)
	}
	return nil
}

// allowed is false for every range that means "somewhere inside this network"
// rather than "somewhere on the internet".
func allowed(ip netip.Addr) bool {
	ip = ip.Unmap()
	switch {
	case ip.IsLoopback(), ip.IsPrivate(), ip.IsUnspecified(),
		ip.IsLinkLocalUnicast(), ip.IsLinkLocalMulticast(),
		ip.IsMulticast(), ip.IsInterfaceLocalMulticast():
		return false
	}
	if ip.Is4() {
		b := ip.As4()
		// 100.64.0.0/10, carrier-grade NAT — and Tailscale's range, which this
		// machine is on. A link pointing in there is reachable from this server
		// and from nowhere the reader is.
		if b[0] == 100 && b[1] >= 64 && b[1] <= 127 {
			return false
		}
		// 0.0.0.0/8. IsUnspecified only covers the one address 0.0.0.0.
		if b[0] == 0 {
			return false
		}
		return true
	}
	// IPv6 unique local addresses, fc00::/7.
	b := ip.As16()
	return b[0]&0xfe != 0xfc
}
