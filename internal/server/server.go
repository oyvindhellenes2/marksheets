package server

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"marksheets/internal/auth"
	"marksheets/internal/doc"
	"marksheets/internal/pages"
	"marksheets/internal/render"
	"marksheets/internal/users"
	"marksheets/internal/vcs"
)

type Server struct {
	templates map[string]*template.Template
	static    embed.FS
	pages     *pages.Store
	types     *doc.Registry
	renderer  *render.Renderer

	// repo is nil when the page folder is not in a git repository. The app
	// works without it; you just get no history.
	repo *vcs.Repo

	// pending remembers the headings renamed on a page since it was last
	// published, so a publish covering many autosaves can still describe a
	// rename cascade the way one save used to. It is a convenience and not a
	// record: losing it on restart costs a good commit message and nothing else.
	pendingMu sync.Mutex
	pending   map[string][]pages.Rename

	auth  *auth.Auth
	users *users.Store

	// Who is on which page, and who saved it last. Both are in memory and both
	// are advisory: presence is a courtesy that makes a collision unlikely, and
	// the check in Store.Save is what makes one harmless. Losing either on a
	// restart costs nothing ([ADR-0021]).
	seenMu sync.Mutex
	seen   map[string]map[string]time.Time
	saver  map[string]string
}

// gone is how long somebody stays "on" a page after their last sign of life.
// The editor says hello every 20 seconds, so this is three missed hellos.
const gone = 70 * time.Second

func New(templates, static embed.FS, store *pages.Store, reg *doc.Registry, repo *vcs.Repo,
	a *auth.Auth, people *users.Store) *Server {
	tmpl, err := parseTemplates(templates, assetStamp(static))
	if err != nil {
		log.Fatalf("templates: %v", err)
	}
	return &Server{
		repo:      repo,
		pending:   map[string][]pages.Rename{},
		templates: tmpl,
		static:    static,
		pages:     store,
		types:     reg,
		renderer:  render.New(store, reg),
		auth:      a,
		users:     people,
		seen:      map[string]map[string]time.Time{},
		saver:     map[string]string{},
	}
}

// me is whoever is making this request. Never nil once the middleware has run,
// but written to survive being called before it — a nil user would otherwise be
// a panic in a template.
func (s *Server) me(r *http.Request) users.User {
	if u := s.auth.User(r); u != nil {
		return *u
	}
	return users.User{}
}

// noteSaver records who wrote a page last, so the next person to be refused can
// be told whose work they would have overwritten.
func (s *Server) noteSaver(slug string, u users.User) {
	s.seenMu.Lock()
	defer s.seenMu.Unlock()
	s.saver[slug] = u.Label()
}

func (s *Server) lastSaver(slug string) string {
	s.seenMu.Lock()
	defer s.seenMu.Unlock()
	return s.saver[slug]
}

// here records that somebody is on a page and reports who else is, most
// recently seen first.
func (s *Server) here(slug string, u users.User) []string {
	s.seenMu.Lock()
	defer s.seenMu.Unlock()

	on := s.seen[slug]
	if on == nil {
		on = map[string]time.Time{}
		s.seen[slug] = on
	}
	if u.Login != "" {
		on[u.Label()] = time.Now()
	}
	var out []string
	for who, at := range on {
		if time.Since(at) > gone {
			delete(on, who)
			continue
		}
		if who != u.Label() {
			out = append(out, who)
		}
	}
	sort.Strings(out) // a stable order, so the line does not shuffle every poll
	return out
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()

	staticFS, err := fs.Sub(s.static, "static")
	if err != nil {
		log.Fatalf("static: %v", err)
	}
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))

	mux.HandleFunc("GET /{$}", s.handleHome)
	mux.HandleFunc("POST /pages", s.handleCreate)
	mux.HandleFunc("DELETE /pages/{slug}", s.handleDelete)
	mux.HandleFunc("GET /p/{slug}", s.handlePage)
	mux.HandleFunc("GET /p/{slug}/view", s.handleView)
	mux.HandleFunc("GET /p/{slug}/doc", s.handleDoc)
	mux.HandleFunc("PUT /p/{slug}", s.handleSave)
	mux.HandleFunc("GET /p/{slug}/historie/{hash}", s.handleHistoryVersion)
	mux.HandleFunc("POST /p/{slug}/gjenopprett/{hash}", s.handleRestore)
	mux.HandleFunc("GET /sider.json", s.handlePageList)
	mux.HandleFunc("GET /oppslag.json", s.handleLookup)
	mux.HandleFunc("GET /typar", s.handleTypes)
	mux.HandleFunc("GET /p/{slug}/historie", s.handleHistory)
	mux.HandleFunc("POST /vcs/init", s.handleVCSInit)
	mux.HandleFunc("POST /publiser", s.handlePublishAll)
	mux.HandleFunc("GET /emne.json", s.handleTagList)
	mux.HandleFunc("GET /brukarar.json", s.handlePeople)
	// Somebody's own page: who they are and what is theirs. A person is
	// addressed by their name at the top level — `/kari` — so the address is
	// the name, the way a page's address is its title.
	mux.HandleFunc("GET /logg-inn", s.handleLogin)
	mux.HandleFunc("GET /{namn}", s.handleProfile)
	mux.HandleFunc("GET /p/{slug}/her", s.handleHere)
	mux.HandleFunc("GET /søk", s.handleSearch)
	mux.HandleFunc("GET /søk/framlegg", s.handleSuggest)
	mux.HandleFunc("GET /sidemeny", s.handleSideIndex)
	mux.HandleFunc("POST /filer", s.handleUpload)
	mux.HandleFunc("GET /filer/{name}", s.handleFile)

	s.auth.Routes(mux)
	return s.auth.Middleware(mux)
}

