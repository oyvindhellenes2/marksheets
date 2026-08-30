package render

import (
	"strings"

	"marksheets/internal/doc"
)

// Target is what an @-query points at: a page, and optionally one node in it.
type Target struct {
	Page string
	Node string // node id, empty when the query addresses the whole page
}

// String is the stored form of a target: "gym#n_gymeq", or "gym" for a page.
func (t Target) String() string {
	if t.Node == "" {
		return t.Page
	}
	return t.Page + "#" + t.Node
}

// ParseTarget reads the stored form back.
func ParseTarget(s string) Target {
	page, node, _ := strings.Cut(s, "#")
	return Target{Page: page, Node: node}
}

// Queries returns every @-query written in a node's text fields, in the exact
// form it appears, so it can be used as a key and rewritten in place.
func (r *Renderer) Queries(n *doc.Node) []string {
	td := r.reg.Get(n.Type)
	if td == nil {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	for _, fd := range td.Fields {
		if fd.Kind != "richtext" && fd.Kind != "text" {
			continue
		}
		for _, raw := range QueriesIn(n.Str(fd.Name)) {
			if !seen[raw] {
				seen[raw] = true
				out = append(out, raw)
			}
		}
	}
	return out
}

// QueriesIn returns the @-queries in a string, exactly as written.
func QueriesIn(s string) []string {
	var out []string
	for _, loc := range queryRe.FindAllStringSubmatchIndex(s, -1) {
		q, consumed := queryAt(s, loc)
		if len(q.segs) == 0 {
			continue
		}
		out = append(out, s[loc[0]:loc[0]+consumed])
	}
	return out
}

// Resolve turns a raw query string into the node it addresses, by slug. This
// is what a save uses to record a durable id for a link the user wrote.
func (r *Renderer) Resolve(raw string) (Target, bool) {
	loc := queryRe.FindStringSubmatchIndex(raw)
	if loc == nil {
		return Target{}, false
	}
	q, _ := queryAt(raw, loc)
	if len(q.segs) == 0 {
		return Target{}, false
	}

	res, err := r.resolve(q)
	if err != nil {
		return Target{}, false
	}
	t := Target{Page: res.page}
	// Record the node the path landed on, filter or not. For a filtered query
	// that node is the scope the filter runs against, and remembering it by id
	// is what lets the query survive its scope being renamed.
	if res.node != nil {
		t.Node = res.node.ID
	}
	return t, true
}

// PathTo builds the readable query path for a node inside a page, so a link
// can be rewritten to match a heading's current name.
func PathTo(pageSlug string, d *doc.Doc, nodeID string) (string, bool) {
	var found []string
	var walk func(nodes []*doc.Node, trail []string) bool
	walk = func(nodes []*doc.Node, trail []string) bool {
		for _, n := range nodes {
			here := append(append([]string{}, trail...), doc.Slug(n.Label()))
			if n.ID == nodeID {
				found = here
				return true
			}
			if walk(n.Items, here) || walk(n.Children, here) {
				return true
			}
		}
		return false
	}
	if !walk(d.Children, nil) {
		return "", false
	}
	return "@" + pageSlug + "/" + strings.Join(found, "/"), true
}

// Requery rewrites a query to use a fresh path while keeping its filter, so
// `@gym/gym-equipment[#øyvind]` becomes `@gym/utstyr[#øyvind]`.
func Requery(raw, newPath string) string {
	// Peel off any link text first and put it back at the end, so renaming a
	// heading cannot swallow the name someone gave the link.
	label := ""
	if i := strings.LastIndexByte(raw, '('); i >= 0 && strings.HasSuffix(raw, ")") {
		label, raw = raw[i:], raw[:i]
	}
	if i := strings.IndexByte(raw, '['); i >= 0 {
		return newPath + raw[i:] + label
	}
	// The dot form writes its tag as a trailing path segment.
	if i := strings.LastIndexAny(raw, "./"); i >= 0 && i+1 < len(raw) && raw[i+1] == '#' {
		return newPath + "/" + raw[i+1:] + label
	}
	return newPath + label
}

// FindNode returns the node with the given id.
func FindNode(d *doc.Doc, id string) *doc.Node {
	var found *doc.Node
	d.Walk(func(n *doc.Node, _ int) {
		if found == nil && n.ID == id {
			found = n
		}
	})
	return found
}
