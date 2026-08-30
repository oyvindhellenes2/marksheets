package render

import (
	"errors"
	"fmt"
	"html"
	"html/template"
	"regexp"
	"strconv"
	"strings"

	"marksheets/internal/doc"
)

// maxDepth caps how far transclusions may nest before we stop expanding.
const maxDepth = 6

// Renderer turns documents into read-view HTML, expanding @-queries as it goes.
type Renderer struct {
	src Source
	reg *doc.Registry
}

func New(src Source, reg *doc.Registry) *Renderer {
	return &Renderer{src: src, reg: reg}
}

// ctx carries transclusion state so a page that pulls from itself, directly or
// through another page, stops instead of recursing forever.
type ctx struct {
	depth    int
	visiting map[string]bool
}

// Page renders a whole document. slug identifies it so self-reference is caught.
func (r *Renderer) Page(slug string, d *doc.Doc) template.HTML {
	c := &ctx{visiting: map[string]bool{slug: true}}
	var b strings.Builder
	r.nodes(&b, d.Children, 1, c)
	return template.HTML(b.String())
}

// nodes renders a sibling run, grouping consecutive list and todo lines into
// a single <ul> so they read as one list.
func (r *Renderer) nodes(b *strings.Builder, nodes []*doc.Node, depth int, c *ctx) {
	for i := 0; i < len(nodes); {
		t := nodes[i].Type
		if t != "list" && t != "todo" && t != "task" {
			r.node(b, nodes[i], depth, c)
			i++
			continue
		}
		j := i
		for j < len(nodes) && nodes[j].Type == t {
			j++
		}
		fmt.Fprintf(b, `<ul class="ms-%s-list">`, t)
		for _, n := range nodes[i:j] {
			r.node(b, n, depth, c)
		}
		b.WriteString(`</ul>`)
		i = j
	}
}

func (r *Renderer) node(b *strings.Builder, n *doc.Node, depth int, c *ctx) {
	switch n.Type {
	case "header":
		level := depth
		if level > 6 {
			level = 6
		}
		fmt.Fprintf(b, `<h%d id="%s" class="ms-h ms-h%d">%s</h%d>`,
			level, html.EscapeString(doc.Slug(n.Label())), level, r.inlineOf(n, "text", c), level)
		fmt.Fprintf(b, `<div class="ms-section">`)
		r.nodes(b, n.Children, depth+1, c)
		b.WriteString(`</div>`)

	case "text":
		fmt.Fprintf(b, `<div class="ms-text">%s</div>`, r.inlineOf(n, "text", c))

	case "list":
		fmt.Fprintf(b, `<li class="ms-item ms-list-item">%s`, r.inlineOf(n, "text", c))
		r.items(b, n, depth, c)
		b.WriteString(`</li>`)

	case "todo":
		checked := ""
		cls := ""
		if n.Bool("done") {
			checked, cls = " checked", " is-done"
		}
		fmt.Fprintf(b,
			`<li class="ms-item ms-todo%s"><input type="checkbox" disabled%s><span class="ms-todo-text">%s</span>`,
			cls, checked, r.inlineOf(n, "text", c))
		if owner := n.Str("owner"); owner != "" {
			fmt.Fprintf(b, `<span class="ms-tag">#%s</span>`, html.EscapeString(strings.TrimPrefix(owner, "#")))
		}
		r.items(b, n, depth, c)
		b.WriteString(`</li>`)

	case "task":
		checked, cls := "", ""
		if n.Bool("done") {
			checked, cls = " checked", " is-done"
		}
		fmt.Fprintf(b,
			`<li class="ms-item ms-task%s"><input type="checkbox" disabled%s><span class="ms-todo-text">%s</span>`,
			cls, checked, r.inlineOf(n, "text", c))
		if n.Page != "" {
			fmt.Fprintf(b, `<a class="ms-task-open" href="/p/%s" title="Arbeidsside">→</a>`,
				html.EscapeString(n.Page))
		}
		if owner := n.Str("owner"); owner != "" {
			fmt.Fprintf(b, `<span class="ms-tag">#%s</span>`, html.EscapeString(strings.TrimPrefix(owner, "#")))
		}
		b.WriteString(`</li>`)

	case "data":
		fmt.Fprintf(b,
			`<div class="ms-item ms-data"><span class="ms-data-name">%s</span><span class="ms-data-value">%s</span></div>`,
			html.EscapeString(n.Str("name")), html.EscapeString(dataValue(n)))

	case "image":
		src := safeURL(n.Str("src"))
		alt := html.EscapeString(n.Str("alt"))
		if src == "" {
			fmt.Fprintf(b, `<div class="ms-item ms-missing">Bilete manglar URL</div>`)
			return
		}
		fmt.Fprintf(b, `<figure class="ms-item ms-figure"><img src="%s" alt="%s">`, src, alt)
		if alt != "" {
			fmt.Fprintf(b, `<figcaption>%s</figcaption>`, alt)
		}
		b.WriteString(`</figure>`)

	default:
		fmt.Fprintf(b, `<div class="ms-text">%s</div>`, r.inline(n.Label(), nil, c))
	}
}

