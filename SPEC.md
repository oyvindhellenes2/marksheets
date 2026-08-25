# Marksheets — resolved design

`Marksheets.md` is the original brief. This file records what was actually built and why it
differs where it does.

## Storage

One page is one JSON file in a folder — `pages/gym.json` is the page you reach with `@gym`. The
file name *is* the slug, so renaming a page is renaming a file, and the folder is the whole
database. Set `PAGES_DIR` to put it elsewhere.

Files are written pretty-printed, so they diff cleanly in git, and they are meant to be edited by
hand. The app stats each file per request and re-reads it when the modification time or size
changes, so an external edit shows up on the next page load without a restart.

Writes go to a temp file in the same folder and are then renamed into place. A crash mid-save can
never leave a half-written page — which matters more now that the file is the only copy.

There is **no SQL database**. Nothing needed one: pages were the only table, `updated` comes from
the file's modification time, and slug uniqueness is enforced by the filesystem via `O_EXCL`. The
module has zero dependencies. When something does need an index — full-text search, backlinks,
revision history — the right shape is a cache rebuilt from the files, not a second source of truth.

A file whose JSON is broken, or whose name is not a valid slug, is still listed on the home page,
marked with its error rather than silently disappearing. Slugs are validated by round-tripping
through `doc.Slug`, which strips path separators and dots, so a request can never address a file
outside the folder.

## Document model

A page is one JSON tree. Nesting depth gives header level: the page title is depth 0, so a header
at depth 1 renders as `h1`, depth 2 as `h2`, and so on down to `h6`.

```json
{
  "title": "Gym",
  "children": [
    { "id": "n_gymeq", "type": "header", "text": "Gym equipment",
      "children": [
        { "id": "n_gy01", "type": "text", "text": "Her er ei oversikt…" },
        { "id": "n_gy02", "type": "todo", "done": false, "text": "romaskin", "owner": "øyvind" },
        { "id": "n_gy05", "type": "data", "name": "budsjett", "value": 10000, "unit": "kr" }
      ]
    }
  ]
}
```

Fields sit at the top level of the node rather than inside a `fields` wrapper, and are written in
the order `types.json` declares them — not alphabetically — so a change makes a one-line diff.
`id`, `type`, `children`, `links` and `fields` are therefore reserved and cannot be field names;
`types.json` is rejected at load if it uses one. The older nested `{"fields": {…}}` shape still
loads, so old files need no migration.

Naming the children key after the heading — `"gym-equipment": [...]` instead of `"children"` — was
considered and rejected: it puts the name in two places that can disagree, makes renaming a
key-rebuild instead of a field write, and does not generalise to nodes whose text is a sentence or
empty.

Three departures from the brief, all for the same reason — the original shape could not be relied on
to round-trip:

- **Ordered arrays, not bare objects.** JSON objects have no defined order and cannot hold duplicate
  keys, so two sections both named "Notes" would collide and key order could change on any save.
- **Named fields, not positional lists.** `[todo, "-[ ]", "romaskin", "#øyvind"]` breaks as soon as a
  template gains an optional field, and it stores presentation (`-[ ]`) inside the data, where it can
  contradict the real state. `done` is a boolean; the checkbox is rendered from it.
- **Permanent node ids.** Queries are written against readable slugs but a node's identity is its
  `id`. Without this, renaming a header silently breaks every query pointing at it.

## Tasks and working files

Every new page starts from a template: an `Oppgåver` heading with one empty task.

**Todos live under `Oppgåver` and nowhere else.** The type picker greys them out elsewhere, as do
`⌘1`–`⌘6` and the `[]` shortcut. This is enforced on creation only — todos that already sit
elsewhere (`gym.json` has three under "Gym equipment") keep working untouched.

Two kinds:

- **`todo`** — an ordinary line, as before. Can hold items.
- **`task`** — owns a page of its own. Cannot hold items; the page replaces nesting.

A task's page is its **working file**: scratch space for that one job, while the page holding the
task stays clean. It is created lazily, on the first save after the task has text, and reached only
through its task — never listed on the front page. It uses the same template but with plain todos,
so a working file cannot spawn working files of its own.

The task records its page by slug and never derives it from the text again:

