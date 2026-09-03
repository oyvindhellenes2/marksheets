package server

import (
	"context"
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
	"strconv"
	"strings"
	"time"

	"marksheets/internal/auth"
	"marksheets/internal/doc"
	"marksheets/internal/files"
	"marksheets/internal/pages"
	"marksheets/internal/render"
	"marksheets/internal/share"
	"marksheets/internal/users"
	"marksheets/internal/vcs"
)

// Tag is one hashtag on the home page's index, with how many pages carry it
// and the link that filters down to them.
type Tag struct {
	Name  string
	Count int
	Link  string
}

// navData is the chrome around every page: the index of pages, the tags that
// filter it, and which pages hold work nobody else has seen. It is the front
// page's list turned into furniture — the same question ("which page?") asked
// from wherever you happen to be.
//
// It deliberately leaves out the people index. That one is heading for a
// profile view of its own once there is somebody to be logged in as, and a
// sidebar is not where "who is this about" belongs.
type navData struct {
	Pages       []*pages.Page
	Tags        []Tag
	Active      string
	Unpublished map[string]bool
	// Current is the page being shown, so the list can mark where you are.
	Current string
	// Query is what is in the search box, kept across a search so the results
	// page does not appear to have forgotten what you asked for.
	Query string
	// HasRepo says whether there is a history to publish to, so the sidebar
	// knows whether to offer the button at all.
	HasRepo bool
	// Me is whoever is signed in. Every page shows it, because with more than
	// one person using this, "who am I here" is part of knowing what you are
	// looking at.
	Me users.User
	// SignedIn is false when the app is running with no identity provider at
	// all, where there is nobody to log out and nothing to say.
	SignedIn bool
	// Bare drops the chrome: no header, no index, no footer. The share view is
	// the one screen that wants the page and nothing around it.
	Bare bool
}

// bareKey marks a request as belonging to a screen drawn without the chrome.
// A flag on the request rather than a look at the path: `/p/del` is a page
// called "del" and `/p/del/del` is its share view, and no amount of suffix
// matching tells those two apart honestly.
type bareKey struct{}

func bare(r *http.Request) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), bareKey{}, true))
}

func isBare(r *http.Request) bool {
	v, _ := r.Context().Value(bareKey{}).(bool)
	return v
}

// navHolder is a page's data struct that carries the sidebar. `render` fills it
// in from the request, so no handler has to remember to — a page that forgot
// would simply lose its navigation, which is the sort of thing nobody notices
// until they are lost.
type navHolder interface{ setNav(navData) }

// nav gathers the sidebar. Working files are left out for the same reason the
// front page leaves them out: they are reached through their task and nowhere
// else, so listing one here would be a way into a page the index hides.
func (s *Server) nav(r *http.Request) navData {
	// Nobody is signed in: the chrome is a name and a way to sign in, and the
	// list of pages is not built at all. Making it and then not drawing it
	// would be one template edit away from telling a stranger what the wiki
	// has in it.
	if s.auth.User(r) == nil {
		return navData{SignedIn: s.auth.Configured(), Bare: isBare(r)}
	}
	n := navData{
		Bare:        isBare(r),
		Active:      doc.Slug(r.URL.Query().Get("emne")),
		Unpublished: s.unpublishedSlugs(),
		Current:     r.PathValue("slug"),
		Query:       strings.TrimSpace(r.URL.Query().Get("q")),
		HasRepo:     s.repo != nil,
		Me:          s.me(r),
		SignedIn:    s.auth.Configured(),
	}
	own, tags, active, err := s.index(r)
	if err != nil {
		// The sidebar is furniture: a folder that cannot be listed is worth a
		// line in the log, not an error page in place of the page you asked
		// for.
		log.Printf("nav: %v", err)
		return n
	}
	n.Pages, n.Tags, n.Active = own, tags, active
	return n
}

// emptyData is the one screen left where the list of pages used to be: what a
// brand new folder shows, when there is no page to open.
type emptyData struct {
	// Broken is set when there are files but none of them can be opened, so
	// the empty page does not claim there is nothing there.
	Broken bool
	Nav    navData
}

