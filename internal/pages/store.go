// Package pages stores each document as one JSON file in a folder. The file
// name is the page slug, so `pages/gym.json` is the page you reach with
// `@gym`, and the files stay readable and editable by hand.
package pages

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"marksheets/internal/doc"
	"marksheets/internal/render"
)

var (
	ErrNotFound = errors.New("side finst ikkje")
	ErrBadSlug  = errors.New("ugyldig sidenamn")
)

const ext = ".json"

type Page struct {
	Slug      string
	Title     string
	Doc       *doc.Doc
	UpdatedAt time.Time
	// Err is set when the file could not be read or parsed. Such a page is
	// still listed — a typo in a hand-edited file should be visible, not
	// silently hide the page.
	Err string
	// Parent is set on a task page: the page and task-todo that own it. Such
	// a page is reached only through that task, never from the front page.
	Parent string
}

// Hidden reports whether the page belongs to a task rather than standing on
// its own.
func (p *Page) Hidden() bool { return p.Parent != "" }

// Lines is the number of nodes on the page, shown on the home page.
func (p *Page) Lines() int {
	if p.Doc == nil {
		return 0
	}
	return p.Doc.Count()
}

// OK reports whether the page can be opened.
func (p *Page) OK() bool { return p.Err == "" }

// Store is a folder of JSON documents.
type Store struct {
	dir string
	reg *doc.Registry

	mu    sync.Mutex
	cache map[string]cached

	// resolver reads queries against this same store, so a save can record
	// what each link points at.
	resolver *render.Renderer
}

// cached holds a parsed page along with the file stamp it was parsed from, so
// a file edited outside the app is noticed and re-read.
type cached struct {
	page *Page
	mod  time.Time
	size int64
}

// NewStore opens (and creates, if missing) the page folder.
func NewStore(dir string, reg *doc.Registry) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("page folder %s: %w", dir, err)
	}
	s := &Store{dir: dir, reg: reg, cache: map[string]cached{}}
	s.resolver = render.New(s, reg)
	return s, nil
}

// Dir is the folder the pages live in.
func (s *Store) Dir() string { return s.dir }

// path maps a slug to a file, refusing anything that is not a plain slug.
// doc.Slug strips path separators and dots, so a slug that survives the
// round-trip cannot escape the folder.
func (s *Store) path(slug string) (string, error) {
	if slug == "" || doc.Slug(slug) != slug {
		return "", ErrBadSlug
	}
	return filepath.Join(s.dir, slug+ext), nil
}

// load reads one page, reusing the cached parse when the file has not changed.
func (s *Store) load(slug string) (*Page, error) {
	path, err := s.path(slug)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if c, ok := s.cache[slug]; ok && c.mod.Equal(info.ModTime()) && c.size == info.Size() {
		return c.page, nil
	}

	p := &Page{Slug: slug, Title: slug, UpdatedAt: info.ModTime(), Doc: &doc.Doc{}}
	raw, err := os.ReadFile(path)
	if err != nil {
		p.Err = err.Error()
	} else if err := json.Unmarshal(raw, p.Doc); err != nil {
		p.Err = "ugyldig JSON: " + err.Error()
	} else {
		if strings.TrimSpace(p.Doc.Title) == "" {
			p.Doc.Title = slug
		}
		p.Doc.Normalise(s.reg)
		p.Title = p.Doc.Title
		p.Parent = p.Doc.Parent
	}

	s.cache[slug] = cached{page: p, mod: info.ModTime(), size: info.Size()}
	return p, nil
}

// List returns every page, most recently changed first.
func (s *Store) List() ([]*Page, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, err
	}

	var out []*Page
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ext) {
			continue
		}
		slug := strings.TrimSuffix(e.Name(), ext)
		p, err := s.load(slug)
		if errors.Is(err, ErrBadSlug) {
			// A file whose name is not a slug can never be addressed by a
			// query, so show it as broken rather than pretending it is fine.
			out = append(out, &Page{Slug: slug, Title: e.Name(), Err: "filnamnet er ikkje eit gyldig sidenamn"})
			continue
		}
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out, nil
}

// BySlug returns one page.
func (s *Store) BySlug(slug string) (*Page, error) {
	return s.load(slug)
}

// DocBySlug implements render.Source so @-queries can reach other pages.
func (s *Store) DocBySlug(slug string) (*doc.Doc, bool) {
	p, err := s.load(slug)
	if err != nil || !p.OK() {
		return nil, false
	}
	return p.Doc, true
}

// Create adds a page from the template.
func (s *Store) Create(title string) (*Page, error) {
	return s.create(title, "")
}