```json
{ "id": "n_x", "type": "task", "done": false, "text": "Klippe hekken",
  "owner": "øyvind", "page": "klippe-hekken" }
```

Rename the task as often as you like; the link is the slug, and the slug never moves. This is the
same principle as the id-based `@`-links: readable text, stable identity underneath.

`parent` is written by the store and never read from a save. The editor sends only title and
children, so trusting the request would clear the field on every save — which detaches a working
file from the task that owns it and drops it onto the front page. A save that tries to set its own
`parent` is ignored.

**Opening it** is a `→` button on the row, not the text itself. Text stays plainly editable — a
hyperlink inside a contenteditable would make click-to-edit and click-to-open the same gesture.
`⌘`-click on the text works too. The button is filled in when the working file has content.

**Finishing a task files it away.** Ticking a task moves it into an `Arkiv` heading inside
`Oppgåver`, created on first use and folded by default, so the tasks heading keeps showing what is
still open. Unticking takes it back out. Ordinary todos just get struck through where they are —
on a working file, reordering lines as you tick them would be the wrong behaviour.

**Deleting is guarded.** A task can only be deleted while its working file is still empty (nothing
but the template). The editor refuses otherwise. Deleting a page takes its working files with it,
since nothing else could ever reach them.

If a task disappears while its page still holds work — a hand-edit, or a save that bypassed the
editor — the page is neither deleted nor stranded: it loses its `parent` and **graduates into an
ordinary page**, listed on the front page like any other. Losing work is not an option, and neither
is leaving a file that nothing in the app can open.

## Line types

Defined in `internal/doc/types.json`, embedded as the default. Set `TYPES_PATH` to your own copy to
change them; `/typar` shows what is currently loaded.

| Type | Fields | Nests | Enter |
|---|---|---|---|
| `header` | text | yes, incl. headers | new text line |
| `text` | text | no | new text line |
| `todo` | done, text, owner | yes, no headers | new todo, owner carried over |
| `list` | text | yes, no headers | new list item |
| `data` | name, value, unit | no | new text line |
| `image` | src, alt | no | new text line |

A type declares `nestable` and `allowsHeaders`, and between them they pick one of three shapes:

- `header` (`nestable` + `allowsHeaders`) holds **children** — this is what gives a page its outline.
- `list` and `todo` (`nestable` only) hold **items** — sub-lines kept *inside* the line.
- `text`, `data`, `image` hold nothing.

**Only headers nest.** A list's or todo's sub-lines are part of the line rather than lines of their
own: they live in an `items` array, carry the same fields as their parent, inherit its type, and
cannot nest further.

```json
{ "id": "n_gy06", "type": "list", "text": "Måndag - beina",
  "items": [ { "id": "n_gy07", "text": "knebøy" },
             { "id": "n_gy08", "text": "markløft" } ] }
```

This is deliberately structural rather than a rule the editor enforces. "Only headers nest" used to
be policed on every Tab, type change and save — and the one time policing failed, five lines were
silently deleted. With sub-lines held inside their parent there is no `children` field on a leaf to
fill wrongly, so the invalid shape cannot be built at all.

Items are still walked like any other line, so tags, `@`-queries and backlinks reach them.

Field kinds: `richtext`, `text`, `slug`, `number`, `bool`, `tag`, `url`. The kind picks the editor
control and tells the query resolver how to match.

## @-queries

Read-only. Nothing pulled in by a query can be edited where it appears.

```
@gym/gym-equipment/budsjett        → 10000 kr, inline
@gym.gym_equipment.budsjett        → the same; . and / are interchangeable
@gym/gym-equipment                 → the whole section, transcluded
@gym/gym-equipment[#øyvind]        → every node under it tagged øyvind
@gym.gym_equipment.#øyvind         → the same, tag as a trailing segment
@gym/gym-equipment[owner=kari]     → matching that field explicitly
```

- The first segment is a page slug; each later segment matches a direct child by slugged label,
  falling back to a deeper descendant so a path may skip a level.
- Slugs normalise spaces, underscores and hyphens alike, so `Gym equipment`, `gym_equipment` and
  `gym-equipment` are the same segment.
- A tag matches either a field of kind `tag` or a `#hashtag` written anywhere in the node's text.
- A filter returns a **flat** set — a matching descendant appears in its own right rather than
  nested under another match.
