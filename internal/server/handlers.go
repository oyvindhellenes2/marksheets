package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"marksheets/internal/doc"
	"marksheets/internal/files"
	"marksheets/internal/pages"
	"marksheets/internal/render"
	"marksheets/internal/vcs"
)

// Tag is one hashtag on the home page's index, with how many pages carry it
// and the link that filters down to them.
type Tag struct {
	Name  string
	Count int
	Link  string
}

type homeData struct {
	Pages []*pages.Page
	// Tags is every tag in use, counted, whatever the current filter is — you
	// have to be able to switch from one to another without going back first.
	Tags []Tag
	// Active is the tag being filtered on, empty for everything.
	Active string
	// Repo is the git root holding the pages, empty when there is none.
	Repo string
	// Unpublished holds the slugs whose file differs from what has been
	// published. Computed from git on every request, never stored — the same
	// reasoning as backlinks: an answer worked out on demand cannot go stale.
	Unpublished map[string]bool
}

func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	own, tags, active, err := s.index(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.render(w, "home.html", homeData{
		Pages:       own,
		Tags:        tags,
		Active:      active,
		Repo:        s.repoRoot(),
		Unpublished: s.unpublishedSlugs(),
	})
}

// index is the front page's list: the pages worth showing, every tag in use,
// and which tag is being filtered on.
//
// A task page is the working file of one task and is reached through that task
// alone, so it is left out of both — it would put a page the index hides behind
// a tag that leads to it.
func (s *Server) index(r *http.Request) ([]*pages.Page, []Tag, string, error) {
	list, err := s.pages.List()
	if err != nil {
		return nil, nil, "", err
	}
	active := doc.Slug(r.URL.Query().Get("emne"))

	count := map[string]int{}
	own := make([]*pages.Page, 0, len(list))
	for _, p := range list {
		if p.Hidden() {
			continue
		}
		for _, t := range p.Tags {
			count[t]++
		}
		if active != "" && !slices.Contains(p.Tags, active) {
			continue
		}
		own = append(own, p)
	}

	tags := make([]Tag, 0, len(count))
	for name, c := range count {
		tags = append(tags, Tag{Name: name, Count: c, Link: "/?emne=" + url.QueryEscape(name)})
	}
	// Most used first, then alphabetical — the tags worth clicking come to the
	// front, and ties do not shuffle between page loads.
	sort.Slice(tags, func(i, j int) bool {
		if tags[i].Count != tags[j].Count {
			return tags[i].Count > tags[j].Count
		}
		return tags[i].Name < tags[j].Name
	})
	return own, tags, active, nil
}

func (s *Server) handleCreate(w http.ResponseWriter, r *http.Request) {
	title := strings.TrimSpace(r.FormValue("title"))
	if title == "" {
		title = "Ny side"
	}
	// Tags are asked for when the page is made, because that is the moment
	// somebody knows what the page is for. An empty field falls back to the
	// page's own name in Create, so the page is never left with none.
	p, err := s.pages.Create(title, doc.ParseTags(r.FormValue("tags")))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// An HTTP header is Latin-1 by spec, so a slug carrying a Norwegian letter
	// has to be percent-encoded before it goes into one. Raw UTF-8 reaches the
	// browser as mojibake — «blåboksen» arrives as "blÃ¥boksen" — and the page
	// it then asks for does not exist. http.Redirect escapes Location itself;
	// HX-Redirect is ours to escape.
	target := "/p/" + url.PathEscape(p.Slug)
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
	// The list comes back filtered the same way it went out: deleting a page
	// while looking at one tag should not drop you back into all of them.
	own, tags, active, err := s.index(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.renderPartial(w, "page-list-partial.html", homeData{
		Pages:       own,
		Tags:        tags,
		Active:      active,
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
		"tags":     p.Doc.Tags,
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
		// What was stored, not what was sent: a save that arrived with no tags
		// gets one, and the editor has to hear about it.
		"tags": p.Tags,
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
	// Title and Tags are what the page carried at that commit, which is not
	// necessarily what it carries now.
	Title    string
	Tags     []string
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
		Tags:     d.Tags,
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

// publishSet is the page, the working files only it can reach, and the
// attachments any of them show. Publishing a page without them would put links
// to nothing — and pictures with holes in them — in front of everyone else.
func (s *Server) publishSet(slug string) []string {
	set := []string{slug + ".json"}
	if p, err := s.pages.BySlug(slug); err == nil && p.OK() {
		set = append(set, s.pages.FilesOn(p.Doc)...)
	}
	list, err := s.pages.List()
	if err != nil {
		return set
	}
	for _, p := range list {
		if p.Parent == "" {
			continue
		}
		if owner, _, _ := strings.Cut(p.Parent, "#"); owner == slug {
			set = append(set, p.Slug+".json")
			if p.OK() {
				set = append(set, s.pages.FilesOn(p.Doc)...)
			}
		}
	}
	return set
}

// handleUpload stores a file and hands back the name it was kept under, which
// is not necessarily the one it arrived with.
func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, files.MaxUpload+(1<<20))
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		http.Error(w, "kunne ikkje lese opplastinga: "+err.Error(), http.StatusBadRequest)
		return
	}
	src, header, err := r.FormFile("fil")
	if err != nil {
		http.Error(w, "inga fil i opplastinga", http.StatusBadRequest)
		return
	}
	defer src.Close()

	name, size, err := s.pages.SaveFile(header.Filename, src)
	if errors.Is(err, pages.ErrTooBig) {
		http.Error(w, "fila er for stor (maks "+files.HumanSize(files.MaxUpload)+")", http.StatusRequestEntityTooLarge)
		return
	}
	if err != nil {
		log.Printf("upload %s: %v", header.Filename, err)
		http.Error(w, "kunne ikkje lagre fila", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"file":     name,
		"original": header.Filename,
		"size":     size,
		"human":    files.HumanSize(size),
	})
}

// handleFile serves a stored file.
//
// The content type comes from a short allowlist rather than from sniffing, and
// anything not on it is sent as a download. An uploaded file is served from the
// same origin as the app, so a type the browser will execute — an SVG, an HTML
// page — would be running on this origin if it were shown inline.
func (s *Server) handleFile(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	f, info, err := s.pages.OpenFile(name)
	if errors.Is(err, pages.ErrNotFound) || errors.Is(err, pages.ErrBadSlug) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer f.Close()

	ctype, inline := files.ServeType(name)
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if !inline {
		w.Header().Set("Content-Disposition", `attachment; filename="`+files.StoredName(name)+`"`)
	}
	http.ServeContent(w, r, name, info.ModTime(), f)
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
