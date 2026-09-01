package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"

	"marksheets/internal/doc"
	"marksheets/internal/pages"
	"marksheets/internal/render"
	"marksheets/internal/vcs"
)

type homeData struct {
	Pages []*pages.Page
	// Repo is the git root holding the pages, empty when there is none.
	Repo string
	// Unpublished holds the slugs whose file differs from what has been
	// published. Computed from git on every request, never stored — the same
	// reasoning as backlinks: an answer worked out on demand cannot go stale.
	Unpublished map[string]bool
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
	s.render(w, "home.html", homeData{
		Pages:       own,
		Repo:        s.repoRoot(),
		Unpublished: s.unpublishedSlugs(),
	})
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
	// Working files are reached through their task alone, so they are left out
	// here for the same reason handleHome leaves them out.
	own := make([]*pages.Page, 0, len(list))
	for _, p := range list {
		if !p.Hidden() {
			own = append(own, p)
		}
	}
	s.renderPartial(w, "page-list-partial.html", homeData{
		Pages:       own,
		Unpublished: s.unpublishedSlugs(),
	})
}

type pageData struct {
	Page      *pages.Page
	DocJSON   template.JS
	TypesJSON template.JS
	TasksJSON template.JS
	Rendered  template.HTML
	Backlinks []pages.Backlink
	HasRepo   bool
	// HasRemote is false when there is nowhere to publish to, so the button can
	// say what it will actually do.
	HasRemote bool
	// Unpublished is true when this page holds work nobody else can see yet.
	Unpublished bool
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
		Page:        p,
		DocJSON:     template.JS(docJSON),
		TypesJSON:   template.JS(typesJSON),
		TasksJSON:   template.JS(tasksJSON),
		HasRepo:     s.repo != nil,
		HasRemote:   s.repo != nil && s.repo.HasRemote(),
		Unpublished: s.unpublishedSlugs()[p.Slug],
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

// handlePageList is what the editor completes `@` against. Working files are
// left out: they are reachable through their task and nowhere else, so
// offering one here would be a way into a page the front page hides.
func (s *Server) handlePageList(w http.ResponseWriter, r *http.Request) {
	list, err := s.pages.List()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	out := make([]map[string]string, 0, len(list))
	for _, p := range list {
		if p.Hidden() || !p.OK() {
			continue
		}
		out = append(out, map[string]string{"slug": p.Slug, "title": p.Title})
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}

// handleLookup answers two questions about a query path at once: does it
// address anything, and what may follow it. The editor uses the first to mark a
// query that resolves and the second to complete the next segment.
func (s *Server) handleLookup(w http.ResponseWriter, r *http.Request) {
	children, ok := s.renderer.Lookup(r.URL.Query().Get("q"))
	if children == nil {
		children = []render.Child{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"ok": ok, "born": children})
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
//
// This writes the file and stops there. Committing is a separate, deliberate
// act — see handlePublish — because durability and history want different
// rhythms: you want the first constantly and the second at the points you
// choose. The editor calls this on a timer.
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

	// Remember any rename for the eventual commit message. One publish now
	// covers many saves, so this is the only place the information exists.
	if len(result.Renames) > 0 {
		s.pendingMu.Lock()
		s.pending[slug] = append(s.pending[slug], result.Renames...)
		s.pendingMu.Unlock()
	}
	// Written to disk is not the same as published, and the editor says so.
	resp["unpublished"] = s.repo != nil

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// handlePublish is the deliberate half of saving: commit what is on disk, then
// send it. Until this runs, work exists only on this machine.
func (s *Server) handlePublish(w http.ResponseWriter, r *http.Request) {
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
	if s.repo == nil {
		http.Error(w, "historikk er ikkje slått på for denne mappa", http.StatusConflict)
		return
	}

	resp := map[string]any{}
	if err := s.repo.Commit(s.publishSet(slug), s.publishMessage(slug, p.Title)); err != nil {
		log.Printf("commit %s: %v", slug, err)
		// The file is written and safe either way; only the history failed.
		http.Error(w, "kunne ikkje commite (fila er lagra): "+err.Error(), http.StatusInternalServerError)
		return
	}

	// The commit is made and safe. A push that fails is reported, never allowed
	// to turn into a failed commit — the same layering as save and commit.
	switch err := s.repo.Push(); {
	case err == nil:
		resp["published"] = true
	case errors.Is(err, vcs.ErrNoRemote):
		resp["committed"] = true
		resp["note"] = "lagra i historikk — ingen stad å publisere til"
	default:
		log.Printf("push %s: %v", slug, err)
		resp["committed"] = true
		resp["pushError"] = err.Error()
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// hashRe guards the one place a request names a git object. Anything else
// would hand arbitrary revision syntax to git.
var hashRe = regexp.MustCompile(`^[0-9a-f]{7,40}$`)

type versionData struct {
	Page  *pages.Page
	Entry vcs.Entry
	// Title is the title the page carried at that commit, which is not
	// necessarily the one it carries now.
	Title    string
	Rendered template.HTML
}

// handleHistoryVersion renders a page as it stood at one commit, so you can
// look before deciding to go back to it.
//
// `@`-queries in it are resolved against the pages as they are *now*: only this
// page is being read out of history, and pretending otherwise would mean
// rebuilding the whole folder at that commit.
func (s *Server) handleHistoryVersion(w http.ResponseWriter, r *http.Request) {
	slug, hash := r.PathValue("slug"), r.PathValue("hash")
	p, entry, d, ok := s.versionAt(w, r, slug, hash)
	if !ok {
		return
	}
	s.renderPartial(w, "history-version-partial.html", versionData{
		Page:     p,
		Entry:    entry,
		Title:    d.Title,
		Rendered: s.renderer.Page(slug, d),
	})
}

// handleRestore writes an old version back over the page.
//
// Nothing in history is touched or removed: the old content comes back as an
// ordinary unpublished change, which you then publish like any other. Going
// back is a step forward, so every commit made since is still there.
func (s *Server) handleRestore(w http.ResponseWriter, r *http.Request) {
	slug, hash := r.PathValue("slug"), r.PathValue("hash")
	_, _, _, ok := s.versionAt(w, r, slug, hash)
	if !ok {
		return
	}
	raw, err := s.repo.Show(slug+".json", hash)
	if err != nil {
		http.Error(w, "fann ikkje den versjonen", http.StatusNotFound)
		return
	}
	if err := s.pages.WriteRaw(slug, raw); err != nil {
		log.Printf("restore %s@%s: %v", slug, hash, err)
		http.Error(w, "kunne ikkje skrive fila: "+err.Error(), http.StatusInternalServerError)
		return
	}
	// Those renames described edits this restore has just undone.
	s.pendingMu.Lock()
	delete(s.pending, slug)
	s.pendingMu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"unpublished": true})
}

// versionAt is the shared front door for the two handlers above: it checks the
// page, the repository and the hash, and hands back the parsed old document.
// It has written the response when ok is false.
func (s *Server) versionAt(w http.ResponseWriter, r *http.Request, slug, hash string) (*pages.Page, vcs.Entry, *doc.Doc, bool) {
	var entry vcs.Entry
	p, err := s.pages.BySlug(slug)
	if errors.Is(err, pages.ErrNotFound) || errors.Is(err, pages.ErrBadSlug) {
		http.NotFound(w, r)
		return nil, entry, nil, false
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return nil, entry, nil, false
	}
	if s.repo == nil {
		http.Error(w, "historikk er ikkje slått på for denne mappa", http.StatusConflict)
		return nil, entry, nil, false
	}
	if !hashRe.MatchString(hash) {
		http.Error(w, "ugyldig commit", http.StatusBadRequest)
		return nil, entry, nil, false
	}
	raw, err := s.repo.Show(slug+".json", hash)
	if err != nil {
		http.Error(w, "fann ikkje den versjonen", http.StatusNotFound)
		return nil, entry, nil, false
	}
	d := &doc.Doc{}
	if err := json.Unmarshal(raw, d); err != nil {
		http.Error(w, "den versjonen er ikkje lesbar: "+err.Error(), http.StatusUnprocessableEntity)
		return nil, entry, nil, false
	}
	if strings.TrimSpace(d.Title) == "" {
		d.Title = slug
	}
	d.Normalise(s.types)

	// The message and date are worth showing beside the version, and the log
	// already has them.
	if entries, err := s.repo.History(slug+".json", 200); err == nil {
		for _, e := range entries {
			if strings.HasPrefix(e.Hash, hash) {
				entry = e
				break
			}
		}
	}
	if entry.Hash == "" {
		entry = vcs.Entry{Hash: hash, Short: hash[:min(7, len(hash))]}
	}
	return p, entry, d, true
}

// publishSet is the page plus the working files only it can reach. Publishing a
// page without them would put links to nothing in front of everyone else.
func (s *Server) publishSet(slug string) []string {
	files := []string{slug + ".json"}
	list, err := s.pages.List()
	if err != nil {
		return files
	}
	for _, p := range list {
		if p.Parent == "" {
			continue
		}
		if owner, _, _ := strings.Cut(p.Parent, "#"); owner == slug {
			files = append(files, p.Slug+".json")
		}
	}
	return files
}

// publishMessage describes a publish in one line, preferring the rename that
// happened somewhere along the way — that is the change worth finding again,
// and reverting the commit still undoes the heading and every link together.
func (s *Server) publishMessage(slug, title string) string {
	s.pendingMu.Lock()
	renames := s.pending[slug]
	delete(s.pending, slug)
	s.pendingMu.Unlock()

	switch len(renames) {
	case 0:
		return fmt.Sprintf("%s: publisert", title)
	case 1:
		return fmt.Sprintf("%s: «%s» → «%s»", title, renames[0].From, renames[0].To)
	default:
		return fmt.Sprintf("%s: %d overskrifter endra namn", title, len(renames))
	}
}

// unpublishedSlugs is which pages hold work nobody else can see yet, keyed by
// slug. Always a map, never nil, so a template lookup is a plain miss when
// there is no repository rather than something to guard against.
func (s *Server) unpublishedSlugs() map[string]bool {
	out := map[string]bool{}
	if s.repo == nil {
		return out
	}
	for name := range s.repo.Unpublished() {
		out[strings.TrimSuffix(name, ".json")] = true
	}
	return out
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
