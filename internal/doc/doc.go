// Package doc defines the Marksheets document model: an ordered tree of typed
// nodes. Header depth is what gives a document its outline — a header at depth 1
// renders as h1, depth 2 as h2, and so on — but every node carries an explicit
// id so links and transclusions survive renames.
package doc

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"strings"
	"unicode"
)

// Node is a single line in a document. Fields are kept in a map rather than
// named struct fields because the set of types is user-editable in types.json.
type Node struct {
	ID     string
	Type   string
	Fields map[string]any
	// Links records what each @-query in this node resolved to, as
	// "page#nodeid" (or just "page"). It is a hint, not the truth: queries
	// still resolve by slug when it is missing or stale, but while it holds
	// a link survives the target being renamed.
	Links map[string]string
	// Children belong to headers alone — they are what gives a page its
	// outline.
	Children []*Node
	// Page is the task page a task-todo owns, by slug. It is recorded once,
	// when the page is created, and never derived from the text again — so
	// renaming a task never renames or breaks anything.
	Page string
	// Items are the sub-lines of a list or todo. They are part of the line
	// rather than lines of their own: they carry the same fields as their
	// parent, inherit its type, and cannot nest any further. Keeping them
	// here is what makes "only headers nest" true by construction instead of
	// a rule the editor has to enforce on every keystroke.
	Items []*Node
}

// HoldsItems reports whether a type keeps sub-lines inside itself (list, todo)
// rather than holding child lines (header) or nothing at all.
func (r *Registry) HoldsItems(typeName string) bool {
	t := r.Get(typeName)
	return t != nil && t.Nestable && !t.AllowsHeaders
}

// HoldsChildren reports whether a type holds child lines — headers only.
func (r *Registry) HoldsChildren(typeName string) bool {
	t := r.Get(typeName)
	return t != nil && t.Nestable && t.AllowsHeaders
}

// Doc is a whole page. The page title is the root of the tree — depth 0 — so
// the first level of headers inside it renders as h1.
type Doc struct {
	Title string `json:"title"`
	// Parent is "page#nodeid" for a task page: the page and the task-todo it
	// belongs to. Empty on an ordinary page. A page with a parent is reached
	// only through that task, never from the front page.
	Parent   string  `json:"parent,omitempty"`
	Children []*Node `json:"children,omitempty"`
}

// TasksHeading is the heading every page starts with, and the only place new
// todos may be created.
const TasksHeading = "Oppgåver"

// ArchiveHeading collects finished tasks inside the tasks heading.
const ArchiveHeading = "Arkiv"

// Template is the starting content for a new page. A task page is the working
// file for one task, so it gets plain todos; an ordinary page gets tasks,
// each of which opens a working file of its own.
func Template(reg *Registry, isTaskPage bool) []*Node {
	kind := "task"
	if isTaskPage {
		kind = "todo"
	}
	return []*Node{{
		ID:     NewID(),
		Type:   "header",
		Fields: map[string]any{"text": TasksHeading},
		Children: []*Node{{
			ID:     NewID(),
			Type:   kind,
			Fields: reg.Defaults(kind),
		}},
	}}
}

// IsEmpty reports whether a page holds nothing but its template — the test for
// whether a task may be deleted.
func (d *Doc) IsEmpty(reg *Registry) bool {
	empty := true
	d.Walk(func(n *Node, _ int) {
		if !empty {
			return
		}
		switch n.Type {
		case "header":
			// The template's own headings do not count as content.
			t := n.Str("text")
			if t != TasksHeading && t != ArchiveHeading {
				empty = false
			}
		default:
			td := reg.Get(n.Type)
			if td == nil {
				return
			}
			for _, fd := range td.Fields {
				if fd.Kind == "bool" || fd.Kind == "tag" {
					continue // defaults, not content
				}
				if strings.TrimSpace(n.Str(fd.Name)) != "" {
					empty = false
					return
				}
			}
		}
	})
	return empty
}

// TasksOf returns every task-todo on the page.
func (d *Doc) TasksOf() []*Node {
	var out []*Node
	d.Walk(func(n *Node, _ int) {
		if n.Type == "task" {
			out = append(out, n)
		}
	})
	return out
}

// NewID returns a short random node id. Ids are opaque and permanent.
func NewID() string {
	b := make([]byte, 4)
	rand.Read(b)
	return "n_" + hex.EncodeToString(b)
}

// Str returns a field as a string, empty if absent or of another kind.
func (n *Node) Str(field string) string {
	if n.Fields == nil {
		return ""
	}
	s, _ := n.Fields[field].(string)
	return s
}

// Bool returns a field as a bool.
func (n *Node) Bool(field string) bool {
	if n.Fields == nil {
		return false
	}
	b, _ := n.Fields[field].(bool)
	return b
}