func (d *emptyData) setNav(n navData) { d.Nav = n }

// handleHome opens the page you were most likely on your way to: the one edited
// most recently.
//
// There is no index page any more. The index is the sidebar, on every page,
// so a screen whose whole job was to list pages was a stop on the way to a page
// and nothing else ([ADR-0019](../../adr/0019-the-front-page-is-a-page.md)).
func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	list, err := s.pages.List()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// List is newest first. A working file is skipped for the same reason it is
	// left out of every other index: it is reached through its task.
	broken := false
	for _, p := range list {
		if p.Hidden() {
			continue
		}
		if !p.OK() {
			broken = true
			continue
		}
		http.Redirect(w, r, "/p/"+url.PathEscape(p.Slug), http.StatusSeeOther)
		return
	}
	s.render(w, r, "tom.html", &emptyData{Broken: broken})
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

// handleDelete removes a page. It is reached from the editor of the page in
// question — the only place a page is now looked at as a whole — so what comes
// back is not a list but somewhere else to be.
func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	if err := s.pages.Delete(r.PathValue("slug")); err != nil &&
		!errors.Is(err, pages.ErrNotFound) && !errors.Is(err, pages.ErrBadSlug) {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// A token must not outlive what it pointed at. Deleting a page takes its
	// share link with it, so the address cannot come back pointing at whatever
	// is written under that slug next.
	s.shares.Forget(r.PathValue("slug"))
	w.Header().Set("HX-Redirect", "/")
	w.WriteHeader(http.StatusOK)
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
	Nav         navData
}

