// Package auth signs people in against an OpenID Connect provider — Pocket ID,
// here — and remembers who is who for the length of a session.
//
// Written against the protocol rather than against a library, because the
// module has no dependencies ([ADR-0002]) and this is the short version of
// OIDC: discovery, the authorization code flow with PKCE, and one call to
// `userinfo`. The ID token is *not* verified locally, which is what would
// otherwise drag in JWKS handling and RS256 — the access token is exchanged
// over TLS with the provider and then spent at the provider, so nothing here
// has to be trusted on its own signature.
//
// **Unconfigured, the app runs as it always has**: one local user, no login
// screen. That is not a back door for a configured instance — with an issuer
// set, every request is checked and there is no local user to fall back to. It
// is what makes the app runnable, and testable, without an identity provider.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"marksheets/internal/doc"
	"marksheets/internal/users"
)

// Session lifetime. Thirty days, and now thirty days of wall clock rather than
// thirty days of uptime: sessions are written to a file and read back at boot
// ([ADR-0023]). They used to live only in memory, on the reasoning that a
// restart signing everybody out is a login rather than an outage — which was
// true while restarts were rare, and stopped being true the day the wiki was
// deployed nine times.
const sessionLife = 30 * 24 * time.Hour

// key is what a session is filed under, here and on disk: the SHA-256 of the
// token, never the token. The cookie holds the real one, so the file is a list
// of expiries and names rather than a ring of working keys — reading it lends
// nobody a session. It costs one hash per request, against a map lookup.
func key(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// stored is one session as it is written down.
type stored struct {
	Key  string     `json:"key"`
	User users.User `json:"user"`
	Till time.Time  `json:"till"`
}

// Config is what the app was started with.
type Config struct {
	// Issuer is the OIDC provider, e.g. https://tilgang.verftet.info. Empty
	// turns authentication off and the app runs as one local user.
	Issuer       string
	ClientID     string
	ClientSecret string
	// BaseURL is this app's own address, for building the redirect back from
	// the provider. Empty means "work it out from the request", which is right
	// behind a proxy that sets X-Forwarded-*.
	BaseURL string
	// Local is the login name used when there is no issuer.
	Local string
	// Sessions is the file the session store is kept in. It holds credentials
	// in the sense that an expiry and a name are enough to say who is signed
	// in, so it lives beside the user list and outside PAGES_DIR — never
	// anywhere with a remote. Empty turns persistence off and sessions live
	// only as long as the process, which is what tests and a bare `go run`
	// want.
	Sessions string
}

// FromEnv reads the configuration.
func FromEnv() Config {
	local := os.Getenv("AUTH_LOCAL")
	if local == "" {
		local = os.Getenv("USER")
	}
	if local == "" {
		local = "lokal"
	}
	return Config{
		Issuer:       strings.TrimRight(os.Getenv("AUTH_ISSUER"), "/"),
		ClientID:     os.Getenv("AUTH_CLIENT_ID"),
		ClientSecret: os.Getenv("AUTH_CLIENT_SECRET"),
		BaseURL:      strings.TrimRight(os.Getenv("AUTH_BASE_URL"), "/"),
		Local:        local,
		Sessions:     os.Getenv("SESSIONS_PATH"),
	}
}

// endpoints is the part of the discovery document we use.
type endpoints struct {
	Authorization string `json:"authorization_endpoint"`
	Token         string `json:"token_endpoint"`
	UserInfo      string `json:"userinfo_endpoint"`
	EndSession    string `json:"end_session_endpoint"`
}

type session struct {
	user users.User
	till time.Time
}

// pending is one login in flight: the PKCE verifier and where to go afterwards.
type pending struct {
	verifier string
	back     string
	till     time.Time
}

type Auth struct {
	cfg   Config
	users *users.Store
	// local is the user everything runs as when there is no issuer.
	local users.User

	// open decides which requests need no session at all. Nil means none do,
	// which is what this package does on its own and what it did before share
	// links existed.
	open func(*http.Request) bool

	mu       sync.Mutex
	ends     endpoints
	found    bool // discovery has succeeded at least once
	sessions map[string]session
	pending  map[string]pending
}

// Open registers the test for requests that need no session. There is exactly
// one, so there is exactly one place to look for what this wiki lets a stranger
// read.
func (a *Auth) Open(test func(*http.Request) bool) { a.open = test }

// New sets up authentication. Discovery is attempted here and retried on
// demand: a provider that is briefly unreachable at boot should not stop the
// app from starting, and it must not quietly leave it unprotected either —
// every request fails closed until discovery succeeds.
func New(cfg Config, store *users.Store) *Auth {
	a := &Auth{
		cfg:      cfg,
		users:    store,
		sessions: map[string]session{},
		pending:  map[string]pending{},
	}
	if !a.Configured() {
		a.local = users.User{
			Sub:   "lokal:" + cfg.Local,
			Login: doc.Slug(cfg.Local),
			Name:  cfg.Local,
		}
		if u, err := store.Upsert(a.local); err == nil {
			a.local = u
		}
		log.Printf("no AUTH_ISSUER — running as one local user, %q", a.local.Login)
		return a
	}
	a.load()
	if err := a.discover(); err != nil {
		log.Printf("auth: could not reach %s yet (%v) — will retry on the first sign-in", cfg.Issuer, err)
	} else {
		log.Printf("auth: signing in against %s", cfg.Issuer)
	}
	return a
}

// load reads the sessions back. A missing file is the ordinary first boot, and
// an unreadable one is reported and stepped over rather than fatal: the worst
// case is everybody signing in again, which is exactly where this started.
// Refusing to start a wiki because a cache of logins would not parse would be
// the wrong trade in every direction.
func (a *Auth) load() {
	if a.cfg.Sessions == "" {
		return
	}
	raw, err := os.ReadFile(a.cfg.Sessions)
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		log.Printf("auth: could not read %s (%v) — everybody signs in again", a.cfg.Sessions, err)
		return
	}
	var list []stored
	if err := json.Unmarshal(raw, &list); err != nil {
		log.Printf("auth: %s will not parse (%v) — everybody signs in again", a.cfg.Sessions, err)
		return
	}
	now := time.Now()
	kept := 0
	a.mu.Lock()
	for _, s := range list {
		if s.Key == "" || now.After(s.Till) {
			continue
		}
		a.sessions[s.Key] = session{user: s.User, till: s.Till}
		kept++
	}
	a.mu.Unlock()
	log.Printf("auth: %d session(s) carried over from %s", kept, a.cfg.Sessions)
}