- A trailing `.` is not swallowed into the path, so a query can end a sentence.
- Failures render as an inline error chip naming the reason; they never break the page.
- Cycles are caught by page (`render.ctx.visiting`) and expansion stops at 6 levels deep.

Inline markdown is `**bold**`, `*italic*`, `` `code` ``, `[text](url)` and `#tag`. Block markdown is
deliberately absent — headers, lists and todos are node types here, not syntax. URLs are restricted
to schemes that cannot execute script.

## Links, ids and renaming

A query is *written* as a readable path but *resolved* by id. On save, every query is resolved once
and its target recorded on the node:

```json
{ "id": "n_ov04", "type": "text", "text": "Heile lista ligg på @gym/gym-equipment",
  "links": { "@gym/gym-equipment": "gym#n_gymeq" } }
```

At render time the id wins and the written path is the fallback. Rename a heading and every link
into it keeps working, because `n_gymeq` did not change — including when you rename it by hand with
the app closed, which is the case any notify-on-save scheme misses. A hand-typed query with no
recorded id resolves by path exactly as before, so the hint is advisory, never load-bearing.

For a filtered query the recorded id is the *scope* the filter runs against; the matching set itself
is recomputed on every render and is never stored.

**Renaming then rewrites the text to match**, as a convenience rather than for correctness. Every
link into the changed page is recomputed from its stored id, so a query that merely passes *through*
a renamed heading is corrected too, not only one that ends at it. Filters survive the rewrite:

```
@gym/gym-equipment/budsjett   →  @gym/utstyr/budsjett
@gym.gym_equipment.#øyvind    →  @gym/utstyr/#øyvind
@gym/gym-equipment[#øyvind]   →  @gym/utstyr[#øyvind]
```

**Backlinks are computed, never stored.** `Store.Backlinks` scans the files on demand. An earlier
design had rendering a page POST a backlink register into the pages it read; that was dropped
because it makes reads write to other files, leaves dangling entries when a link or page is deleted,
and is bypassed entirely by hand-editing. A computed answer cannot go stale, and at this size the
scan is far cheaper than keeping a register honest.

## Saving and history

Saving is **manual**: `⌘S` writes the file and makes a git commit. There is no autosave, so unsaved
work is mirrored into `localStorage` and offered back on the next visit rather than silently kept or
silently dropped.

Git is the historian, never the database:

- The file is written and safe **before** a commit is attempted; a failed commit is reported but can
  never turn into a failed save.
- Only paths inside `PAGES_DIR` are ever staged — never `git add -A`. The app cannot commit its own
  source.
- Commits fall back to an identity of their own when git has none configured, rather than failing.
- Commits are serialised; git's index takes one writer at a time.
- Messages are generated from what changed, so a rename cascade is one commit across every file it
  touched: `Gym: «Gym equipment» → «Utstyr» (2 sider oppdaterte)`. Reverting that one commit undoes
  the heading and every link together.

History is optional. If `PAGES_DIR` is not inside a repository the app runs without it and the home
page offers to start one.

In this checkout `pages/` sits inside the project's own repository, so saves commit into the same
history as the code. It used to be a separate repo; those 60 commits were brought across with
`git subtree add --prefix=pages`, and `pages/` is an ordinary directory now. Only paths under
`PAGES_DIR` are ever staged, so a save still cannot commit source.

## Editor keys

| Key | Does |
|---|---|
| `Enter` | new line below, same level; splits the line at the caret |
| `Enter` on an empty line | step out one level — the brief's "double enter", without a timer |
| `Tab` / `⇧Tab` | in / out one level, carrying any children along |
| `⇧Enter` | start a new heading beside the one you are in, after its contents |
| `⌥Enter` | soft line break inside the line |
| `Backspace` at the start of an empty line | delete it, caret to the line above |
| `#`, `-`, `[]`, `= ` at line start | switch type |
| `⌘1`–`⌘6` | switch type |
| `↑` / `↓` | move to the line above or below, caret at the end |
| `↑` on the first line | conjure a new line above, which vanishes again if you leave without typing |
| `⌘Z` / `⇧⌘Z` | undo / redo |
| `⌘S` | save the file and commit it |