func (d *pageData) setNav(n navData) { d.Nav = n }

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

	data := &pageData{
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
	s.render(w, r, "page.html", data)
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

// handlePeople is everybody who could be given something. The editor offers
// this list and nothing else, which is what makes an owner a real person rather
// than a word somebody typed ([ADR-0020]).
func (s *Server) handlePeople(w http.ResponseWriter, r *http.Request) {
	list := s.users.List()
	out := make([]map[string]string, 0, len(list))
	for _, u := range list {
		out = append(out, map[string]string{"login": u.Login, "name": u.Label()})
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}

// handleTagList is every tag in use, for completing one as it is typed.
func (s *Server) handleTagList(w http.ResponseWriter, r *http.Request) {
	_, tags, _, err := s.index(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	out := make([]string, 0, len(tags))
	for _, t := range tags {
		out = append(out, t.Name)
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

// handleShare draws one page and nothing else: the read view and its contents
// list, with no header, no index and no footer, and with the links into other
// pages struck out. It is what a share link opens.
//
// No backlinks. They are this wiki talking about itself, which is the opposite
// of what somebody follows a link to one page to read.
//
// **This screen is behind the same login as every other.** A link to it is a
// link for somebody who already has a way in; it is not a way in. Making it
// readable without an account is a separate thing to build and a separate thing
// to decide — see the note in SPEC.
func (s *Server) handleShare(w http.ResponseWriter, r *http.Request) {
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
	if !p.OK() {
		http.Error(w, "sida kan ikkje lesast", http.StatusUnprocessableEntity)
		return
	}
	// Rendered the shared way even here, where the reader is signed in: this is
	// the preview, and a preview that showed live links would be showing
	// something other than what it is a preview of.
	s.render(w, bare(r), "del.html", &pageData{
		Page:     p,
		Rendered: s.renderer.Shared(slug, p.Doc),
	})
}

// handleShareLink mints the public link for a page, or hands back the one it
// already has. Pressing the button twice gives the same address — a page that
// grew a second link every time somebody copied it would be a page nobody could
// ever un-share.
func (s *Server) handleShareLink(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	p, err := s.pages.BySlug(slug)
	if errors.Is(err, pages.ErrNotFound) || errors.Is(err, pages.ErrBadSlug) {
		http.NotFound(w, r)
		return
	}
	if err != nil || !p.OK() {
		http.Error(w, "sida kan ikkje delast", http.StatusUnprocessableEntity)
		return
	}
	l, err := s.shares.For(slug, s.me(r).Label())
	if err != nil {
		log.Printf("share %s: %v", slug, err)
		http.Error(w, "kunne ikkje lage delingslenke: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"url":  "/delt/" + l.Token,
		"days": int(share.Life.Hours() / 24),
	})
}

// handleUnshare takes a page's public link back. The address stops working at
// once, and the next press of Del mints a different one.
func (s *Server) handleUnshare(w http.ResponseWriter, r *http.Request) {
	if err := s.shares.Revoke(r.PathValue("slug")); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"shared": false})
}

// handleShared is the public view. The only handler in the app that answers a
// request with no session behind it, which is why it does as little as it can:
// resolve the token, read one page, draw it.
//
// An unknown token is a plain 404 — the same answer a made-up one gets, so
// guessing tells you nothing about which pages exist.
func (s *Server) handleShared(w http.ResponseWriter, r *http.Request) {
	slug, ok := s.shares.Slug(r.PathValue("token"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	p, err := s.pages.BySlug(slug)
	if err != nil || !p.OK() {
		// The page has gone or will not parse. Nothing useful to say to
		// somebody who is not signed in, and no reason to say which it was.
		http.NotFound(w, r)
		return
	}
	s.render(w, bare(r), "del.html", &pageData{
		Page:     p,
		Rendered: s.renderer.Shared(slug, p.Doc),
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

	// The version the editor started from. Empty means the editor is not
	// answering for anything it read, and the save goes through as it always
	// did — that is the restore path, and anything else that writes a whole
	// document without having had one open.
	result, err := s.pages.Save(slug, &d, r.Header.Get("X-Version"))
	var stale pages.ErrStale
	if errors.As(err, &stale) {
		// Somebody else saved while this editor was typing. Nothing is written
		// and nothing is lost: the editor stops saving and says so, and what it
		// holds is still in its draft.
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]any{
			"stale":   true,
			"version": stale.Current.Version,
			"by":      s.lastSaver(slug),
			"savedAt": stale.Current.UpdatedAt.Format(time.RFC3339),
		})
		return
	}
	if err != nil {
		if errors.Is(err, pages.ErrNotFound) || errors.Is(err, pages.ErrBadSlug) {
			http.NotFound(w, r)
			return
		}
		log.Printf("save %s: %v", slug, err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.noteSaver(slug, s.me(r))

	p, err := s.pages.BySlug(slug)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	resp := map[string]any{
		// What the editor must answer for on its next save.
		"version": p.Version,
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

// versionRe reads a message written by publishMessage back apart.
var versionRe = regexp.MustCompile(`^(.*) v(\d+)(?: —.*)?$`)

// publishMessage names a publish: the page, and which version of it this is.
//
// The number counts publishes of a page *under its current name*. Only the
// previous commit is consulted, so a title that changes starts again at v1 —
// a page called something else is, for the purpose of counting, a different
// page, and «Hytta v9» meaning the ninth version of something once called
// «Hyttebok» would be a quiet lie.
//
// A rename that happened along the way is still named after the version. That
// is the change worth finding again in a log, and reverting the commit still
// undoes the heading and every link into it together.
func (s *Server) publishMessage(slug, title string) string {
	s.pendingMu.Lock()
	renames := s.pending[slug]
	delete(s.pending, slug)
	s.pendingMu.Unlock()

	msg := fmt.Sprintf("%s v%d", title, s.nextVersion(slug, title))
	switch len(renames) {
	case 0:
		return msg
	case 1:
		return fmt.Sprintf("%s — «%s» → «%s»", msg, renames[0].From, renames[0].To)
	default:
		return fmt.Sprintf("%s — %d overskrifter endra namn", msg, len(renames))
	}
}

// nextVersion is one past the version the page was last published as, or 1 if
// it has never been published or has been renamed since.
func (s *Server) nextVersion(slug, title string) int {
	if s.repo == nil {
		return 1
	}
	entries, err := s.repo.History(slug+".json", 1)
	if err != nil || len(entries) == 0 {
		return 1
	}
	m := versionRe.FindStringSubmatch(entries[0].Message)
	if m == nil || m[1] != title {
		return 1
	}
	was, err := strconv.Atoi(m[2])
	if err != nil {
		return 1
	}
	return was + 1
}

// handlePublishAll sends everything that has not been sent.
//
// It lives on the home page because that is where it is true. A commit can be
// limited to one page — Repo.Commit stages only what it is given — but a push
// cannot: git sends a whole branch, and there is no way to send part of one.
// That is the same limit that gave the pages a repository of their own
// ([ADR-0006]), and it is why a per-page Publiser button was a promise the app
// could not keep.
//
// So each changed page is still committed on its own, with its own message and
// its own place in that page's history, and the branch is sent once at the end.
func (s *Server) handlePublishAll(w http.ResponseWriter, r *http.Request) {
	if s.repo == nil {
		http.Error(w, "historikk er ikkje slått på for denne mappa", http.StatusConflict)
		return
	}

	// Whoever pressed the button is the author of these commits. With nobody
	// signed in this is empty and git's own identity stands, exactly as before.
	me := s.me(r)
	by := vcs.Author{Name: me.Name, Email: me.Email}
	if !s.auth.Configured() {
		by = vcs.Author{}
	}

	slugs := make([]string, 0, 8)
	for slug := range s.unpublishedSlugs() {
		slugs = append(slugs, slug)
	}
	sort.Strings(slugs) // a stable order makes the history readable

	resp := map[string]any{}
	var done []string
	for _, slug := range slugs {
		p, err := s.pages.BySlug(slug)
		if err != nil || !p.OK() {
			continue
		}
		if err := s.repo.Commit(s.publishSet(slug), s.publishMessage(slug, p.Title), by); err != nil {
			log.Printf("commit %s: %v", slug, err)
			http.Error(w, "kunne ikkje commite "+slug+" (filene er lagra): "+err.Error(),
				http.StatusInternalServerError)
			return
		}
		done = append(done, slug)
	}
	resp["committed"] = len(done)

	// The commits are made and safe. A push that fails is reported, never
	// allowed to turn into a failed commit — the same layering as save/commit.
	switch err := s.repo.Push(); {
	case err == nil:
		resp["published"] = true
	case errors.Is(err, vcs.ErrNoRemote):
		resp["note"] = "lagra i historikk — ingen stad å publisere til"
	default:
		log.Printf("push: %v", err)
		resp["pushError"] = err.Error()
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
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
		// Only a page marks a page. An attachment travels with the page that
		// shows it, and lives in a folder, so it has a slash in its name.
		if strings.Contains(name, "/") || !strings.HasSuffix(name, ".json") {
			continue
		}
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

// profileData is one person's page.
type profileData struct {
	User  users.User
	Found bool
	// Me is true when this is the page of whoever is looking at it.
	Me     bool
	People []users.User
	Groups []pages.OwnerGroup
	Open   int
	Done   int
	Nav    navData
}

func (d *profileData) setNav(n navData) { d.Nav = n }

// loginData is the sign-in screen.
type loginData struct {
	// Back is where the person was going when they were stopped.
	Back string
	// Issuer is the provider's address, shown because being sent to another
	// site is worth saying out loud rather than springing on somebody.
	Issuer string
	Nav    navData
}

func (d *loginData) setNav(n navData) { d.Nav = n }

// handleLogin is the sign-in screen: a page of ours with a button on it, not a
// bounce straight out to the provider.
//
// A redirect would be one fewer click and worse. A session that runs out
// mid-sentence would throw you at another site with no explanation, there would
// be nowhere to put "that took too long, try again", and the app would never
// once say whose door it is sending you to.
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if !s.auth.Configured() || s.auth.User(r) != nil {
		http.Redirect(w, r, auth.Back(r), http.StatusSeeOther)
		return
	}
	s.render(w, r, "logg-inn.html", &loginData{Back: auth.Back(r), Issuer: s.auth.Issuer()})
}

// handleProfile is somebody's own page: who they are and everything with their
// name on it, gathered from every page and grouped by the page it was written
// on ([ADR-0017](../../adr/0017-a-person-is-a-way-in.md)).
//
// It is addressed by the name itself — `/kari` — because that is what the name
// *is* once people are real: an address, the same way a page's title is one.
// This is the view the old `/ansvarleg` page became when there was somebody to
// be logged in as.
func (s *Server) handleProfile(w http.ResponseWriter, r *http.Request) {
	name := doc.Slug(r.PathValue("namn"))
	if name == "" {
		http.NotFound(w, r)
		return
	}
	me := s.me(r)
	data := &profileData{People: s.users.List(), Me: name == me.Login}

	u, ok := s.users.Get(name)
	if !ok {
		// Not somebody we know. Still a real page rather than a 404 when it is
		// a name written on a task — a page from before this person had an
		// account, or from before the names were people at all.
		u = users.User{Login: name, Name: name}
	}
	data.User, data.Found = u, ok

	groups, err := s.pages.AssignedTo(name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	data.Groups = groups
	for _, g := range groups {
		data.Open += len(g.Open)
		data.Done += len(g.Done)
	}
	if !ok && len(groups) == 0 {
		http.NotFound(w, r)
		return
	}
	s.render(w, r, "profil.html", data)
}

// handleHere is the presence line in the editor bar: it records that whoever
// asked is on this page, and answers with who else is.
//
// A poll rather than a socket. It is one small request every twenty seconds
// from an open editor, it needs no connection to keep alive, and being a
// courtesy rather than a lock, it can be wrong for twenty seconds without
// costing anything ([ADR-0021](../../adr/0021-a-save-answers-for-what-it-read.md)).
func (s *Server) handleHere(w http.ResponseWriter, r *http.Request) {
	others := s.here(r.PathValue("slug"), s.me(r))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if len(others) == 0 {
		return
	}
	fmt.Fprintf(w, `<span class="here" title="Er inne på sida no">%s er inne</span>`,
		template.HTMLEscapeString(strings.Join(others, ", ")))
}

// searchData is a search and what it found.
type searchData struct {
	Query string
	Hits  []pages.Hit
	// Lines is how many matching lines were found in all, which is not the
	// number of pages and is the number people mean by "how many".
	Lines int
	Nav   navData
}

func (d *searchData) setNav(n navData) { d.Nav = n }

// handleSearch is the whole scan: every page whose name, tags or lines hold
// what was typed.
//
// It reads the files on every request and keeps no index
// ([ADR-0018](../../adr/0018-search-is-a-scan.md)). An index would be a second
// copy of the notes to keep honest against hand edits and files arriving from
// git, and at this size the scan is the cheaper half of that trade.
func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	data := &searchData{Query: q}
	if q != "" {
		hits, err := s.pages.Search(q)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		data.Hits = hits
		for _, h := range hits {
			data.Lines += len(h.Lines) + h.More
		}
	}
	s.render(w, r, "sok.html", data)
}

// handleSuggest is the list under the search box as it is typed: page names,
// nothing else. Enter goes to the scan above.
func (s *Server) handleSuggest(w http.ResponseWriter, r *http.Request) {
	list, err := s.pages.Suggest(r.URL.Query().Get("q"), 7)
	if err != nil {
		log.Printf("suggest: %v", err)
	}
	s.renderPartial(w, "search-menu-partial.html", map[string]any{
		"Pages": list,
		"Query": strings.TrimSpace(r.URL.Query().Get("q")),
	})
}

// handleSideIndex is the sidebar's index on its own, so a tag can filter it
// without taking you off the page you are reading.
//
// Two fragments, one request: the tags the click was on, and — out of band —
// the pages they narrow. They are not adjacent in the sidebar, and what sits
// between them is a form that may be open with something typed in it.
func (s *Server) handleSideIndex(w http.ResponseWriter, r *http.Request) {
	nav := s.nav(r)
	s.renderPartial(w, "side-tags-partial.html", nav)
	s.renderPartial(w, "side-pages-oob-partial.html", nav)
}

type typesData struct {
	Types  *doc.Registry
	Source string
	Nav    navData
}

func (d *typesData) setNav(n navData) { d.Nav = n }

func (s *Server) handleTypes(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, "types.html", &typesData{Types: s.types, Source: s.types.Source})
}
