package pages

import (
	"sort"
	"strings"
	"time"

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
	// Updated is when this page's file last changed, shown beside the title.
	// It is the page's own time and nobody in particular's: the store knows
	// when a file moved, not who moved it.
	Updated time.Time
	// Under holds the working files hanging off this page, each with the todos
	// written on it. A working file used to stand at the top level beside the
	// page it belongs to, which put the job and the notes about the job in two
	// unrelated places in the list.
	Under []OwnerGroup
	// Parent is set only when a working file could **not** be filed under its
	// page — the page is gone, deleted by hand or lost in a restore. Nesting
	// says where a file hangs; this is what is left to say it with when the
	// nesting could not be made.
	Parent      string
	ParentTitle string
	Open        []Assignment
	Done        []Assignment
}

// Count is the open and the done tasks in this group, the working files under
// it included.
//
// It is a method rather than a loop at the call site because the call site got
// it wrong once: the count in the profile's heading added up `len(g.Open)` over
// the top level, which was exact while the list was flat and silently short by
// every todo on a working file the moment those moved underneath their page.
// A group knows what is in it; nothing else should have to.
func (g OwnerGroup) Count() (open, done int) {
	open, done = len(g.Open), len(g.Done)
	for _, u := range g.Under {
		o, d := u.Count()
		open += o
		done += d
	}
	return open, done
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
// written on: an ordinary page at the top level, and the working files hanging
// off it indented underneath.
//
// Working files are included rather than hidden. A task page is left out of the
// front page because it is reached through its task, but the todos written on
// it are still somebody's job, and leaving them out would make this list
// quietly wrong. What changed is where they are drawn — beside the page they
// belong to rather than somewhere else in the same flat list, which put the job
// and the notes about the job in two unrelated places.
//
// **The order is the group's, not the page's.** A parent sits where its most
// recently changed page sits, counting the working files under it. Sorting the
// parents on their own file time alone would sink a group to the bottom of the
// list while somebody was working in one of its working files — which is the
// case this list is most often open for. The date drawn beside each title is
// still that page's own; the nesting is what makes an older parent above a
// newer one read as the truth rather than as a bug.
func (s *Store) AssignedTo(name string) ([]OwnerGroup, error) {
	want := doc.Slug(strings.TrimSpace(name))
	if want == "" {
		return nil, nil
	}

	group := map[string]*OwnerGroup{}
	hangsOff := map[string]string{} // working file -> the page it belongs to
	var seen []string               // slugs, in the order the store listed them

	err := s.eachAssigned(func(p *Page, owner string, n *doc.Node) {
		if owner != want {
			return
		}
		g, ok := group[p.Slug]
		if !ok {
			g = &OwnerGroup{Slug: p.Slug, Title: p.Title, Updated: p.UpdatedAt}
			if p.Parent != "" {
				hangsOff[p.Slug], _, _ = strings.Cut(p.Parent, "#")
			}
			group[p.Slug] = g
			seen = append(seen, p.Slug)
		}
		a := Assignment{Text: strings.TrimSpace(n.Label()), Done: n.Bool("done"), Page: n.Page}
		if a.Done {
			g.Done = append(g.Done, a)
			return
		}
		g.Open = append(g.Open, a)
	})
	if err != nil {
		return nil, err
	}

	// A working file needs something to be indented under, and the page it
	// belongs to may hold nothing of this person's own — somebody else's page,
	// with one task on it that opened a file you are working in. So the page is
	// added on demand, with no tasks of its own, which is the truth: the work is
	// on the file below it.
	//
	// Read in a pass of its own, because `seen` is appended to here and the loop
	// below walks the finished list. A working file's own todos never open
	// further working files — a task page gets plain todos — so this nests
	// exactly two deep and cannot recurse.
	for _, slug := range seen {
		owner := hangsOff[slug]
		if owner == "" || group[owner] != nil {
			continue
		}
		p, err := s.load(owner)
		if err != nil || !p.OK() {
			continue // gone; handled below, where it stays at the top level
		}
		group[owner] = &OwnerGroup{Slug: p.Slug, Title: p.Title, Updated: p.UpdatedAt}
		seen = append(seen, owner)
	}

	nested := map[string]bool{}
	for _, slug := range seen {
		owner := hangsOff[slug]
		if owner == "" {
			continue
		}
		host, ok := group[owner]
		if !ok {
			// The page it hung off is gone. The file stays at the top level and
			// says where it belonged, rather than dropping somebody's job on the
			// floor because its parent did.
			group[slug].Parent = owner
			group[slug].ParentTitle = owner
			continue
		}
		host.Under = append(host.Under, *group[slug])
		nested[slug] = true
	}

	// When the group was last touched, which is what the top level is sorted on.
	active := map[string]time.Time{}
	var out []OwnerGroup
	for _, slug := range seen {
		if nested[slug] {
			continue
		}
		g := group[slug]
		sort.Slice(g.Under, func(i, j int) bool {
			return newer(g.Under[i].Updated, g.Under[j].Updated, g.Under[i].Title, g.Under[j].Title)
		})
		at := g.Updated
		for _, u := range g.Under {
			if u.Updated.After(at) {
				at = u.Updated
			}
		}
		active[slug] = at
		out = append(out, *g)
	}
	sort.Slice(out, func(i, j int) bool {
		return newer(active[out[i].Slug], active[out[j].Slug], out[i].Title, out[j].Title)
	})
	return out, nil
}

// newer orders two pages by time, most recent first, and falls back to the
// title so that two pages written in the same second do not swap places between
// page loads.
func newer(a, b time.Time, at, bt string) bool {
	if !a.Equal(b) {
		return a.After(b)
	}
	return at < bt
}
