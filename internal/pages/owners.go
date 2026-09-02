package pages

import (
	"sort"
	"strings"

	"marksheets/internal/doc"
)

// Who a task is for is the one thing about it that is not on the page it lives
// on. Every other way in — the front page, a tag, a query — starts from a page
// and works down; this starts from a person and gathers what is theirs from
// wherever it was written.
//
// Nothing is stored for it. The answer is worked out from the files on every
// request, the same bargain as backlinks and the unpublished set: a computed
// answer cannot disagree with the pages it is computed from.

// Assignment is one task or todo somebody is named on.
type Assignment struct {
	Text string
	Done bool
	// Page is the working file the task owns, empty when it has none.
	Page string
}

// OwnerGroup is one page's share of somebody's tasks. Open and Done are kept
// apart because they are read for different reasons: what is left, and what
// happened.
type OwnerGroup struct {
	Slug  string
	Title string
	// Parent is the page this one is the working file of, empty for an
	// ordinary page. A working file is reached through the task that owns it,
	// so saying where it hangs is the difference between a link and a riddle.
	Parent      string
	ParentTitle string
	Open        []Assignment
	Done        []Assignment
}

// Owner is a name tasks are filed under, and how they stand.
type Owner struct {
	Name string
	Open int
	Done int
}

// eachAssigned calls fn for every node that names somebody, once per name.
//
// "Names somebody" is a field of kind `user` holding a value — the same rule
// `@side[@kari]` matches on, so the page and the query cannot drift apart.
// Today that is the owner of a task or a todo and nothing else; a type that
// grows such a field is picked up here without being told about.
func (s *Store) eachAssigned(fn func(p *Page, name string, n *doc.Node)) error {
	list, err := s.List()
	if err != nil {
		return err
	}
	for _, p := range list {
		if !p.OK() {
			continue
		}
		p.Doc.Walk(func(n *doc.Node, _ int) {
			td := s.reg.Get(n.Type)
			if td == nil {
				return
			}
			for _, fd := range td.Fields {
				if fd.Kind != "user" {
					continue
				}
				name := doc.Slug(strings.TrimSpace(n.Str(fd.Name)))
				if name == "" {
					continue
				}
				fn(p, name, n)
			}
		})
	}
	return nil
}

// Owners is everybody with something to their name, most to do first.
func (s *Store) Owners() ([]Owner, error) {
	open, done := map[string]int{}, map[string]int{}
	err := s.eachAssigned(func(_ *Page, name string, n *doc.Node) {
		if n.Bool("done") {
			done[name]++
			return
		}
		open[name]++
	})
	if err != nil {
		return nil, err
	}
	out := make([]Owner, 0, len(open))
	for name := range done {
		if _, ok := open[name]; !ok {
			open[name] = 0
		}
	}
	for name, n := range open {
		out = append(out, Owner{Name: name, Open: n, Done: done[name]})
	}
	// Most still to do first, then alphabetical, so the order does not shuffle
	// between page loads.
	sort.Slice(out, func(i, j int) bool {
		if out[i].Open != out[j].Open {
			return out[i].Open > out[j].Open
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// AssignedTo is everything one person is named on, grouped by the page it was
// written on and in the order the store lists those pages — most recently
// touched first.
//
// Working files are included rather than hidden. A task page is left out of
// the front page because it is reached through its task, but the todos written
// on it are still somebody's job, and leaving them out would make this list
// quietly wrong.
func (s *Store) AssignedTo(name string) ([]OwnerGroup, error) {
	want := doc.Slug(strings.TrimSpace(name))
	if want == "" {
		return nil, nil
	}
	var out []OwnerGroup
	at := map[string]int{} // page slug -> index in out
	err := s.eachAssigned(func(p *Page, owner string, n *doc.Node) {
		if owner != want {
			return
		}
		i, seen := at[p.Slug]
		if !seen {
			g := OwnerGroup{Slug: p.Slug, Title: p.Title}
			if p.Parent != "" {
				g.Parent, _, _ = strings.Cut(p.Parent, "#")
				g.ParentTitle = g.Parent
				if parent, err := s.load(g.Parent); err == nil && parent.OK() {
					g.ParentTitle = parent.Title
				}
			}
			i = len(out)
			at[p.Slug] = i
			out = append(out, g)
		}
		a := Assignment{Text: strings.TrimSpace(n.Label()), Done: n.Bool("done"), Page: n.Page}
		if a.Done {
			out[i].Done = append(out[i].Done, a)
			return
		}
		out[i].Open = append(out[i].Open, a)
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