The brief specified double-Enter to outdent and Shift+Enter for a new header. Both were changed:
double-Enter cannot fire without Enter firing first, it left no way to type a blank line, and it gave
no outdent for the ordinary case. Tab/Shift+Tab is what every outliner uses and what hands expect.

Arrow keys move by one press. A single-line field has nowhere for the browser to move the caret, so
it parks it at the edge of the field and the move costs a second press; the editor takes over
instead and lands at the end of the target line. In a field holding soft line breaks the browser
still handles movement until the caret reaches the field's first or last line.

Indentation is validated as you go — the type menu greys out any type the current parent cannot hold.

**Tab never moves a line out of its heading.** Only two things can be indented:

- a **header** moves between outline levels, taking its contents with it;
- a **list or todo** becomes an item of the nearest line above that is not itself an item, provided
  that line is of the same type — an item inherits its parent's type, so a todo becoming an item of
  a list would lose its checkbox and owner.

Everything else has no indentation of its own; where it sits is decided by the heading it is under.
`⇧Tab` on an item makes it a line again, directly after the line it belonged to.

Because a leaf can no longer change depth, `Enter` on a heading opens the first line *inside* it
rather than a sibling beside it. That is what replaces tabbing every new line into place — and it
is why `⇧Enter` starts the *next* heading at the level of the one you are in, placed after
everything it contains. Without it, moving on to a new section meant typing `#` (which makes a
child) and then outdenting. The soft line break moved to `⌥Enter` to make room; the original brief
asked for `⇧Enter` to make a heading, and that is what it does again.

**Undo works on the model, not the DOM.** Every structural edit rebuilds the rows, which discards
whatever undo state the browser held, so `⌘Z` cannot be left to the browser. Each step keeps a copy
of the rows from before a change together with the caret position, and `⌘Z` puts both back; the
handler calls `preventDefault` so there is one history rather than one per contenteditable field.

Typing is grouped rather than recorded per keystroke: a run in the same field is one step, ended by
a pause of 700ms, a move to another field, or finishing a word — so undo comes back word by word.
Structural edits are always their own step. Folding is not undoable, being a view preference rather
than content. The stack holds 200 steps.

**Headings fold.** A row with children gets a twisty; folded rows show how many lines are hidden.
Folding is a view preference, kept per page in `localStorage` and never written into the document.
Arrow keys skip what is folded away. On a page you have never opened, `Arkiv` headings start folded
— finished work should be out of the way by default — but the moment you fold or unfold anything
there, your own choice is stored and takes over.

## Implementation notes

Go + HTMX, following `../mystuff/STACK.md`, on port 3003. No auth: single user. Pages are JSON
files (see **Storage** above), so the SQLite dependency from `STACK.md` is gone — `go.mod` lists no
requirements at all.

**The one departure from STACK.md:** the page editor is vanilla JS, not HTMX. A keyboard-driven
outliner needs per-keystroke local state, and a server round-trip per keypress would feel bad. HTMX
still drives the home page and the read-view swap. The editor holds rows as a **flat list with a
depth**, because indent, outdent and block moves are then slices and arithmetic; the tree is rebuilt
only on save (`nest()` in `editor.js`).

Saving is a debounced full-document PUT — the editor owns the tree, so there is nothing to patch.
The editor holds no lock on the file, so editing the same page in the app and in a text editor at
the same time means last-write-wins.

Static assets and templates are embedded with `embed.FS`, so **the server must be restarted to pick
up CSS or JS edits**.

## Not built yet

Formulas and aggregates (`=sum(@gym.*.budsjett)`), which is what would make the "spreadsheet" half
literal — the dependency graph they need hooks into the existing cycle guard in `render.ctx`.

Also absent: search, drag-to-reorder, restoring a page from a history entry (the log is shown but
not yet actionable), and renaming a *page* — only headings propagate today, since a page rename is a
file rename and needs its own handling.

`Normalise` **lifts orphans rather than dropping them**: a node whose type cannot nest hands its
children up to its own level instead of losing them. This exists because it once did drop them — an
outdent bug produced a tree with todos under a text line, and the save silently deleted five lines.
Malformed input should be repaired, never quietly discarded.

Undo history lives only in the page: it does not survive a reload, and it is not written to the
file. Git history is the durable record, per save; `⌘Z` is the working one, per edit.
