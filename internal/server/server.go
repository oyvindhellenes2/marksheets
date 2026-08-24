package server

import (
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"strings"
	"time"

	"marksheets/internal/doc"
	"marksheets/internal/pages"
	"marksheets/internal/render"
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
}

func New(templates, static embed.FS, store *pages.Store, reg *doc.Registry, repo *vcs.Repo) *Server {
	tmpl, err := parseTemplates(templates)
	if err != nil {
		log.Fatalf("templates: %v", err)
	}
	return &Server{
		repo:      repo,
		templates: tmpl,
		static:    static,
		pages:     store,
		types:     reg,
		renderer:  render.New(store, reg),
	}
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
	mux.HandleFunc("GET /typar", s.handleTypes)
	mux.HandleFunc("GET /p/{slug}/historie", s.handleHistory)
	mux.HandleFunc("POST /vcs/init", s.handleVCSInit)

	return mux
}

func parseTemplates(fsys embed.FS) (map[string]*template.Template, error) {
	funcs := template.FuncMap{
		"formatTime": formatTime,
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
			t, err := template.New(base).Funcs(funcs).ParseFS(fsys, name)
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

func (s *Server) render(w http.ResponseWriter, name string, data any) {
	t, ok := s.templates[name]
	if !ok {
		http.Error(w, "ukjend mal: "+name, http.StatusInternalServerError)
		return
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
	if err := t.Execute(w, data); err != nil {
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