// persist writes the sessions out. The snapshot is taken under the lock and the
// file written outside it: every request takes this mutex to read, and none of
// them should wait on a disk.
//
// Whole file, temp-and-rename, 0600 — the same shape as the user list next to
// it, for the same reason. A half-written file here signs everybody out.
func (a *Auth) persist() {
	if a.cfg.Sessions == "" {
		return
	}
	now := time.Now()
	a.mu.Lock()
	list := make([]stored, 0, len(a.sessions))
	for k, s := range a.sessions {
		if now.After(s.till) {
			continue
		}
		list = append(list, stored{Key: k, User: s.user, Till: s.till})
	}
	a.mu.Unlock()

	// A stable order keeps the file readable and its writes boring to diff.
	sort.Slice(list, func(i, j int) bool { return list[i].Key < list[j].Key })

	raw, err := json.MarshalIndent(list, "", " ")
	if err != nil {
		log.Printf("auth: could not encode sessions: %v", err)
		return
	}
	if dir := filepath.Dir(a.cfg.Sessions); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			log.Printf("auth: could not make %s: %v", dir, err)
			return
		}
	}
	tmp := a.cfg.Sessions + ".tmp"
	if err := os.WriteFile(tmp, append(raw, '\n'), 0o600); err != nil {
		log.Printf("auth: could not write %s: %v", tmp, err)
		return
	}
	if err := os.Rename(tmp, a.cfg.Sessions); err != nil {
		log.Printf("auth: could not replace %s: %v", a.cfg.Sessions, err)
	}
}

// Configured reports whether there is an identity provider to sign in against.
func (a *Auth) Configured() bool { return a.cfg.Issuer != "" && a.cfg.ClientID != "" }

// Issuer is the provider's address, for saying where somebody is being sent.
func (a *Auth) Issuer() string { return a.cfg.Issuer }

func (a *Auth) discover() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", a.cfg.Issuer+"/.well-known/openid-configuration", nil)
	if err != nil {
		return err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("discovery: %s", res.Status)
	}
	var ends endpoints
	if err := json.NewDecoder(res.Body).Decode(&ends); err != nil {
		return err
	}
	if ends.Authorization == "" || ends.Token == "" || ends.UserInfo == "" {
		return errors.New("discovery document is missing an endpoint")
	}
	a.mu.Lock()
	a.ends, a.found = ends, true
	a.mu.Unlock()
	return nil
}

