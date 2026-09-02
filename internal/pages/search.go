package pages

import (
	"sort"
	"strconv"
	"strings"

	"marksheets/internal/doc"
)

// Search reads the files and looks. There is no index.
//
// That is the same bargain as backlinks, the unpublished set and the people
// index: an answer worked out on demand cannot disagree with the pages it was
// worked out from. An index would be a second copy of the notes that has to be
// kept honest against hand edits, files arriving from git, and the app being
// closed while somebody types in a text editor — and at this size the scan
// costs less than the bookkeeping would.

// maxLines is how many matching lines one page shows before the rest are
// counted instead. A page that mentions a word forty times has answered the
// question by the fifth.
const maxLines = 5

// Hit is one page that matched, and where.
type Hit struct {
	Slug  string
	Title string
	// Parent is the page this one is the working file of, empty for an
	// ordinary page.
	Parent      string
	ParentTitle string
	// Name is set when the page's own title or one of its tags matched, which
	// is a different kind of hit from a line inside it.
	Name  bool
	Lines []HitLine
	// More is how many further lines matched but are not listed.
	More int
}

// HitLine is one matching line, split around the match so the template can
// mark it without building HTML down here.
type HitLine struct {
	// Where is the trail of headings the line sits under, outermost first.
	Where  []string
	Before string
	Match  string
	After  string
}

// Search returns every page whose title, tags or content hold q, most recently
// edited first, with pages matched by name before pages matched by content.
func (s *Store) Search(q string) ([]Hit, error) {
	needle := strings.ToLower(strings.TrimSpace(q))
	if needle == "" {
		return nil, nil
	}
	list, err := s.List()
	if err != nil {
		return nil, err
	}

	var out []Hit
	for _, p := range list {
		if !p.OK() {
			continue
		}
		hit := Hit{Slug: p.Slug, Title: p.Title}
		if strings.Contains(strings.ToLower(p.Title), needle) {
			hit.Name = true
		}
		for _, t := range p.Tags {
			if strings.Contains(strings.ToLower(t), needle) {
				hit.Name = true
			}
		}
		s.searchDoc(&hit, p.Doc, needle)
		if !hit.Name && len(hit.Lines) == 0 {
			continue
		}
		if p.Parent != "" {
			hit.Parent, _, _ = strings.Cut(p.Parent, "#")
			hit.ParentTitle = hit.Parent
			if parent, err := s.load(hit.Parent); err == nil && parent.OK() {
				hit.ParentTitle = parent.Title
			}
		}
		out = append(out, hit)
	}

	// A page whose *name* matches is what somebody meant far more often than a
	// page that happens to say the word once, so those come first. List order —
	// most recently edited — decides the rest, and sort.SliceStable keeps it.
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name && !out[j].Name })
	return out, nil
}

// searchDoc walks a page, keeping the trail of headings above each line so a
// hit can say where in the page it is.
func (s *Store) searchDoc(hit *Hit, d *doc.Doc, needle string) {
	var where []string
	var walk func(nodes []*doc.Node)
	walk = func(nodes []*doc.Node) {
		for _, n := range nodes {
			for _, text := range s.lineTexts(n) {
				if line, ok := split(text, needle); ok {
					line.Where = append([]string(nil), where...)
					if hit.More > 0 || len(hit.Lines) >= maxLines {
						hit.More++
					} else {
						hit.Lines = append(hit.Lines, line)
					}
				}
			}
			if n.Type == "header" {
				where = append(where, n.Label())
				walk(n.Items)
				walk(n.Children)
				where = where[:len(where)-1]
				continue
			}
			walk(n.Items)
			walk(n.Children)
		}
	}
	walk(d.Children)
}

// lineTexts is what one line offers to a search: the line as it reads, and —
// for a table — one string per row, since a table's content is in its cells
// rather than in its fields.
//
// A heading is searched too: it is a line somebody wrote, and finding the
// section by its name is finding the section.
func (s *Store) lineTexts(n *doc.Node) []string {
	if n.Type == "table" {
		out := make([]string, 0, len(n.Rows)+1)
		if name := strings.TrimSpace(n.Str("name")); name != "" {
			out = append(out, name)
		}
		if cols := strings.TrimSpace(strings.Join(n.Columns, " · ")); cols != "" {
			out = append(out, cols)
		}
		for _, row := range n.Rows {
			if line := strings.TrimSpace(strings.Join(row.Cells, " · ")); line != "" {
				out = append(out, line)
			}
		}
		return out
	}

	// Every string field, in the order the type declares them, so a data line
	// reads back as "budsjett 25000 kr" and matches on any part of it.
	var parts []string
	if td := s.reg.Get(n.Type); td != nil {
		for _, fd := range td.Fields {
			if fd.Kind == "bool" {
				continue
			}
			v := fieldText(n, fd.Name)
			if v == "" {
				continue
			}
			parts = append(parts, v)
		}
	}
	if len(parts) == 0 {
		if label := strings.TrimSpace(n.Label()); label != "" {
			parts = append(parts, label)
		}
	}
	if len(parts) == 0 {
		return nil
	}
	return []string{strings.Join(parts, " ")}
}

// fieldText is a field as it reads on the page. A number is stored as a number
// — that is what makes 25000 sortable and one day summable — and `Str` returns
// nothing for one, so a search for the budget would have missed every budget
// there is.
func fieldText(n *doc.Node, field string) string {
	if v := strings.TrimSpace(n.Str(field)); v != "" {
		return v
	}
	if f, ok := n.Num(field); ok {
		return strconv.FormatFloat(f, 'f', -1, 64)
	}
	return ""
}

// split cuts a line around the first match, keeping what was actually typed
// rather than the lowered copy the comparison was made on.
func split(text, needle string) (HitLine, bool) {
	i := strings.Index(strings.ToLower(text), needle)
	if i < 0 {
		return HitLine{}, false
	}
	return HitLine{
		Before: text[:i],
		Match:  text[i : i+len(needle)],
		After:  text[i+len(needle):],
	}, true
}

// Suggest is the short list offered under the search box as you type: pages
// whose name or tag matches, and nothing else.
//
// Names only, on purpose. This half of search answers "take me to the page I
// am thinking of", which is a question about names; the full scan behind Enter
// answers the other one.
func (s *Store) Suggest(q string, limit int) ([]*Page, error) {
	needle := strings.ToLower(strings.TrimSpace(q))
	if needle == "" {
		return nil, nil
	}
	list, err := s.List()
	if err != nil {
		return nil, err
	}
	var starts, holds []*Page
	for _, p := range list {
		if p.Hidden() || !p.OK() {
			continue
		}
		name := strings.ToLower(p.Title)
		switch {
		case strings.HasPrefix(name, needle):
			starts = append(starts, p)
		case strings.Contains(name, needle) || tagMatches(p.Tags, needle):
			holds = append(holds, p)
		}
	}
	// What you typed the start of comes before what merely contains it.
	out := append(starts, holds...)
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func tagMatches(tags []string, needle string) bool {
	for _, t := range tags {
		if strings.Contains(strings.ToLower(t), needle) {
			return true
		}
	}
	return false
}