// Num returns a field as a float64, and whether it was numeric. JSON round-trips
// numbers as float64, but a value typed into the editor may still be a string.
func (n *Node) Num(field string) (float64, bool) {
	if n.Fields == nil {
		return 0, false
	}
	switch v := n.Fields[field].(type) {
	case float64:
		return v, true
	case json.Number:
		f, err := v.Float64()
		return f, err == nil
	case string:
		var f float64
		if err := json.Unmarshal([]byte(strings.TrimSpace(v)), &f); err == nil {
			return f, true
		}
	}
	return 0, false
}

// Label is the human-readable text of a node, used for slug matching in
// queries: a header's title, a data node's name, otherwise its text.
func (n *Node) Label() string {
	switch n.Type {
	case "data":
		return n.Str("name")
	case "image":
		return n.Str("alt")
	default:
		if s := n.Str("text"); s != "" {
			return s
		}
		return n.Str("name")
	}
}

// Slug normalises a string for use as a path segment in an @-query. Spaces,
// underscores and hyphens all collapse to a hyphen, so "Gym equipment",
// "gym_equipment" and "gym-equipment" are the same segment.
func Slug(s string) string {
	var b strings.Builder
	lastDash := true // suppress a leading dash
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			lastDash = false
		case r == ' ' || r == '_' || r == '-' || r == '\t':
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	return strings.TrimSuffix(b.String(), "-")
}

// Walk calls fn for every node in the tree, depth-first, parents before
// children. Depth starts at 1 for top-level nodes.
func (d *Doc) Walk(fn func(n *Node, depth int)) {
	walk(d.Children, 1, fn)
}

// Walk calls fn for n's descendants (not n itself), depth relative to n.
func (n *Node) Walk(fn func(n *Node, depth int)) {
	walk(n.Children, 1, fn)
}

func walk(nodes []*Node, depth int, fn func(n *Node, depth int)) {
	for _, n := range nodes {
		fn(n, depth)
		// Items are visited like any other line so that queries, tags and
		// backlinks reach them.
		walk(n.Items, depth+1, fn)
		walk(n.Children, depth+1, fn)
	}
}

// Count returns the total number of nodes in the document.
func (d *Doc) Count() int {
	c := 0
	d.Walk(func(*Node, int) { c++ })
	return c
}

// Normalise fills in missing ids and drops children from types that cannot
// nest, so a hand-edited or seeded document is always well-formed.
func (d *Doc) Normalise(reg *Registry) {
	d.Children = normalise(d.Children, reg)
}

func normalise(nodes []*Node, reg *Registry) []*Node {
	out := make([]*Node, 0, len(nodes))
	for _, n := range nodes {
		if n == nil {
			continue
		}
		if n.ID == "" {
			n.ID = NewID()
		}
		if n.Fields == nil {
			n.Fields = map[string]any{}
		}
		if len(n.Links) == 0 {
			n.Links = nil
		}
		if reg.Get(n.Type) == nil {
			n.Type = "text"
		}

		switch {
		case reg.HoldsChildren(n.Type):
			// A header takes anything. Stray items are lines that belong
			// under it.
			n.Children = normalise(append(n.Items, n.Children...), reg)
			n.Items = nil
			out = append(out, n)

		case reg.HoldsItems(n.Type):
			// A list or todo absorbs whatever was nested under it as items,
			// one level deep. Older files stored these as children, so this
			// doubles as their migration.
			n.Items = asItems(n, append(n.Items, n.Children...), reg)
			n.Children = nil
			out = append(out, n)

		default:
			// Nothing nests here. Rather than discard what was nested — the
			// mistake that once deleted real content — hand it up a level.
			orphans := append(n.Items, n.Children...)
			n.Items, n.Children = nil, nil
			out = append(out, n)
			out = append(out, normalise(orphans, reg)...)
		}
	}
	return out
}

// asItems turns nested nodes into sub-lines of parent. Items inherit the
// parent's type and cannot nest, so anything nested inside them is flattened
// into the same list instead of being dropped.
func asItems(parent *Node, nested []*Node, reg *Registry) []*Node {
	var out []*Node
	for _, n := range nested {
		if n == nil {
			continue
		}
		deeper := append(n.Items, n.Children...)
		if n.ID == "" {
			n.ID = NewID()
		}
		if n.Fields == nil {
			n.Fields = map[string]any{}
		}
		if len(n.Links) == 0 {
			n.Links = nil
		}
		n.Type = parent.Type // items inherit; they carry no type of their own
		n.Items, n.Children = nil, nil
		out = append(out, n)
		out = append(out, asItems(parent, deeper, reg)...)
	}
	return out
}
