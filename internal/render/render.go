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
	"marksheets/internal/preview"
)

// maxDepth caps how far transclusions may nest before we stop expanding.
const maxDepth = 6

// Renderer turns documents into read-view HTML, expanding @-queries as it goes.
type Renderer struct {
	src Source
	reg *doc.Registry
	// cards is what has been read off the sites people link to. It may be nil —
	// a renderer built without one draws the plain card and nothing breaks,
	// which is what keeps the read view testable with no network in reach.
	cards *preview.Store
}

func New(src Source, reg *doc.Registry, cards *preview.Store) *Renderer {
	return &Renderer{src: src, reg: reg, cards: cards}
}

// ctx carries transclusion state so a page that pulls from itself, directly or
// through another page, stops instead of recursing forever.
type ctx struct {
	depth    int
	visiting map[string]bool
	// shared marks a rendering for a public share link, where the links that
	// lead further into the wiki are not drawn as links at all. On a page
	// anybody can open, "disabled in the browser" is not disabled: the reader
	// may have no script, and the href would still name a page.
	shared bool
}

// dead draws what a link would have been, as text. The words stay — they are
// part of a sentence somebody wrote, and dropping them to make a rule true
// would be editing the page. Only the way out goes.
func dead(class, label string) string {
	return fmt.Sprintf(`<span class="%s is-dead" title="Lenkja er av på eit delt dokument">%s</span>`,
		class, label)
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
	return r.page(slug, d, false)
}

// Shared renders a page for a public share link: the same page, with every
// link that leads further into the wiki drawn as plain text instead.
//
// What a query *pulled in* still shows. A transcluded section is part of what
// this page says — it was written into the sentence — so sharing the page
// shares it. That is worth knowing before pressing the button, and it is said
// in SPEC rather than quietly worked around here.
func (r *Renderer) Shared(slug string, d *doc.Doc) template.HTML {
	return r.page(slug, d, true)
}

func (r *Renderer) page(slug string, d *doc.Doc, shared bool) template.HTML {
	c := &ctx{visiting: map[string]bool{slug: true}, shared: shared}
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
		// A line that is nothing but an address becomes a card. Apple Notes does
		// this, and the reason it is worth copying is that a bare URL on its own
		// line carries no information at all — it is the one place where what
		// somebody wrote and what they meant are furthest apart.
		//
		// Only a line that is *entirely* one address. A link inside a sentence
		// stays a link inside a sentence: turning that into a card would break
		// the sentence in half.
		if u := loneURL(n.Str("text")); u != "" {
			b.WriteString(r.card(u))
			break
		}
		// A line that is nothing but a quotation is set as one — indented, with
		// a rule beside it. Inside a sentence a quotation stays inline; it is
		// only when the whole line is somebody else's words that the line itself
		// is the quotation and can be drawn like one.
		if loneQuote(n.Str("text")) {
			fmt.Fprintf(b, `<div class="ms-quoteline">%s</div>`, r.inlineOf(n, "text", c))
			break
		}
		fmt.Fprintf(b, `<div class="ms-text">%s</div>`, r.inlineOf(n, "text", c))

	// A code line is the one place text is printed exactly as it was typed:
	// no markdown, and — the reason the type exists — no @-query expansion.
	// Documenting the query language needs a way to write a query down without
	// it being answered, and a code *span* is no help there: inline() splits on
	// queries before inlineMarkdown ever sees the backticks, so the query is
	// long gone by the time the span could protect it.
	//
	// The field's kind is `code` rather than `text` for the same reason one
	// level down: links.go records a link hint for every query it finds in a
	// `richtext` or `text` field, and a line that only quotes a query should
	// not turn up in anybody's backlinks.
	case "code":
		fmt.Fprintf(b, `<pre class="ms-code"><code>%s</code></pre>`,
			html.EscapeString(n.Str("text")))

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
		r.owner(b, n, c)
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
			if c.shared {
				b.WriteString(dead("ms-task-open", "→"))
			} else {
				fmt.Fprintf(b, `<a class="ms-task-open" href="/p/%s" title="Arbeidsdokument">→</a>`,
					html.EscapeString(n.Page))
			}
		}
		r.owner(b, n, c)
		b.WriteString(`</li>`)

	// A boxed aside: something the reader should stop for rather than read
	// past. Which of the four it is comes off the `slag` field, and everything
	// that differs between them — the colour, the glyph, the word — is one
	// lookup, so adding a fifth is an entry in types.json and a rule in the
	// stylesheet rather than a branch here.
	case "callout":
		kind := calloutKind(n.Str("slag"))
		fmt.Fprintf(b,
			`<div class="ms-item ms-callout ms-callout-%s"><div class="ms-callout-head">`+
				`<span class="ms-callout-mark" aria-hidden="true">%s</span><span class="ms-callout-kind">%s</span></div>`+
				`<div class="ms-callout-body">%s`,
			kind.name, kind.mark, html.EscapeString(kind.word), r.inlineOf(n, "text", c))
		r.items(b, n, depth, c)
		b.WriteString(`</div></div>`)

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