// items renders a line's sub-lines. They share their parent's type, so they
// are rendered as that type inside a nested list.
func (r *Renderer) items(b *strings.Builder, n *doc.Node, depth int, c *ctx) {
	if len(n.Items) == 0 {
		return
	}
	fmt.Fprintf(b, `<ul class="ms-items ms-%s-list">`, n.Type)
	for _, it := range n.Items {
		sub := *it
		sub.Type = n.Type // items carry no type of their own
		sub.Items = nil
		r.node(b, &sub, depth+1, c)
	}
	b.WriteString(`</ul>`)
}

// dataValue formats a data node as "value unit".
func dataValue(n *doc.Node) string {
	var v string
	if f, ok := n.Num("value"); ok {
		v = strconv.FormatFloat(f, 'f', -1, 64)
	} else {
		v = n.Str("value")
	}
	if u := n.Str("unit"); u != "" {
		return v + " " + u
	}
	return v
}

// inlineOf renders one field of a node, passing along that node's link hints
// so each @-query resolves to the id it was saved against.
func (r *Renderer) inlineOf(n *doc.Node, field string, c *ctx) string {
	return r.inline(n.Str(field), n.Links, c)
}

// inline renders text: HTML-escaped, with inline markdown applied and
// @-queries expanded in place.
func (r *Renderer) inline(s string, links map[string]string, c *ctx) string {
	if s == "" {
		return ""
	}
	var b strings.Builder
	prev := 0
	for _, loc := range queryRe.FindAllStringSubmatchIndex(s, -1) {
		start := loc[0]
		q, consumed := queryAt(s, loc)
		if len(q.segs) == 0 {
			continue
		}
		b.WriteString(inlineMarkdown(s[prev:start]))
		b.WriteString(r.expand(q, links[s[start:start+consumed]], c))
		prev = start + consumed
	}
	b.WriteString(inlineMarkdown(s[prev:]))
	return b.String()
}

// expand resolves a query and renders whatever it points at, read-only.
//
// The recorded id wins over the written path: that is what lets a link keep
// working after its target is renamed. The path is the fallback, so a query
// typed by hand — with no id recorded yet — still resolves.
func (r *Renderer) expand(q query, hint string, c *ctx) string {
	res, err := r.byHint(hint, q)
	if err != nil {
		if res, err = r.resolve(q); err != nil {
			return errChip(q.raw, err.Error())
		}
	}

	// A page on its own is a *link*, not a transclusion. Pulling an entire page
	// into the middle of a sentence was never something anyone wanted — it was
	// what happened by accident when a page was merely mentioned — so the bare
	// form points at the page instead of pulling it in. A path with more than
	// one segment still transcludes, which is the form that is actually useful.
	if res.node == nil && !res.filtered {
		return r.pageLink(q, res.page)
	}

	// Link text names somewhere you can go. A field is a value and a filter is
	// a set; neither is a place, so there would be nothing for the name to
	// point at. Headings will qualify once a fragment has something to land on.
	if q.hadLabel {
		return errChip(q.raw, "namn i parentes verkar berre på ei lenkje til ei heil side")
	}

	// Only what follows pulls content in, and only that can recurse.
	if c.depth >= maxDepth {
		return errChip(q.raw, "for mange nivå med henting")
	}
	if c.visiting[res.page] {
		return errChip(q.raw, "sirkulær henting")
	}

	c.visiting[res.page] = true
	c.depth++
	defer func() { c.depth--; delete(c.visiting, res.page) }()

	// A bare data node reads as a value inline, not as a block.
	if !res.filtered && res.node != nil && res.node.Type == "data" {
		return fmt.Sprintf(`<span class="ms-tx ms-tx-value" title="%s">%s</span>`,
			html.EscapeString(q.raw), html.EscapeString(dataValue(res.node)))
	}

	nodes := res.nodes
	if res.filtered {
		// A filter yields a flat set: any matching descendant is already in
		// the set in its own right, so rendering subtrees would repeat it.
		flat := make([]*doc.Node, len(nodes))
		for i, n := range nodes {
			shallow := *n
			shallow.Children = nil
			shallow.Items = nil
			flat[i] = &shallow
		}
		nodes = flat
	} else if res.node != nil && res.node.Type != "header" {
		nodes = []*doc.Node{res.node}
	}

	var b strings.Builder
	fmt.Fprintf(&b, `<div class="ms-tx ms-tx-block"><span class="ms-tx-source">%s</span>`, html.EscapeString(q.raw))
	r.nodes(&b, nodes, 3, c)
	b.WriteString(`</div>`)
	return b.String()
}