func (a *Auth) endpointsNow() (endpoints, error) {
	a.mu.Lock()
	ends, found := a.ends, a.found
	a.mu.Unlock()
	if found {
		return ends, nil
	}
	if err := a.discover(); err != nil {
		return endpoints{}, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.ends, nil
}

// ---------------------------------------------------------------- sessions

const cookieName = "okt"

// User is who is making this request, or nil for nobody.
func (a *Auth) User(r *http.Request) *users.User {
	if !a.Configured() {
		u := a.local
		return &u
	}
	c, err := r.Cookie(cookieName)
	if err != nil {
		return nil
	}
	k := key(c.Value)
	a.mu.Lock()
	defer a.mu.Unlock()
	s, ok := a.sessions[k]
	if !ok {
		return nil
	}
	if time.Now().After(s.till) {
		delete(a.sessions, k)
		return nil
	}
	u := s.user
	return &u
}

func (a *Auth) start(w http.ResponseWriter, r *http.Request, u users.User) {
	token := random(32)
	a.mu.Lock()
	a.sessions[key(token)] = session{user: u, till: time.Now().Add(sessionLife)}
	// Nothing clears these on its own any more, so the sweep here is the only
	// thing keeping a server that has been up for months from holding sessions
	// nobody will come back to.
	for k, s := range a.sessions {
		if time.Now().After(s.till) {
			delete(a.sessions, k)
		}
	}
	a.mu.Unlock()
	a.persist()

	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure(r),
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(sessionLife),
	})
}

// Middleware turns away anybody who is not signed in. A page request is sent to
// the sign-in screen; anything else is refused outright, because a fetch that
// lands on a login page is a confusing way to be told to log in.
func (a *Auth) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/logg-") || strings.HasPrefix(r.URL.Path, "/static/") {
			next.ServeHTTP(w, r)
			return
		}
		// One hook, and deliberately one: what may be read without an account
		// is a question about pages, which this package knows nothing about.
		// A list of paths here would be the same decision made in the place
		// least able to justify it, and would drift from whatever set it.
		if a.open != nil && a.open(r) {
			next.ServeHTTP(w, r)
			return
		}
		if a.User(r) != nil {
			next.ServeHTTP(w, r)
			return
		}
		if r.Method != "GET" || r.Header.Get("HX-Request") != "" ||
			strings.Contains(r.Header.Get("Accept"), "application/json") {
			http.Error(w, "ikkje innlogga", http.StatusUnauthorized)
			return
		}
		http.Redirect(w, r, "/logg-inn?attende="+url.QueryEscape(r.URL.RequestURI()), http.StatusSeeOther)
	})
}

// ------------------------------------------------------------------ routes

// Routes registers everything under /logg-. The screen at /logg-inn is not
// registered here: it is a page of the app like any other, drawn with the app's
// own chrome, and lives with the other page handlers. Only the step that
// actually leaves for the provider is here.
func (a *Auth) Routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /logg-inn/start", a.handleStart)
	mux.HandleFunc("GET /logg-inn/attende", a.handleCallback)
	mux.HandleFunc("POST /logg-ut", a.handleLogout)
	mux.HandleFunc("GET /logg-ut", a.handleLogout)
}

// Back reads the "where was I going" parameter, refusing anything that is not a
// path inside this app — an open redirect is a login page's classic mistake.
func Back(r *http.Request) string {
	back := r.URL.Query().Get("attende")
	if !strings.HasPrefix(back, "/") || strings.HasPrefix(back, "//") {
		return "/"
	}
	return back
}

// handleStart leaves for the provider.
func (a *Auth) handleStart(w http.ResponseWriter, r *http.Request) {
	if !a.Configured() {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	ends, err := a.endpointsNow()
	if err != nil {
		log.Printf("auth: %v", err)
		http.Error(w, "får ikkje kontakt med innloggingstenesta", http.StatusServiceUnavailable)
		return
	}

	// PKCE. The verifier never leaves this process; the provider only ever
	// sees its hash, so a stolen authorization code is not enough to redeem.
	verifier := random(32)
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])

	state := random(16)
	back := Back(r)
	a.mu.Lock()
	a.pending[state] = pending{verifier: verifier, back: back, till: time.Now().Add(10 * time.Minute)}
	for k, p := range a.pending {
		if time.Now().After(p.till) {
			delete(a.pending, k)
		}
	}
	a.mu.Unlock()

	q := url.Values{
		"response_type":         {"code"},
		"client_id":             {a.cfg.ClientID},
		"redirect_uri":          {a.redirect(r)},
		"scope":                 {"openid profile email"},
		"state":                 {state},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
	}
	http.Redirect(w, r, ends.Authorization+"?"+q.Encode(), http.StatusSeeOther)
}

