package render

import (
	"errors"
	"fmt"
	"html"
	"html/template"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"marksheets/internal/doc"
	"marksheets/internal/files"
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
//
// The whole tasks section is left out — the heading and the tasks under it
// alike. `Les` is for reading the page, and a to-do list is working state
// rather than something anyone reads: it is where the page is worked on, not
// what the page says. Leaving the heading but keeping the tasks was tried
// first and is the worse half-measure, since the list is the bulky part.
//
// A query can still reach them. `@side/oppgåver[#øyvind]` renders wherever it
// is written, because that is somebody asking for the tasks rather than the
// page showing them unasked.
func (r *Renderer) Page(slug string, d *doc.Doc) template.HTML {
	c := &ctx{visiting: map[string]bool{slug: true}}
	rest := d.Children
	if len(rest) > 0 && doc.IsTasksHeading(rest[0]) {
		rest = rest[1:]
	}
	var b strings.Builder
	r.nodes(&b, rest, 1, c)
	return template.HTML(b.String())
}

// listTag is the element a run of a given line type is wrapped in. A numbered
// list is an <ol> so the browser numbers it — the numbers are presentation and
// are never stored, which is what keeps inserting a line in the middle from
// rewriting every line after it.
func listTag(typeName string) string {
	if typeName == "ordered" {
		return "ol"
	}
	return "ul"
}

// grouped reports whether a type is one of the line kinds that run together
// into a single list element.
func grouped(typeName string) bool {
	switch typeName {
	case "list", "ordered", "todo", "task":
		return true
	}
	return false
}

// nodes renders a sibling run, grouping consecutive list and todo lines into
// a single list element so they read as one list.
func (r *Renderer) nodes(b *strings.Builder, nodes []*doc.Node, depth int, c *ctx) {
	for i := 0; i < len(nodes); {
		t := nodes[i].Type
		if !grouped(t) {
			r.node(b, nodes[i], depth, c)
			i++
			continue
		}
		j := i
		for j < len(nodes) && nodes[j].Type == t {
			j++
		}
		tag := listTag(t)
		fmt.Fprintf(b, `<%s class="ms-%s-list">`, tag, t)
		for _, n := range nodes[i:j] {
			r.node(b, n, depth, c)
		}
		fmt.Fprintf(b, `</%s>`, tag)
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

	case "list", "ordered":
		fmt.Fprintf(b, `<li class="ms-item ms-%s-item">%s`, n.Type, r.inlineOf(n, "text", c))
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

	case "table":
		r.table(b, n, c)

	case "file":
		r.file(b, n)

	default:
		fmt.Fprintf(b, `<div class="ms-text">%s</div>`, r.inline(n.Label(), nil, c))
	}
}

// table renders a table as a table. The header row is drawn only when some
// column is actually named — an unnamed table of two columns is a layout, and
// a row of empty headings above it would be furniture.
//
// Cells carry inline markdown but not `@`-queries. A query resolves against a
// recorded link id, and links are recorded per *field* (see Store.recordLinks),
// which a cell is not — so a query written in a cell would resolve by path
// today, break silently on a rename, and never appear in a backlink. Half a
// feature is worse than none; see SPEC, "Not built yet".
func (r *Renderer) table(b *strings.Builder, n *doc.Node, c *ctx) {
	b.WriteString(`<figure class="ms-item ms-table"><table>`)

	named := false
	for _, col := range n.Columns {
		if strings.TrimSpace(col) != "" {
			named = true
			break
		}
	}
	if named {
		b.WriteString(`<thead><tr>`)
		for _, col := range n.Columns {
			fmt.Fprintf(b, `<th>%s</th>`, inlineMarkdown(col))
		}
		b.WriteString(`</tr></thead>`)
	}

	b.WriteString(`<tbody>`)
	for _, row := range n.Rows {
		b.WriteString(`<tr>`)
		for i := range n.Columns {
			cell := ""
			if i < len(row.Cells) {
				cell = row.Cells[i]
			}
			fmt.Fprintf(b, `<td>%s</td>`, inlineMarkdown(cell))
		}
		b.WriteString(`</tr>`)
	}
	b.WriteString(`</tbody></table>`)

	if name := strings.TrimSpace(n.Str("name")); name != "" {
		fmt.Fprintf(b, `<figcaption>%s</figcaption>`, html.EscapeString(name))
	}
	b.WriteString(`</figure>`)
}

// file renders an attachment: a picture if it is one, and otherwise a box that
// says what the file is, how big it is, and — for a PDF — shows the first page.
//
// The preview is an <iframe> pointing at the file itself. Every browser has a
// PDF viewer built in, so this costs no dependency and no conversion step; the
// viewer runs the document in its own sandbox rather than as script on this
// origin, which is the same reasoning that put PDF on the inline allowlist in
// the first place and keeps SVG off it.
//
// A node with no stored file but an `src` is a page written before uploads
// existed, when the type held a URL. It still draws, because content is never
// dropped to make room for a better idea.
func (r *Renderer) file(b *strings.Builder, n *doc.Node) {
	name := strings.TrimSpace(n.Str("name"))
	stored := n.Str("file")

	if stored == "" {
		if legacy := safeURL(n.Str("src")); legacy != "" {
			// The old type held nothing but pictures.
			r.figure(b, legacy, name, name)
			return
		}
		b.WriteString(`<div class="ms-item ms-missing">Inga fil lasta opp</div>`)
		return
	}

	href := "/" + files.Dir + "/" + url.PathEscape(stored)
	label := name
	if label == "" {
		label = stored
	}
	if files.IsImage(stored) {
		r.figure(b, href, label, name)
		return
	}

	size, there := r.src.FileSize(stored)
	fmt.Fprintf(b, `<figure class="ms-item ms-fileblock"><div class="ms-fileblock-head">`)
	fmt.Fprintf(b, `<span class="ms-fileblock-kind">%s</span>`, html.EscapeString(kindOf(stored)))
	fmt.Fprintf(b, `<a class="ms-fileblock-name" href="%s">%s</a>`, href, html.EscapeString(label))
	if there {
		fmt.Fprintf(b, `<span class="ms-fileblock-size">%s</span>`, files.HumanSize(size))
	} else {
		// A page pointing at a file that is gone says so, rather than offering
		// a link that does nothing.
		b.WriteString(`<span class="ms-fileblock-size is-missing">fila manglar</span>`)
	}
	b.WriteString(`</div>`)

	if there && strings.EqualFold(kindOf(stored), "PDF") {
		// The fragment asks the viewer for a plain fitted page: this is a
		// glance at the document, and the controls for reading it properly
		// are one click away on the name above.
		fmt.Fprintf(b, `<iframe class="ms-fileblock-view" src="%s#toolbar=0&navpanes=0&view=FitH" title="%s" loading="lazy"></iframe>`,
			href, html.EscapeString(label))
	}
	b.WriteString(`</figure>`)
}

// figure draws a picture with its caption.
func (r *Renderer) figure(b *strings.Builder, href, alt, caption string) {
	fmt.Fprintf(b, `<figure class="ms-item ms-figure"><img src="%s" alt="%s">`,
		href, html.EscapeString(alt))
	if caption != "" {
		fmt.Fprintf(b, `<figcaption>%s</figcaption>`, html.EscapeString(caption))
	}
	b.WriteString(`</figure>`)
}

// kindOf is the extension, shown as the badge on a file box.
func kindOf(name string) string {
	ext := strings.TrimPrefix(filepath.Ext(name), ".")
	if ext == "" {
		return "FIL"
	}
	return strings.ToUpper(ext)
}

// items renders a line's sub-lines. They share their parent's type, so they
// are rendered as that type inside a nested list.
func (r *Renderer) items(b *strings.Builder, n *doc.Node, depth int, c *ctx) {
	if len(n.Items) == 0 {
		return
	}
	tag := listTag(n.Type)
	fmt.Fprintf(b, `<%s class="ms-items ms-%s-list">`, tag, n.Type)
	for _, it := range n.Items {
		sub := *it
		sub.Type = n.Type // items carry no type of their own
		sub.Items = nil
		r.node(b, &sub, depth+1, c)
	}
	fmt.Fprintf(b, `</%s>`, tag)
}

// dataValue formats a data node as "value unit", leaving out either half when
// it is not there.
//
// An empty value is nothing, not zero. It used to print "0", because an empty
// field was stored as one — so a line meant to read "epost oyvind@me.com" read
// "epost 0 oyvind@me.com" instead. A stored zero is still a real value and
// still prints; it is the empty field that now prints nothing.
func dataValue(n *doc.Node) string {
	var v string
	if f, ok := n.Num("value"); ok {
		v = strconv.FormatFloat(f, 'f', -1, 64)
	} else {
		v = strings.TrimSpace(n.Str("value"))
	}
	u := strings.TrimSpace(n.Str("unit"))
	if v == "" {
		return u
	}
	if u == "" {
		return v
	}
	return v + " " + u
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
			// A query naming a page that does not exist is usually a page
			// waiting to be written, not a mistake — so it is offered as one
			// rather than reported as an error.
			if len(q.segs) > 0 {
				if _, ok := r.src.DocBySlug(q.segs[0]); !ok {
					return newPageChip(q.raw, q.segs[0])
				}
			}
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
	// The label says where this came from, so it may as well take you there.
	// The page, not the section: only the read view emits heading ids, and it
	// has no URL of its own for a fragment to land in yet.
	fmt.Fprintf(&b, `<div class="ms-tx ms-tx-block"><a class="ms-tx-source" href="/p/%s">%s</a>`,
		html.EscapeString(res.page), html.EscapeString(q.raw))
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

// newPageChip is a query pointing at a page nobody has written yet. It is a
// button rather than a link: following it would be going somewhere, and there
// is nowhere to go until you say the page should exist.
func newPageChip(raw, slug string) string {
	return fmt.Sprintf(
		`<button type="button" class="ms-tx ms-tx-error ms-tx-new" data-newpage="%s" title="Sida finst ikkje — klikk for å lage henne">%s</button>`,
		html.EscapeString(slug), html.EscapeString(raw))
}

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
	// NUL is the placeholder marker below, and nothing legitimate contains it.
	out := html.EscapeString(strings.ReplaceAll(s, "\x00", ""))

	// Each construct is parked behind a placeholder the moment it is rendered,
	// so a later rule cannot reach inside what an earlier one produced. Without
	// this the hashtag rule rewrote the `#` *inside* an href — which ends the
	// attribute early and destroys the link — and reached into `code` spans to
	// change what the code said.
	var held []string
	hold := func(fragment string) string {
		held = append(held, fragment)
		return fmt.Sprintf("\x00%d\x00", len(held)-1)
	}
	emphasise := func(t string) string {
		t = mdBold.ReplaceAllString(t, `<strong>$1</strong>`)
		return mdItalic.ReplaceAllString(t, `<em>$1</em>`)
	}

	out = mdCode.ReplaceAllStringFunc(out, func(m string) string {
		return hold("<code>" + mdCode.FindStringSubmatch(m)[1] + "</code>")
	})
	out = mdLink.ReplaceAllStringFunc(out, func(m string) string {
		p := mdLink.FindStringSubmatch(m)
		href := safeURL(html.UnescapeString(p[2]))
		if href == "" {
			return p[1]
		}
		// The text of a link may still be emphasised; only the href is sealed.
		return hold(fmt.Sprintf(`<a href="%s">%s</a>`, href, emphasise(p[1])))
	})
	out = emphasise(out)
	out = hashtagRe.ReplaceAllString(out, `<span class="ms-tag">#$1</span>`)

	for i := len(held) - 1; i >= 0; i-- {
		out = strings.Replace(out, fmt.Sprintf("\x00%d\x00", i), held[i], 1)
	}
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