// pageLink renders a link to a whole page. The text is whatever stood in the
// parentheses, or else the page's own title — looked up at render time rather
// than stored, so it cannot drift from what the page is actually called, and
// so renaming a page needs no propagation to keep every link to it honest.
func (r *Renderer) pageLink(q query, slug string) string {
	label := strings.TrimSpace(q.label)
	if label == "" {
		if d, ok := r.src.DocBySlug(slug); ok {
			label = strings.TrimSpace(d.Title)
		}
	}
	if label == "" {
		label = slug
	}
	return fmt.Sprintf(`<a class="ms-link" href="/p/%s">%s</a>`,
		html.EscapeString(slug), html.EscapeString(label))
}

// byHint resolves a query through its recorded target id. For a filtered
// query the id records the scope the filter runs against; the matching set
// itself is always recomputed.
func (r *Renderer) byHint(hint string, q query) (result, error) {
	if hint == "" {
		return result{}, errNoHint
	}
	t := ParseTarget(hint)
	d, ok := r.src.DocBySlug(t.Page)
	if !ok {
		return result{}, errNoHint
	}
	res := result{page: t.Page, nodes: d.Children}
	if t.Node != "" {
		n := FindNode(d, t.Node)
		if n == nil {
			return result{}, errNoHint // target deleted — fall back to the path
		}
		res.node, res.nodes = n, childLines(n)
	}
	// The filter is always recomputed; only the scope it applies to is
	// remembered by id.
	return r.applyFilter(res, q)
}

var errNoHint = errors.New("ingen lenkje-id")

func errChip(raw, msg string) string {
	return fmt.Sprintf(`<span class="ms-tx ms-tx-error" title="%s">%s</span>`,
		html.EscapeString(msg), html.EscapeString(raw))
}

var (
	mdLink   = regexp.MustCompile(`\[([^\]]*)\]\(([^)\s]+)\)`)
	mdBold   = regexp.MustCompile(`\*\*([^*]+)\*\*`)
	mdItalic = regexp.MustCompile(`\*([^*]+)\*`)
	mdCode   = regexp.MustCompile("`([^`]+)`")
)

// inlineMarkdown escapes text and applies the inline markdown that a
// line-based editor still needs. Block syntax is deliberately absent: headers,
// lists and todos are node types here, not markdown.
func inlineMarkdown(s string) string {
	if s == "" {
		return ""
	}
	out := html.EscapeString(s)
	out = mdCode.ReplaceAllString(out, `<code>$1</code>`)
	out = mdLink.ReplaceAllStringFunc(out, func(m string) string {
		p := mdLink.FindStringSubmatch(m)
		href := safeURL(html.UnescapeString(p[2]))
		if href == "" {
			return p[1]
		}
		return fmt.Sprintf(`<a href="%s">%s</a>`, href, p[1])
	})
	out = mdBold.ReplaceAllString(out, `<strong>$1</strong>`)
	out = mdItalic.ReplaceAllString(out, `<em>$1</em>`)
	out = hashtagRe.ReplaceAllString(out, `<span class="ms-tag">#$1</span>`)
	return out
}

// safeURL allows only links that cannot execute script.
func safeURL(u string) string {
	u = strings.TrimSpace(u)
	if u == "" {
		return ""
	}
	lower := strings.ToLower(u)
	switch {
	case strings.HasPrefix(lower, "http://"), strings.HasPrefix(lower, "https://"),
		strings.HasPrefix(lower, "mailto:"), strings.HasPrefix(lower, "/"),
		strings.HasPrefix(lower, "#"), strings.HasPrefix(lower, "data:image/"):
		return html.EscapeString(u)
	case strings.Contains(lower, ":"):
		return "" // unknown scheme
	default:
		return html.EscapeString(u) // relative link
	}
}