// create makes a page. A non-empty parent ("page#nodeid") marks it as the
// working file of one task.
func (s *Store) create(title, parent string) (*Page, error) {
	base := doc.Slug(title)
	if base == "" {
		base = "side"
	}
	d := &doc.Doc{
		Title:    title,
		Parent:   parent,
		Children: doc.Template(s.reg, parent != ""),
	}

	slug := base
	for attempt := 2; ; attempt++ {
		path, err := s.path(slug)
		if err != nil {
			return nil, err
		}
		// O_EXCL makes the uniqueness check and the claim one atomic step.
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err == nil {
			f.Close()
			if err := s.write(slug, d); err != nil {
				return nil, err
			}
			return s.load(slug)
		}
		if !os.IsExist(err) || attempt > 50 {
			return nil, err
		}
		slug = base + "-" + strconv.Itoa(attempt)
	}
}

// SaveResult describes what a save changed, so the caller can report it and
// commit it.
type SaveResult struct {
	// Created are task pages opened by this save.
	Created []string
	// Kept are task pages whose task was removed but which still hold
	// content, so the page was left alone.
	Kept []string
	// Renames are headings whose name changed in this save. Only headings:
	// every node is matched when links are rewritten, but a heading is the only
	// one whose rename is worth putting in a commit message.
	Renames []Rename
	// Relinked are other pages whose links were rewritten to match.
	Relinked []string
	// Files are every file written, relative to the page folder.
	Files []string
}

// Rename is a heading that changed name.
type Rename struct{ From, To string }

// Save replaces a page's document. The title comes from the document itself,
// so renaming a page happens in the editor like every other edit.
//
// Links in the saved page are resolved to ids, and links elsewhere that point
// at a heading renamed here are rewritten to read correctly again.
func (s *Store) Save(slug string, d *doc.Doc) (*SaveResult, error) {
	if _, err := s.path(slug); err != nil {
		return nil, err
	}
	prev, err := s.load(slug)
	if err != nil {
		return nil, err
	}

	// Parent is the store's to keep, not the editor's. The editor sends only
	// title and children, so taking it from the request would clear it on
	// every save — which detaches a task page from the task that owns it.
	if prev.OK() {
		d.Parent = prev.Doc.Parent
	}

	d.Normalise(s.reg)

	created, kept, err := s.syncTasks(slug, prev, d)
	if err != nil {
		return nil, err
	}
	s.recordLinks(d)

	var renames []renamed
	if prev.OK() {
		renames = findRenames(prev.Doc, d)
	}

	if err := s.write(slug, d); err != nil {
		return nil, err
	}

	res := &SaveResult{Files: []string{slug + ext}, Created: created, Kept: kept}
	for _, r := range renames {
		if r.typ != "header" {
			continue
		}
		res.Renames = append(res.Renames, Rename{From: r.from, To: r.to})
	}

	touched, err := s.propagate(slug, d, renames)
	res.Relinked = touched
	for _, t := range touched {
		res.Files = append(res.Files, t+ext)
	}
	return res, err
}

// write saves a document atomically: a full temp file in the same folder,
// then a rename. A crash mid-save can never leave a half-written page, which
// matters more now that the file is the only copy.
func (s *Store) write(slug string, d *doc.Doc) error {
	raw, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	return s.writeBytes(slug, append(raw, '\n'))
}

// WriteRaw puts a file back byte for byte, for restoring an old version from
// git. It goes through the same temp-file-and-rename as an ordinary save, and
// it refuses anything that will not parse — overwriting a good page with
// something unreadable is the one outcome worth failing for.
func (s *Store) WriteRaw(slug string, raw []byte) error {
	var probe doc.Doc
	if err := json.Unmarshal(raw, &probe); err != nil {
		return fmt.Errorf("ugyldig JSON: %w", err)
	}
	return s.writeBytes(slug, raw)
}

func (s *Store) writeBytes(slug string, raw []byte) error {
	path, err := s.path(slug)
	if err != nil {
		return err
	}

	tmp, err := os.CreateTemp(s.dir, "."+slug+".*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())

	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return err
	}

	s.mu.Lock()
	delete(s.cache, slug)
	s.mu.Unlock()
	return nil
}

// Delete removes a page, and with it the working files of its tasks. Those
// pages are reachable only through this one, so leaving them behind would put
// files on disk that nothing in the app could ever open again.
func (s *Store) Delete(slug string) error {
	var tasks []string
	if p, err := s.load(slug); err == nil && p.OK() {
		for _, t := range p.Doc.TasksOf() {
			if t.Page != "" {
				tasks = append(tasks, t.Page)
			}
		}
	}
	if err := s.remove(slug); err != nil {
		return err
	}
	for _, t := range tasks {
		if err := s.remove(t); err != nil && !errors.Is(err, ErrNotFound) {
			return err
		}
	}
	return nil
}

func (s *Store) remove(slug string) error {
	path, err := s.path(slug)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return ErrNotFound
		}
		return err
	}
	s.mu.Lock()
	delete(s.cache, slug)
	s.mu.Unlock()
	return nil
}
