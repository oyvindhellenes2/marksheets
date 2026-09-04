// Package doc defines the Marksheets document model: an ordered tree of typed
// nodes. Header depth is what gives a document its outline — a header at depth 1
// renders as h1, depth 2 as h2, and so on — but every node carries an explicit
// id so links and transclusions survive renames.
package doc

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"slices"
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
	// TaskNo is a task's number on its page: 1, 2, 3, given out in the order the
	// tasks were written and never given out twice.
	//
	// It exists to be said out loud — "look at task 4" — so the one thing it
	// must not do is move. It is therefore stored rather than counted from the
	// position: reordering the list leaves every number where it was, and
	// deleting task 2 leaves a gap rather than shuffling 3 down into it. A
	// number that renumbered itself would make every reference to it wrong the
	// moment somebody tidied the list, which is the whole thing this is for.
	//
	// Zero means a task written before numbering existed. Those are left
	// alone rather than backfilled: giving them numbers now would be inventing
	// an order nobody chose.
	//
	// Named TaskNo rather than Num because Num is already the accessor that
	// reads a numeric *field* off a node; the two are unrelated.
	TaskNo int
	// Columns are a table's column headings, and Rows are its rows. They are
	// the table's alone; no other type has them.
	//
	// The columns are declared once for the whole table rather than repeated
	// on every row, which is what stops two rows disagreeing about what they
	// hold — and what gives the read view a header row to draw. A cell is
	// positional against Columns; see Row.
	Columns []string
	Rows    []*Row
	// Items are the sub-lines of a list or todo. They are part of the line
	// rather than lines of their own: they carry the same fields as their
	// parent and inherit its type. A sub-line may hold sub-lines of its own,
	// and there it stops — MaxItemDepth levels, no more. Keeping them here is
	// what makes "only headers nest" true by construction instead of a rule
	// the editor has to enforce on every keystroke.
	Items []*Node
}

// Row is one row of a table: an id, and one cell per column.
//
// Cells are positional rather than keyed by column name. That is a deliberate
// departure from "named fields, not positional lists" — the rule that shapes
// every other node — and it holds here for a reason that does not apply
// elsewhere: the columns are declared on the *same node* as the cells, so a
// cell cannot drift from a schema kept somewhere else, and a column change
// rewrites the whole table in one step. Keying by name would also make two
// columns with the same heading impossible, and tables have those.
type Row struct {
	ID    string   `json:"id"`
	Cells []string `json:"cells"`
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
	Parent string `json:"parent,omitempty"`
	// Tags are the page's hashtags — what it is about, and how it is found.
	// Every page carries at least one; see EnsureTags. They are stored as
	// slugs, so the "#" in front of one is presentation and never data.
	Tags []string `json:"tags,omitempty"`
	// TaskSeq is the highest task number ever given out on this page — not the
	// highest still on it. The difference is the whole point: delete the last
	// task and the next one must not take its number back, or a reference
	// somebody wrote down starts pointing at different work.
	//
	// It has to be stored for that. Working it out from the tasks present only
	// remembers as far back as the highest surviving number, so deleting the
	// top task and saving twice quietly frees its number for reuse.
	//
	// Like Parent, it is the store's and never the editor's: the editor sends
	// title, tags and children, and Store.Save carries this across from what is
	// on disk. Nothing in the browser can lose it.
	TaskSeq  int     `json:"taskSeq,omitempty"`
	Children []*Node `json:"children,omitempty"`
}

// IsTaskPage reports whether this page is the working file of a task rather
// than a page standing on its own.
func (d *Doc) IsTaskPage() bool { return d.Parent != "" }

// ParseTags reads hashtags as a person writes them — "#hage, ved  arbeid" —
// into normalised, deduplicated slugs. Commas, spaces and the "#" itself are
// all separators, so no particular way of typing the list is wrong.
func ParseTags(s string) []string {
	var out []string
	for _, part := range strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == '#' || r == ';' || unicode.IsSpace(r)
	}) {
		tag := Slug(part)
		if tag == "" {
			continue
		}
		if !slices.Contains(out, tag) {
			out = append(out, tag)
		}
	}
	return out
}

// EnsureTags normalises a page's tags and guarantees there is at least one,
// falling back to the given slug. Every page has a tag because the front page
// lists pages by tag: a page with none would be findable by nothing but its
// name. The editor keeps the last one from being removed, so this only fires
// for a file written by hand or made before tags existed.
func (d *Doc) EnsureTags(fallback string) {
	d.Tags = ParseTags(strings.Join(d.Tags, " "))
	if len(d.Tags) == 0 {
		if tag := Slug(fallback); tag != "" {
			d.Tags = []string{tag}
		}
	}
}

// TasksHeading is the heading pinned to the top of every page, and the only
// place new todos may be created.
const TasksHeading = "Oppgåver"