func (a *Auth) handleCallback(w http.ResponseWriter, r *http.Request) {
	if !a.Configured() {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	if msg := r.URL.Query().Get("error"); msg != "" {
		http.Error(w, "innlogginga vart avvist: "+msg, http.StatusForbidden)
		return
	}
	state, code := r.URL.Query().Get("state"), r.URL.Query().Get("code")

	a.mu.Lock()
	p, ok := a.pending[state]
	delete(a.pending, state)
	a.mu.Unlock()
	if !ok || time.Now().After(p.till) {
		// Also what an old tab, or somebody else's link, looks like.
		http.Error(w, "innlogginga tok for lang tid — prøv på nytt", http.StatusBadRequest)
		return
	}

	token, err := a.exchange(r, code, p.verifier)
	if err != nil {
		log.Printf("auth: token exchange: %v", err)
		http.Error(w, "kunne ikkje fullføre innlogginga", http.StatusBadGateway)
		return
	}
	u, err := a.userinfo(token)
	if err != nil {
		log.Printf("auth: userinfo: %v", err)
		http.Error(w, "kunne ikkje hente brukaren", http.StatusBadGateway)
		return
	}
	stored, err := a.users.Upsert(u)
	if err != nil {
		log.Printf("auth: could not record user: %v", err)
		stored = u // a file that will not write is not a reason to refuse entry
	}
	a.start(w, r, stored)
	http.Redirect(w, r, p.back, http.StatusSeeOther)
}

func (a *Auth) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(cookieName); err == nil {
		a.mu.Lock()
		delete(a.sessions, key(c.Value))
		a.mu.Unlock()
		// Signing out has to outlive the process too, or a restart would hand
		// the session back to whoever still had the cookie.
		a.persist()
	}
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: secure(r), SameSite: http.SameSiteLaxMode,
	})
	// Only this app's session is ended. Signing out of the provider itself is
	// its own business, and doing it from here would sign somebody out of
	// everything else they had open.
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (a *Auth) exchange(r *http.Request, code, verifier string) (string, error) {
	ends, err := a.endpointsNow()
	if err != nil {
		return "", err
	}
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {a.redirect(r)},
		"client_id":     {a.cfg.ClientID},
		"code_verifier": {verifier},
	}
	if a.cfg.ClientSecret != "" {
		form.Set("client_secret", a.cfg.ClientSecret)
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "POST", ends.Token, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	var out struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return "", err
	}
	if out.AccessToken == "" {
		return "", fmt.Errorf("no access token (%s %s)", res.Status, out.Error)
	}
	return out.AccessToken, nil
}

// userinfo asks the provider who the token belongs to. Claim names follow the
// OIDC standard; `preferred_username` is what becomes the address of somebody's
// page, and the fallbacks are for providers that do not send one.
func (a *Auth) userinfo(token string) (users.User, error) {
	ends, err := a.endpointsNow()
	if err != nil {
		return users.User{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", ends.UserInfo, nil)
	if err != nil {
		return users.User{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return users.User{}, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return users.User{}, fmt.Errorf("userinfo: %s", res.Status)
	}
	var claims struct {
		Sub      string `json:"sub"`
		Name     string `json:"name"`
		Username string `json:"preferred_username"`
		Email    string `json:"email"`
	}
	if err := json.NewDecoder(res.Body).Decode(&claims); err != nil {
		return users.User{}, err
	}
	if claims.Sub == "" {
		return users.User{}, errors.New("userinfo without a subject")
	}

	login := claims.Username
	if login == "" {
		login, _, _ = strings.Cut(claims.Email, "@")
	}
	if login == "" {
		login = claims.Name
	}
	if login == "" {
		login = claims.Sub
	}
	name := claims.Name
	if name == "" {
		name = login
	}
	return users.User{Sub: claims.Sub, Login: doc.Slug(login), Name: name, Email: claims.Email}, nil
}

// redirect is where the provider sends people back to. Taken from the
// configuration when there is one, and otherwise from the request — which is
// what works behind a proxy, and is why the proxy has to set X-Forwarded-Proto.
func (a *Auth) redirect(r *http.Request) string {
	if a.cfg.BaseURL != "" {
		return a.cfg.BaseURL + "/logg-inn/attende"
	}
	scheme := "http"
	if secure(r) {
		scheme = "https"
	}
	host := r.Header.Get("X-Forwarded-Host")
	if host == "" {
		host = r.Host
	}
	return scheme + "://" + host + "/logg-inn/attende"
}

func secure(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func random(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand does not fail in practice, and a session token that is
		// not random is worse than no session at all.
		panic("auth: no randomness: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
