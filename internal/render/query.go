// Package render turns a document into HTML and resolves @-queries.
//
// A query addresses a node by path and optionally filters the nodes under it:
//
//	@gym/gym-equipment/budsjett     a data node   → "10000 kr"
//	@gym.gym_equipment.budsjett     the same, dots instead of slashes
//	@gym/gym-equipment[#øyvind]     every node under it tagged #øyvind
//	@gym.gym_equipment.#øyvind      the same, tag as a trailing segment
//	@gym/gym-equipment[owner=øyvind] the same, matching the field explicitly
//
// Queries are read-only: nothing rendered from a query can be edited in place.
package render

import (
	"fmt"
	"regexp"
	"strings"

	"marksheets/internal/doc"
)

// Source supplies pages to the resolver.
type Source interface {
	// DocBySlug returns the document for a page slug, or ok=false.
	DocBySlug(slug string) (*doc.Doc, bool)
}

// queryRe matches an @-query and its optional bracket filter.
var queryRe = regexp.MustCompile(`@([\p{L}\p{N}_\-./#]+)(?:\[([^\]]*)\])?`)

type query struct {
	raw    string
	segs   []string
	tag    string // set by [#tag] or a trailing .#tag segment
	field  string // set by [field=value]
	value  string
}

// parseQuery splits the captured path and filter into a query. It returns the
// number of bytes of the raw match that actually belong to the query, so a
// trailing sentence full stop is not swallowed into the path.
func parseQuery(path, filter string, hadFilter bool) (query, int) {
	trimmed := strings.TrimRight(path, "./-")
	consumed := len("@") + len(trimmed)
	q := query{raw: "@" + trimmed}

	for _, seg := range strings.FieldsFunc(trimmed, func(r rune) bool { return r == '/' || r == '.' }) {
		if strings.HasPrefix(seg, "#") {
			q.tag = doc.Slug(strings.TrimPrefix(seg, "#"))
			continue
		}
		q.segs = append(q.segs, doc.Slug(seg))
	}

	if hadFilter && len(trimmed) == len(path) {
		consumed += len(filter) + 2
		q.raw += "[" + filter + "]"
		f := strings.TrimSpace(filter)
		switch {
		case strings.HasPrefix(f, "#"):
			q.tag = doc.Slug(strings.TrimPrefix(f, "#"))
		case strings.Contains(f, "="):
			k, v, _ := strings.Cut(f, "=")
			q.field = strings.TrimSpace(k)
			q.value = strings.TrimSpace(strings.Trim(strings.TrimSpace(v), `"'`))
		case f != "":
			q.tag = doc.Slug(f)
		}
	}
	return q, consumed
}

// result is what a query resolved to.
type result struct {
	page    string
	node    *doc.Node // the node the path addressed; nil means the whole page
	nodes   []*doc.Node
	filtered bool
}

// resolve walks the query path and applies any filter.
func (r *Renderer) resolve(q query) (result, error) {
	if len(q.segs) == 0 {
		return result{}, fmt.Errorf("tom spørjing")
	}
	pageSlug := q.segs[0]
	d, ok := r.src.DocBySlug(pageSlug)
	if !ok {
		return result{}, fmt.Errorf("fann inga side som heiter %q", q.segs[0])
	}

	res := result{page: pageSlug, nodes: d.Children}
	for _, seg := range q.segs[1:] {
		n := findChild(res.nodes, seg)
		if n == nil {
			n = findDescendant(res.nodes, seg)
		}
		if n == nil {
			return result{}, fmt.Errorf("fann ikkje %q i %s", seg, pageSlug)
		}
		res.node, res.nodes = n, n.Children
	}

	return r.applyFilter(res, q)
}

// applyFilter narrows a resolved scope to the nodes matching the query's
// filter, if it has one.
func (r *Renderer) applyFilter(res result, q query) (result, error) {
	if q.tag == "" && q.field == "" {
		return res, nil
	}
	scope := res.nodes
	if res.node != nil {
		// filterNodes already recurses, so pass the node alone — passing its
		// children too would visit every descendant twice.
		scope = []*doc.Node{res.node}
	}
	res.nodes = filterNodes(r.reg, scope, q)
	res.filtered = true
	if len(res.nodes) == 0 {
		label := q.tag
		if label == "" {
			label = q.field + "=" + q.value
		}
		return res, fmt.Errorf("ingen treff på %q", label)
	}
	return res, nil
}

// childLines is what sits under a node: a header's children, or a line's items.
func childLines(n *doc.Node) []*doc.Node {
	if len(n.Children) > 0 {
		return n.Children
	}
	return n.Items
}

// findChild looks for a direct child whose label slugs to seg.
func findChild(nodes []*doc.Node, seg string) *doc.Node {
	for _, n := range nodes {
		if doc.Slug(n.Label()) == seg {
			return n
		}
	}
	return nil
}

// findDescendant looks deeper, so a path may skip intermediate headers.
func findDescendant(nodes []*doc.Node, seg string) *doc.Node {
	for _, n := range nodes {
		if found := findChild(n.Items, seg); found != nil {
			return found
		}
		if found := findChild(n.Children, seg); found != nil {
			return found
		}
	}
	for _, n := range nodes {
		if found := findDescendant(append(append([]*doc.Node{}, n.Items...), n.Children...), seg); found != nil {
			return found
		}
	}
	return nil
}

// filterNodes collects every node at or under scope that matches the filter.
func filterNodes(reg *doc.Registry, scope []*doc.Node, q query) []*doc.Node {
	var out []*doc.Node
	var visit func(nodes []*doc.Node)
	visit = func(nodes []*doc.Node) {
		for _, n := range nodes {
			if matches(reg, n, q) {
				out = append(out, n)
			}
			visit(n.Items)
			visit(n.Children)
		}
	}
	visit(scope)
	return out
}

// matches reports whether a node satisfies the filter. A tag matches either a
// field of kind "tag" or a #hashtag written anywhere in the node's text.
func matches(reg *doc.Registry, n *doc.Node, q query) bool {
	if q.field != "" {
		return strings.EqualFold(strings.TrimPrefix(n.Str(q.field), "#"), strings.TrimPrefix(q.value, "#"))
	}
	if q.tag == "" {
		return false
	}
	td := reg.Get(n.Type)
	if td == nil {
		return false
	}
	for _, fd := range td.Fields {
		v := n.Str(fd.Name)
		if v == "" {
			continue
		}
		if fd.Kind == "tag" && doc.Slug(strings.TrimPrefix(v, "#")) == q.tag {
			return true
		}
		for _, t := range hashtagsIn(v) {
			if t == q.tag {
				return true
			}
		}
	}
	return false
}

var hashtagRe = regexp.MustCompile(`#([\p{L}\p{N}_-]+)`)

func hashtagsIn(s string) []string {
	var out []string
	for _, m := range hashtagRe.FindAllStringSubmatch(s, -1) {
		out = append(out, doc.Slug(m[1]))
	}
	return out
}