// IsTasksHeading reports whether a node is the pinned tasks heading. It is
// matched by its slugged label rather than by an id, because a hand-written
// file has never been near the editor and still has to be recognised.
func IsTasksHeading(n *Node) bool {
	return n != nil && n.Type == "header" && Slug(n.Label()) == Slug(TasksHeading)
}

// TaskType is the line the tasks heading holds: a task on an ordinary page,
// where it opens a working file of its own, and a plain todo on a working
// file, which cannot spawn working files.
func TaskType(isTaskPage bool) string {
	if isTaskPage {
		return "todo"
	}
	return "task"
}

// ArchiveHeading collects finished tasks inside the tasks heading.
const ArchiveHeading = "Arkiv"

// Template is the starting content for a new page: the tasks block, and one
// empty line under it to start writing on.
//
// That line is a text line rather than a heading, and it is the one piece of
// body text a page has before anything is typed. A page opens with the caret
// in it, so it has to be the thing you most often want to write next — and
// having to clear a heading before typing a sentence is backwards. A heading
// is one `#` away.
// The author is whoever is making the page, and they are put down for its
// first task; see assignTo. An empty one is a page nobody signed for — a
// repair, or a file written by hand.
func Template(reg *Registry, isTaskPage bool, author string) []*Node {
	return append(TasksBlock(reg, isTaskPage, author), BodyLine(reg))
}

// BodyLine is the empty text line a page is written on: the one the template
// ends with, and the one ensureBody puts back if a page is left without any.
func BodyLine(reg *Registry) *Node {
	return &Node{
		ID:     NewID(),
		Type:   "text",
		Fields: reg.Defaults("text"),
	}
}

// TasksBlock is the pinned heading with one empty task. A task page is the
// working file for one task, so it gets plain todos; an ordinary page gets
// tasks, each of which opens a working file of its own.
//
// It is the template minus the body line, because Normalise uses it to repair
// a document that arrives without a tasks heading — and adding a stray blank
// line to somebody's existing page while fixing its heading would be a poor
// trade.
func TasksBlock(reg *Registry, isTaskPage bool, author string) []*Node {
	kind := TaskType(isTaskPage)
	first := reg.Defaults(kind)
	assignTo(reg, kind, first, author)
	return []*Node{{
		ID:     NewID(),
		Type:   "header",
		Fields: map[string]any{"text": TasksHeading},
		Children: []*Node{{
			ID:     NewID(),
			Type:   kind,
			Fields: first,
		}},
	}}
}