// owner draws who a task is for, as a link to their page.
//
// No `#`. A person is not a tag — a tag is what a page is about, a person is
// somebody who can be given a task and who has a page of their own — and
// writing both the same way made them look like one thing ([ADR-0020]). The
// name is the address: `/kari`.
func (r *Renderer) owner(b *strings.Builder, n *doc.Node, c *ctx) {
	owner := strings.TrimSpace(n.Str("owner"))
	if owner == "" {
		return
	}
	if c.shared {
		b.WriteString(dead("ms-owner", html.EscapeString(owner)))
		return
	}
	fmt.Fprintf(b, `<a class="ms-owner" href="/%s">%s</a>`,
		url.PathEscape(doc.Slug(owner)), html.EscapeString(owner))
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
	if files.IsVideo(stored) {
		r.player(b, href, stored, label)
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

// player draws a video with its caption, the way figure draws a picture.
//
// `preload="metadata"` and no `autoplay`: a page with a video on it should cost
// what a page costs until somebody asks for the video. Metadata alone is enough
// for the browser to draw the right shape and know how long it is, so the page
// does not jump when the file arrives.
//
// The `type` is the one the file will actually be served with, so the browser
// can decide whether it can play it before fetching anything. A source it
// cannot take falls through to the text inside the element, which is a plain
// link to the file — a download beats a black rectangle.
func (r *Renderer) player(b *strings.Builder, href, stored, caption string) {
	kind, _ := files.ServeType(stored)
	fmt.Fprintf(b, `<figure class="ms-item ms-figure ms-video">`+
		`<video controls preload="metadata"><source src="%s" type="%s">`+
		`<a href="%s">%s</a></video>`,
		href, html.EscapeString(kind), href, html.EscapeString(caption))
	if caption != "" {
		fmt.Fprintf(b, `<figcaption>%s</figcaption>`, html.EscapeString(caption))
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

// calloutKind is how one flavour of callout is drawn: the class the stylesheet
// colours it by, the glyph, and the word above the text.
//
// The word is spelled out rather than left to the glyph alone. A coloured box
// with a triangle in it means "careful" to somebody who already knows the
// convention and nothing at all to anybody else, and these are worth reading
// on a page about a sprinkler system.
type calloutStyle struct{ name, mark, word string }

var calloutStyles = map[string]calloutStyle{
	"info":     {"info", "i", "Info"},
	"tips":     {"tips", "★", "Tips"},
	"atvaring": {"atvaring", "!", "Åtvaring"},
	"fare":     {"fare", "!", "Fare"},
}

// calloutKind falls back to info for a value the registry no longer has — a
// page can outlive the choice it was written with, and an unknown flavour
// should still draw as a box rather than vanish.
func calloutKind(v string) calloutStyle {
	if s, ok := calloutStyles[v]; ok {
		return s
	}
	return calloutStyles["info"]
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
// are rendered as that type inside a nested list — and a sub-line with
// sub-lines of its own draws them the same way, one level further in.
// doc.Normalise caps that at doc.MaxItemDepth, so this cannot run away.
func (r *Renderer) items(b *strings.Builder, n *doc.Node, depth int, c *ctx) {
	if len(n.Items) == 0 {
		return
	}
	tag := listTag(n.Type)
	fmt.Fprintf(b, `<%s class="ms-items ms-%s-list">`, tag, n.Type)
	for _, it := range n.Items {
		sub := *it
		sub.Type = n.Type // items carry no type of their own
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
		return r.pageLink(q, res.page, c)
	}

	// A heading with a name in parentheses is a link to that heading, not a
	// transclusion of it. `@dokument/bolk` pulls the section in; `@dokument/bolk()`
	// points at it.
	//
	// This is what the comment here used to promise and could not keep: link
	// text names somewhere you can go, and a heading only became somewhere once
	// the read view started giving every heading an `id`. It does, so the
	// fragment has something to land on and the promise is kept.
	//
	// An empty pair of parentheses means "link it, and use the heading's own
	// words" — the shortest way to write it, and the form that reads best in a
	// sentence that already says what it is pointing at.
	if q.hadLabel && res.node != nil && res.node.Type == "header" && !res.filtered {
		return r.headingLink(q, res.page, res.node, c)
	}

	// Link text names somewhere you can go. A field is a value and a filter is
	// a set; neither is a place, so there would be nothing for the name to
	// point at.
	if q.hadLabel {
		return errChip(q.raw, "namn i parentes verkar berre på ei lenkje til eit dokument eller ei overskrift")
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
	if c.shared {
		fmt.Fprintf(&b, `<div class="ms-tx ms-tx-block">%s`,
			dead("ms-tx-source", html.EscapeString(q.raw)))
	} else {
		fmt.Fprintf(&b, `<div class="ms-tx ms-tx-block"><a class="ms-tx-source" href="/p/%s">%s</a>`,
			html.EscapeString(res.page), html.EscapeString(q.raw))
	}
	r.nodes(&b, nodes, 3, c)
	b.WriteString(`</div>`)
	return b.String()
}

// pageLink renders a link to a whole page. The text is whatever stood in the
// parentheses, or else the page's own title — looked up at render time rather
// than stored, so it cannot drift from what the page is actually called, and
// so renaming a page needs no propagation to keep every link to it honest.
func (r *Renderer) pageLink(q query, slug string, c *ctx) string {
	label := strings.TrimSpace(q.label)
	if label == "" {
		if d, ok := r.src.DocBySlug(slug); ok {
			label = strings.TrimSpace(d.Title)
		}
	}
	if label == "" {
		label = slug
	}
	if c.shared {
		return dead("ms-link", html.EscapeString(label))
	}
	return fmt.Sprintf(`<a class="ms-link" href="/p/%s">%s</a>`,
		html.EscapeString(slug), html.EscapeString(label))
}

// headingLink points at one heading inside a document.
//
// The fragment is the heading's slugged label, which is exactly what `node`
// writes as the `id` when it draws one — the two have to agree, and they agree
// by both going through `doc.Slug`. Do not build the fragment any other way.
//
// An empty name falls back to the heading's own words rather than to the slug:
// `@kafeen/opningstider()` should read as "Opningstider" in the sentence, not as
// "opningstider".
func (r *Renderer) headingLink(q query, slug string, n *doc.Node, c *ctx) string {
	label := strings.TrimSpace(q.label)
	if label == "" {
		label = strings.TrimSpace(n.Label())
	}
	if label == "" {
		label = slug
	}
	if c.shared {
		// The same rule as any other link that leads further into the archive:
		// on a document anybody can open, it is struck out on the server rather
		// than disabled in the browser ([ADR-0024]).
		return dead("ms-link", html.EscapeString(label))
	}
	return fmt.Sprintf(`<a class="ms-link" href="/p/%s#%s">%s</a>`,
		html.EscapeString(slug), html.EscapeString(doc.Slug(n.Label())), html.EscapeString(label))
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
		`<button type="button" class="ms-tx ms-tx-error ms-tx-new" data-newpage="%s" title="Dokumentet finst ikkje — klikk for å lage det">%s</button>`,
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
	// A bare address, linked where somebody wrote one without making a link of
	// it. Deliberately narrow: it must start at a word boundary with a scheme,
	// so a URL inside a markdown link — already held behind a placeholder by
	// the time this runs — and a bare word with a dot in it are both left
	// alone. Trailing punctuation is trimmed by the handler, because a sentence
	// ending in a link should not swallow its own full stop.
	bareURL = regexp.MustCompile(`\bhttps?://[^\s<>"]+`)
	// A quotation, in either the Norwegian guillemets or straight double
	// quotes. The straight ones arrive as `&#34;` because the text has been
	// escaped by the time this runs, which is also what keeps a quote mark
	// inside an attribute of an earlier rule out of reach.
	//
	// Non-greedy, so two quoted phrases on one line stay two quotations rather
	// than becoming one that swallows the words between them. Both marks are
	// required, so a lone inch mark or an unbalanced quote is left as typed.
	quoteRe = regexp.MustCompile(`«([^»]+)»|&#34;([^&]*(?:&(?:amp|lt|gt|#39);[^&]*)*)&#34;`)
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
	// Bare addresses, after the markdown links so that a `[text](url)` is
	// already sealed behind a placeholder and cannot be linked a second time.
	out = bareURL.ReplaceAllStringFunc(out, func(m string) string {
		// A link at the end of a sentence should not eat the punctuation. The
		// closing bracket is trimmed only when there is no opening one to match
		// it, so a Wikipedia address with brackets in it survives.
		trimmed := strings.TrimRight(m, ".,;:!?")
		if strings.HasSuffix(trimmed, ")") && strings.Count(trimmed, "(") < strings.Count(trimmed, ")") {
			trimmed = strings.TrimRight(trimmed, ")")
		}
		href := safeURL(html.UnescapeString(trimmed))
		if href == "" {
			return m
		}
		return hold(fmt.Sprintf(`<a class="ms-url" href="%s" rel="noopener noreferrer nofollow">%s</a>`,
			href, trimmed)) + m[len(trimmed):]
	})
	out = emphasise(out)
	out = hashtagRe.ReplaceAllString(out, `$1<span class="ms-tag">#$2</span>`)
	// Quotations last, so that what is inside one may still be emphasised, be a
	// hashtag, or be any of the fragments already held. The marks are kept as
	// the author typed them rather than swapped for the other pair: this styles
	// what somebody wrote, it does not correct it.
	out = quoteRe.ReplaceAllStringFunc(out, func(m string) string {
		p := quoteRe.FindStringSubmatch(m)
		inner, open, close := p[1], "«", "»"
		if inner == "" {
			inner, open, close = p[2], "&#34;", "&#34;"
		}
		if strings.TrimSpace(inner) == "" {
			return m // an empty pair of quotes is punctuation, not a quotation
		}
		return fmt.Sprintf(`<q class="ms-quote"><span class="ms-quote-mark">%s</span>%s<span class="ms-quote-mark">%s</span></q>`,
			open, inner, close)
	})

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

// loneURL returns the address on a line that holds one and nothing else.
func loneURL(s string) string {
	s = strings.TrimSpace(s)
	if strings.ContainsAny(s, " \t\n") || s == "" {
		return ""
	}
	lower := strings.ToLower(s)
	if !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
		return ""
	}
	if u, err := url.Parse(s); err != nil || u.Host == "" {
		return ""
	}
	return s
}

// card draws a bare address as something worth looking at.
//
// What it can draw depends on what has been read off the site, and that answer
// arrives late by design: `preview.Store.Get` never blocks, so the first render
// of a new link gets the plain card and the next one — after the fetch has
// finished in the background — gets the title and the picture. A page must not
// hang because somebody linked to a slow site.
func (r *Renderer) card(target string) string {
	href := safeURL(target)
	if href == "" {
		return ""
	}
	var c preview.Card
	if r.cards != nil {
		c = r.cards.Get(target)
	}

	site, path := hostAndPath(target)
	var b strings.Builder
	if !c.OK() {
		// Nothing read yet, or nothing to read. Still better than a raw address:
		// the host is the part a person recognises, and the path is the part
		// they do not need.
		fmt.Fprintf(&b, `<a class="ms-card is-plain" href="%s" rel="noopener noreferrer nofollow">`, href)
		fmt.Fprintf(&b, `<span class="ms-card-body"><span class="ms-card-title">%s</span>`, html.EscapeString(site))
		if path != "" {
			fmt.Fprintf(&b, `<span class="ms-card-site">%s</span>`, html.EscapeString(path))
		}
		b.WriteString(`</span></a>`)
		return b.String()
	}

	fmt.Fprintf(&b, `<a class="ms-card" href="%s" rel="noopener noreferrer nofollow">`, href)
	b.WriteString(`<span class="ms-card-body">`)
	fmt.Fprintf(&b, `<span class="ms-card-title">%s</span>`, html.EscapeString(c.Title))
	if c.Description != "" {
		fmt.Fprintf(&b, `<span class="ms-card-desc">%s</span>`, html.EscapeString(c.Description))
	}
	name := c.Site
	if name == "" {
		name = site
	}
	fmt.Fprintf(&b, `<span class="ms-card-site">%s</span>`, html.EscapeString(name))
	b.WriteString(`</span>`)
	if img := safeURL(c.Image); img != "" {
		// `no-referrer` so the site serving the picture is not also told which
		// page of the wiki somebody is reading, and `lazy` so a page full of
		// links does not fetch a dozen images before it draws.
		fmt.Fprintf(&b, `<img class="ms-card-img" src="%s" alt="" loading="lazy" referrerpolicy="no-referrer">`, img)
	}
	b.WriteString(`</a>`)
	return b.String()
}

// hostAndPath splits an address into the part a person recognises and the rest.
func hostAndPath(target string) (string, string) {
	u, err := url.Parse(target)
	if err != nil {
		return target, ""
	}
	host := strings.TrimPrefix(u.Host, "www.")
	path := strings.TrimSuffix(u.Path, "/")
	if u.RawQuery != "" {
		path += "?" + u.RawQuery
	}
	return host, path
}

// loneQuote reports whether a line is one quotation and nothing else.
//
// Read on the raw text, before escaping, so the straight mark here is `"` and
// not the entity it becomes later. Exactly one pair is required: a line holding
// two quoted phrases is a sentence about them, not a quotation of them.
func loneQuote(s string) bool {
	s = strings.TrimSpace(s)
	if len([]rune(s)) < 3 {
		return false
	}
	if strings.HasPrefix(s, "«") && strings.HasSuffix(s, "»") {
		return strings.Count(s, "»") == 1
	}
	if strings.HasPrefix(s, `"`) && strings.HasSuffix(s, `"`) {
		return strings.Count(s, `"`) == 2
	}
	return false
}
