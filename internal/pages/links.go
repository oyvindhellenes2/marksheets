package pages

import (
	"fmt"
	"strings"

	"marksheets/internal/doc"
	"marksheets/internal/render"
)

// Backlink is one page linking to another.
type Backlink struct {
	FromPage  string
	FromTitle string
	Query     string
	Context   string // the line the link was written on
}

// recordLinks resolves every @-query in a document and stores the id it points
// at, so the link survives its target being renamed.
func (s *Store) recordLinks(d *doc.Doc) {
	d.Walk(func(n *doc.Node, _ int) {
		queries := s.resolver.Queries(n)
		if len(queries) == 0 {
			n.Links = nil
			return
		}
		links := map[string]string{}
		for _, raw := range queries {
			if t, ok := s.resolver.Resolve(raw); ok {
				links[raw] = t.String()
			} else if old, had := n.Links[raw]; had {
				// Unresolvable right now — a page being renamed, say. Keep
				// what we knew rather than forgetting it.
				links[raw] = old
			}
		}
		if len(links) == 0 {
			links = nil
		}
		n.Links = links
	})
}

// Backlinks returns every link pointing at a page, found by scanning the
// files. Nothing is stored: a computed answer cannot go stale, and at this
// size the scan is far cheaper than keeping a register honest.
func (s *Store) Backlinks(target string) ([]Backlink, error) {
	list, err := s.List()
	if err != nil {
		return nil, err
	}
	var out []Backlink
	for _, p := range list {
		if p.Slug == target || !p.OK() {
			continue
		}
		p.Doc.Walk(func(n *doc.Node, _ int) {
			for raw, stored := range n.Links {
				if render.ParseTarget(stored).Page != target {
					continue
				}
				out = append(out, Backlink{
					FromPage:  p.Slug,
					FromTitle: p.Title,
					Query:     raw,
					Context:   strings.TrimSpace(n.Label()),
				})
			}
		})
	}
	return out, nil
}

// renamed is a node whose label changed in a save.
type renamed struct {
	id       string
	from, to string
	// typ is the node's type. Every node is matched here, because a query can
	// point at any of them — but only a heading rename is worth describing in
	// a commit message, so the type has to survive the trip.
	typ string
}

// findRenames compares two versions of a page and reports nodes, matched by
// id, whose label changed.
func findRenames(old, new *doc.Doc) []renamed {
	before := map[string]string{}
	old.Walk(func(n *doc.Node, _ int) { before[n.ID] = n.Label() })

	var out []renamed
	new.Walk(func(n *doc.Node, _ int) {
		was, ok := before[n.ID]
		if ok && was != n.Label() && doc.Slug(was) != doc.Slug(n.Label()) {
			out = append(out, renamed{id: n.ID, from: was, to: n.Label(), typ: n.Type})
		}
	})
	return out
}

// propagate rewrites the readable text of links pointing into a page whose
// headings changed name.
//
// Links already keep working through their recorded id, so this is cosmetic —
// it stops a query reading `@gym/gym-equipment` after that heading became
// "Utstyr". Every link into the page is recomputed from its stored id rather
// than pattern-matched, so a link that merely passes *through* a renamed
// heading is corrected too, not just one that ends at it.
//
// It returns the pages it changed.
func (s *Store) propagate(pageSlug string, d *doc.Doc, renames []renamed) ([]string, error) {
	if len(renames) == 0 {
		return nil, nil
	}

	list, err := s.List()
	if err != nil {
		return nil, err
	}

	var touched []string
	for _, p := range list {
		if p.Slug == pageSlug || !p.OK() {
			continue
		}
		changed := false
		p.Doc.Walk(func(n *doc.Node, _ int) {
			for raw, stored := range n.Links {
				t := render.ParseTarget(stored)
				if t.Page != pageSlug || t.Node == "" {
					continue
				}
				path, ok := render.PathTo(pageSlug, d, t.Node)
				if !ok {
					continue // target is gone; the link keeps its old text
				}
				fresh := render.Requery(raw, path)
				if fresh == raw || !rewriteQuery(n, raw, fresh) {
					continue
				}
				delete(n.Links, raw)
				n.Links[fresh] = stored
				changed = true
			}
		})
		if !changed {
			continue
		}
		if err := s.write(p.Slug, p.Doc); err != nil {
			return touched, fmt.Errorf("oppdatere lenkjer i %s: %w", p.Slug, err)
		}
		touched = append(touched, p.Slug)
	}
	return touched, nil
}

// rewriteQuery replaces a query with its updated form in every text field of a
// node, reporting whether anything changed.
func rewriteQuery(n *doc.Node, from, to string) bool {
	changed := false
	for name, v := range n.Fields {
		text, ok := v.(string)
		if !ok || !strings.Contains(text, from) {
			continue
		}
		n.Fields[name] = strings.ReplaceAll(text, from, to)
		changed = true
	}
	return changed
}