// assignTo puts a person down for a line: every field of `user` kind the type
// has is set to their login.
//
// The kind is what is asked about, never the field happening to be called
// `owner`. That is the same rule the query language, the people index and the
// editor's picker each hold ([ADR-0020]), and it is what makes a second
// person-field on a type work everywhere at once instead of in three places
// out of four.
//
// This exists because the *first* task on a page is made here, on the server,
// while every task after it is made in the editor — where `defaults` in
// editor.js has always filled a `user` field with whoever is signed in. So the
// first one was the odd one out: you made a page, wrote the first thing you
// meant to do on it, and it was the one task on the wiki nobody was down for.
//
// An empty login sets nothing, which leaves the field as `reg.Defaults` left
// it: blank, and blank is what an unassigned task looks like. That is the case
// for a repair — `pinTasks` fixing a document that arrives without its heading
// must not put whoever happened to open the page down for a task they have
// never seen.
func assignTo(reg *Registry, typeName string, fields map[string]any, login string) {
	if login == "" {
		return
	}
	t := reg.Get(typeName)
	if t == nil {
		return
	}
	for _, fd := range t.Fields {
		if fd.Kind == "user" {
			fields[fd.Name] = login
		}
	}
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
			if n.Type == "table" {
				for _, r := range n.Rows {
					for _, cell := range r.Cells {
						if strings.TrimSpace(cell) != "" {
							empty = false
							return
						}
					}
				}
				for _, c := range n.Columns {
					if strings.TrimSpace(c) != "" {
						empty = false
						return
					}
				}
			}
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
	case "file":
		// What it is called, else what it is called on disk — a file with no
		// description is still addressable by its name.
		if s := n.Str("name"); s != "" {
			return s
		}
		return n.Str("file")
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
// nest, so a hand-edited or seeded document is always well-formed. It also
// pins the tasks heading to the top of the page, making one if the file has
// none — the heading is furniture rather than content, so a document that
// arrives without it is repaired like any other malformed input.
func (d *Doc) Normalise(reg *Registry) {
	d.Children = normalise(d.Children, reg)
	d.Children = pinTasks(d.Children, reg, d.IsTaskPage())
	d.Children = ensureBody(d.Children, reg)
}

// ensureBody makes sure there is a line below the tasks section to write on.
//
// The tasks heading is pinned to the top and the page proper begins after it,
// so a document holding nothing *but* that section has nowhere to put a caret:
// it opens, draws its tasks, and cannot be typed into at all. The editor is
// supposed to stop you deleting the last such line, and did not — but a file is
// hand-editable and reachable by restore, so the repair belongs here as well as
// the guard there.
//
// This is not the warning in CLAUDE.md about repairing a *missing heading* with
// Template rather than TasksBlock. That one says: do not slip a body line into
// somebody's page as a side effect of fixing something else. Here the missing
// body is the fault being fixed, and an empty text line is the whole of it.
func ensureBody(nodes []*Node, reg *Registry) []*Node {
	for _, n := range nodes {
		if !IsTasksHeading(n) {
			return nodes // something other than the tasks section: there is a body
		}
	}
	return append(nodes, BodyLine(reg))
}

// pinTasks moves the tasks heading to the front, or adds one. Only a top-level
// heading counts: a section called "Oppgåver" nested inside another heading is
// somebody's own, not the pinned one.
func pinTasks(nodes []*Node, reg *Registry, isTaskPage bool) []*Node {
	for i, n := range nodes {
		if !IsTasksHeading(n) {
			continue
		}
		if i == 0 {
			return nodes
		}
		out := make([]*Node, 0, len(nodes))
		out = append(out, n)
		out = append(out, nodes[:i]...)
		return append(out, nodes[i+1:]...)
	}
	// A document with nothing in it at all is a new page in every way that
	// matters, so it gets the whole template, body line included.
	//
	// No author either way. This is a repair of a file that already exists,
	// and reading it is not the same as writing it: the person it happened in
	// front of never said they would do anything.
	if len(nodes) == 0 {
		return Template(reg, isTaskPage, "")
	}
	return append(TasksBlock(reg, isTaskPage, ""), nodes...)
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
		// `image` became `file`: an external URL in a page was never what
		// anyone wanted, and an uploaded file that travels with the page is.
		// The old `src` is left in the node rather than dropped, and the read
		// view still draws it, so a page written before this keeps its
		// picture.
		if n.Type == "image" {
			n.Type = "file"
			if alt := n.Str("alt"); alt != "" && n.Str("name") == "" {
				n.Fields["name"] = alt
				delete(n.Fields, "alt")
			}
		}
		if reg.Get(n.Type) == nil {
			n.Type = "text"
		}

		if n.Type == "table" {
			normaliseTable(n)
		}

		switch {
		case reg.HoldsChildren(n.Type):
			// A header takes anything. Stray items are lines that belong
			// under it.
			n.Children = normalise(append(n.Items, n.Children...), reg)
			n.Items = nil
			out = append(out, n)

		case reg.HoldsItems(n.Type):
			// A list or todo absorbs whatever was nested under it as items.
			// Older files stored these as children, so this doubles as their
			// migration.
			n.Items = asItems(n, append(n.Items, n.Children...), reg, 1)
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

// normaliseTable makes a table rectangular: at least one column and one row,
// every row as wide as the columns, and every row carrying an id.
//
// A short row is padded rather than rejected, and a long one is trimmed only
// of cells no column can hold — which is the same bargain as everywhere else
// here, repair rather than refuse, so a table typed by hand into the file
// still opens.
func normaliseTable(n *Node) {
	if len(n.Columns) == 0 {
		n.Columns = []string{"", ""}
	}
	if len(n.Rows) == 0 {
		n.Rows = []*Row{{ID: NewID()}}
	}
	for _, r := range n.Rows {
		if r.ID == "" {
			r.ID = NewID()
		}
		for len(r.Cells) < len(n.Columns) {
			r.Cells = append(r.Cells, "")
		}
		if len(r.Cells) > len(n.Columns) {
			r.Cells = r.Cells[:len(n.Columns)]
		}
	}
	n.Items, n.Children = nil, nil
}

// MaxItemDepth is how far sub-lines go inside a line: a sub-line, and a
// sub-line of that. Two levels is where a list stops being a list and starts
// being an outline, which is what headings are for — and the editor can only
// build what this allows, so the two agree by construction rather than by
// checking each other.
const MaxItemDepth = 2

// asItems turns nested nodes into sub-lines of parent, level counting from 1
// for the sub-lines of the line itself. Items inherit the parent's type, and
// anything nested deeper than MaxItemDepth is lifted into the deepest level
// there is rather than dropped.
func asItems(parent *Node, nested []*Node, reg *Registry, level int) []*Node {
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
		if level < MaxItemDepth {
			n.Items = asItems(parent, deeper, reg, level+1)
			continue
		}
		out = append(out, asItems(parent, deeper, reg, level)...)
	}
	return out
}
