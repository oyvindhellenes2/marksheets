package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strings"
	"time"

	"marksheets/internal/doc"
	"marksheets/internal/pages"
	"marksheets/internal/vcs"
)

type homeData struct {
	Pages []*pages.Page
	// Repo is the git root holding the pages, empty when there is none.
	Repo string
}

func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	list, err := s.pages.List()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// A task page is the working file of one task and is reached through that
	// task alone, so it does not belong in the index.
	own := make([]*pages.Page, 0, len(list))
	for _, p := range list {
		if !p.Hidden() {
			own = append(own, p)
		}
	}
	s.render(w, "home.html", homeData{Pages: own, Repo: s.repoRoot()})
}

func (s *Server) handleCreate(w http.ResponseWriter, r *http.Request) {
	title := strings.TrimSpace(r.FormValue("title"))
	if title == "" {
		title = "Ny side"
	}
	p, err := s.pages.Create(title)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	target := "/p/" + p.Slug
	if r.Header.Get("HX-Request") != "" {
		w.Header().Set("HX-Redirect", target)
		w.WriteHeader(http.StatusOK)
		return
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	if err := s.pages.Delete(r.PathValue("slug")); err != nil &&
		!errors.Is(err, pages.ErrNotFound) && !errors.Is(err, pages.ErrBadSlug) {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	list, err := s.pages.List()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.renderPartial(w, "page-list-partial.html", homeData{Pages: list})
}

type pageData struct {
	Page      *pages.Page
	DocJSON   template.JS
	TypesJSON template.JS
	TasksJSON template.JS
	Rendered  template.HTML
	Backlinks []pages.Backlink
	HasRepo   bool
	// Parent is the page this one belongs to, when it is a task's working file.
	Parent      string
	ParentTitle string
}

func (s *Server) handlePage(w http.ResponseWriter, r *http.Request) {
	p, err := s.pages.BySlug(r.PathValue("slug"))
	if errors.Is(err, pages.ErrNotFound) || errors.Is(err, pages.ErrBadSlug) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !p.OK() {
		http.Error(w, "kan ikkje opne sida: "+p.Err, http.StatusUnprocessableEntity)
		return
	}

	docJSON, err := json.Marshal(p.Doc)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	typesJSON, err := s.types.JSON()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	tasksJSON, err := json.Marshal(s.pages.TaskStates(p.Doc))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	data := pageData{
		Page:      p,
		DocJSON:   template.JS(docJSON),
		TypesJSON: template.JS(typesJSON),
		TasksJSON: template.JS(tasksJSON),
		HasRepo:   s.repo != nil,
	}
	if p.Parent != "" {
		data.Parent, _, _ = strings.Cut(p.Parent, "#")
		data.ParentTitle = data.Parent
		if parent, err := s.pages.BySlug(data.Parent); err == nil {
			data.ParentTitle = parent.Title
		}
	}
	s.render(w, "page.html", data)
}

// handleDoc returns the stored document, so the editor can reload after the
// server has rewritten links.
func (s *Server) handleDoc(w http.ResponseWriter, r *http.Request) {
	p, err := s.pages.BySlug(r.PathValue("slug"))
	if errors.Is(err, pages.ErrNotFound) || errors.Is(err, pages.ErrBadSlug) {
		http.NotFound(w, r)
		return
	}
	if err != nil || !p.OK() {
		http.Error(w, "kan ikkje lese sida", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"title":    p.Doc.Title,
		"children": p.Doc.Children,
		"tasks":    s.pages.TaskStates(p.Doc),
	})
}

// handleView returns the read view as an HTMX fragment, with @-queries resolved.
func (s *Server) handleView(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	p, err := s.pages.BySlug(slug)
	if errors.Is(err, pages.ErrNotFound) || errors.Is(err, pages.ErrBadSlug) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	back, err := s.pages.Backlinks(slug)
	if err != nil {
		log.Printf("backlinks %s: %v", slug, err)
	}
	s.renderPartial(w, "read-partial.html", pageData{
		Page:      p,
		Rendered:  s.renderer.Page(slug, p.Doc),
		Backlinks: back,
	})
}

// handleSave takes the whole document from the editor. The editor owns the
// tree, so this is a full replace rather than a patch.
func (s *Server) handleSave(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")

	var d doc.Doc
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20)).Decode(&d); err != nil {
		http.Error(w, "ugyldig dokument: "+err.Error(), http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(d.Title) == "" {
		d.Title = "Utan tittel"
	}

	result, err := s.pages.Save(slug, &d)
	if err != nil {
		if errors.Is(err, pages.ErrNotFound) || errors.Is(err, pages.ErrBadSlug) {
			http.NotFound(w, r)
			return
		}
		log.Printf("save %s: %v", slug, err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	p, err := s.pages.BySlug(slug)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	resp := map[string]any{
		"savedAt": p.UpdatedAt.Format(time.RFC3339),
		"title":   p.Title,
	}
	resp["tasks"] = s.pages.TaskStates(p.Doc)
	if n := len(result.Created); n > 0 {
		resp["note"] = fmt.Sprintf("%d %s %s", n, plural(n, "oppgåveside", "oppgåvesider"), plural(n, "oppretta", "oppretta"))
	}
	if n := len(result.Kept); n > 0 {
		resp["warning"] = fmt.Sprintf("%d %s hadde innhald og er no vanlege sider: %s",
			n, plural(n, "arbeidsside", "arbeidssider"), strings.Join(result.Kept, ", "))
	}
	if n := len(result.Relinked); n > 0 {
		resp["relinked"] = result.Relinked
		resp["note"] = fmt.Sprintf("%d %s %s", n, plural(n, "side", "sider"), plural(n, "oppdatert", "oppdaterte"))
	}

	// The file is already safely written. A commit that fails is reported,
	// never allowed to turn into a failed save.
	if s.repo != nil {
		if err := s.repo.Commit(result.Files, commitMessage(p.Title, result)); err != nil {
			log.Printf("commit %s: %v", slug, err)
			resp["vcsError"] = err.Error()
		} else {
			resp["committed"] = true
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// commitMessage describes a save in one line: the rename if there was one,
// since that is the change worth being able to find again later.
func commitMessage(title string, r *pages.SaveResult) string {
	if len(r.Renames) == 1 {
		m := fmt.Sprintf("%s: «%s» → «%s»", title, r.Renames[0].From, r.Renames[0].To)
		if n := len(r.Relinked); n > 0 {
			m += fmt.Sprintf(" (%d %s %s)", n, plural(n, "side", "sider"), plural(n, "oppdatert", "oppdaterte"))
		}
		return m
	}
	if n := len(r.Renames); n > 1 {
		return fmt.Sprintf("%s: %d overskrifter endra namn", title, n)
	}
	return fmt.Sprintf("%s: endra", title)
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

func (s *Server) repoRoot() string {
	if s.repo == nil {
		return ""
	}
	return s.repo.Root()
}

type historyData struct {
	Page    *pages.Page
	Entries []vcs.Entry
}

func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	p, err := s.pages.BySlug(slug)
	if errors.Is(err, pages.ErrNotFound) || errors.Is(err, pages.ErrBadSlug) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	data := historyData{Page: p}
	if s.repo != nil {
		if entries, err := s.repo.History(slug+".json", 50); err == nil {
			data.Entries = entries
		} else {
			log.Printf("history %s: %v", slug, err)
		}
	}
	s.renderPartial(w, "history-partial.html", data)
}

// handleVCSInit turns on history by creating a repository in the page folder.
func (s *Server) handleVCSInit(w http.ResponseWriter, r *http.Request) {
	if s.repo != nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	repo, err := vcs.Init(s.pages.Dir())
	if err != nil {
		http.Error(w, "kunne ikkje starte git: "+err.Error(), http.StatusInternalServerError)
		return
	}
	s.repo = repo
	log.Printf("git history enabled in %s", repo.Root())
	w.Header().Set("HX-Refresh", "true")
	w.WriteHeader(http.StatusOK)
}

type typesData struct {
	Types  *doc.Registry
	Source string
}

func (s *Server) handleTypes(w http.ResponseWriter, r *http.Request) {
	s.render(w, "types.html", typesData{Types: s.types, Source: s.types.Source})
}