// assetStamp is a short hash of everything under static/, hung off the asset
// URLs as `?v=`. The stylesheet and the scripts are embedded in the binary, so
// their URLs never change and their content changes on every deploy — and in
// front of this app sits Cloudflare, which caches CSS and JS at its edge for
// hours and tells the browser to hold them for as long. A deploy then looks
// like it did nothing, which is the most confusing way for a change to fail.
//
// A URL that changes with the content is the only version of this that cannot
// go stale, and it costs one walk of an embedded filesystem at boot. WalkDir
// is lexical, so the same files give the same stamp on every start — two
// machines serving the same build agree.
func assetStamp(static embed.FS) string {
	h := sha256.New()
	err := fs.WalkDir(static, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := static.ReadFile(path)
		if err != nil {
			return err
		}
		io.WriteString(h, path)
		h.Write(b)
		return nil
	})
	if err != nil {
		// Nothing here is worth refusing to start over: an unstamped URL is
		// the behaviour this app had before, not a broken page.
		log.Printf("asset stamp: %v — serving unversioned asset URLs", err)
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))[:10]
}

func parseTemplates(fsys embed.FS, stamp string) (map[string]*template.Template, error) {
	funcs := template.FuncMap{
		"formatTime": formatTime,
		// asset "/static/style.css" → "/static/style.css?v=1a2b3c4d5e"
		"asset": func(path string) string {
			if stamp == "" {
				return path
			}
			return path + "?v=" + stamp
		},
	}

	names, err := fs.Glob(fsys, "templates/*.html")
	if err != nil {
		return nil, err
	}
	partials, err := fs.Glob(fsys, "templates/*-partial.html")
	if err != nil {
		return nil, err
	}

	out := map[string]*template.Template{}
	for _, name := range names {
		base := strings.TrimPrefix(name, "templates/")
		if base == "base.html" {
			continue
		}
		// Partials render standalone as HTMX fragments; pages are wrapped in
		// base.html and get every partial parsed alongside them. Parsing each
		// page separately keeps their "content" blocks from colliding.
		if strings.HasSuffix(base, "-partial.html") {
			// Parsed alongside the other partials, not alone: one partial may
			// draw another — the sidebar's index is swapped on its own and
			// also sent back beside an unrelated fragment when a page is
			// deleted, and both spellings should mean the same markup.
			t, err := template.New(base).Funcs(funcs).ParseFS(fsys, partials...)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", base, err)
			}
			out[base] = t
			continue
		}
		files := append([]string{"templates/base.html", name}, partials...)
		t, err := template.New("base.html").Funcs(funcs).ParseFS(fsys, files...)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", base, err)
		}
		out[base] = t
	}
	return out, nil
}

func (s *Server) render(w http.ResponseWriter, r *http.Request, name string, data any) {
	t, ok := s.templates[name]
	if !ok {
		http.Error(w, "ukjend mal: "+name, http.StatusInternalServerError)
		return
	}
	// Every full page is drawn inside the same chrome, so the chrome's data is
	// filled in here rather than by each handler in turn.
	if h, ok := data.(navHolder); ok {
		h.setNav(s.nav(r))
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := t.ExecuteTemplate(w, "base.html", data); err != nil {
		log.Printf("render %s: %v", name, err)
	}
}

func (s *Server) renderPartial(w http.ResponseWriter, name string, data any) {
	t, ok := s.templates[name]
	if !ok {
		http.Error(w, "ukjend mal: "+name, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := t.ExecuteTemplate(w, name, data); err != nil {
		log.Printf("render partial %s: %v", name, err)
	}
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	local := t.Local()
	switch d := time.Since(local); {
	case d < time.Minute:
		return "nettopp"
	case d < time.Hour:
		return fmt.Sprintf("%d min sidan", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%d t sidan", int(d.Hours()))
	default:
		return local.Format("02.01.06")
	}
}
