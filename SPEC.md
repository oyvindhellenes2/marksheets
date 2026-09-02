# Marksheets — resolved design

`Marksheets.md` is the original brief. This file records what was actually built and why it
differs where it does. It is written in the present tense and edited freely; the decisions behind it,
with the alternatives that were weighed and rejected, are in [`adr/`](adr/).

## Storage

One page is one JSON file in a folder — `pages/gym.json` is the page you reach with `@gym`. The
file name *is* the slug, so renaming a page is renaming a file, and the folder is the whole
database. Set `PAGES_DIR` to put it elsewhere.

Files are written pretty-printed, so they diff cleanly in git, and they are meant to be edited by
hand. The app stats each file per request and re-reads it when the modification time or size
changes, so an external edit shows up on the next page load without a restart.

Writes go to a temp file in the same folder and are then renamed into place. A crash mid-save can
never leave a half-written page — which matters more now that the file is the only copy.

There is **no SQL database** ([ADR-0001](adr/0001-pages-are-files.md)). Nothing needed one: pages were the only table, `updated` comes from
the file's modification time, and slug uniqueness is enforced by the filesystem via `O_EXCL`. The
module has zero dependencies. Nothing has needed an index yet either: backlinks, the unpublished
set, the people index and **search** are all worked out from the files on the request that asks for
them ([ADR-0018](adr/0018-search-is-a-scan.md)). If one ever is needed, the right shape is a cache
rebuilt from the files, not a second source of truth.

A file whose JSON is broken, or whose name is not a valid slug, is still listed in the sidebar and
still found by search, rather than silently disappearing. Slugs are validated by round-tripping
through `doc.Slug`, which strips path separators and dots, so a request can never address a file
outside the folder.

## Tags

**Every page carries at least one hashtag** ([ADR-0009](adr/0009-every-page-has-a-hashtag.md)). They are asked for on the form that makes a page, and
changed on the page itself, under the title. They say what a page is *about*, which its name only
sometimes does — `Byggje ny disk` is a job; `#ombygging` is the thing it belongs to.

They are the page's own, not a line's: doc-level, beside the title, and not to be confused with the
`#hashtag` written inside a line's text, which is content and reachable by a filter. Tags are stored
as slugs — `["ombygging", "uteplass"]` — so the `#` in front of one is presentation. They are typed
as words and separated by spaces or commas alike; a tag of more than one word is hyphenated.

**A tag is a link, wherever it appears** — in the sidebar, under the title in the editor, and in the
read view. In the sidebar it narrows the list of pages in place; elsewhere it is `/?emne=hage`,
which the sidebar reads on the way in.

**Typing a tag completes it.** The field offers every tag already in use that starts with what has
been typed and is not already on the page; `↑`/`↓` move, `Enter` or `Tab` takes one, `Esc` dismisses.
The form that makes a page offers the same list through a plain `<datalist>`. Without this, the
second page about the cabin gets `#hytte` because the first one said `#hytta`, and neither finds the
other.

**The sidebar indexes every tag in use**, above the pages, most-used first and then alphabetical.
Clicking one narrows the list to the pages carrying it. Working files are left out of the count as
well as the list: they are reached through their task and nowhere else, so a tag leading to one
would be a way into a page the index hides.

The list is cut off at **five rows** with a `Vis meir` under it, because the pages are what the
sidebar is for and thirty tags would push them off the bottom of it. Five *rows*, not five tags —
how many fit on a line depends on how long the words are, so the clamp is measured from a real chip
in the browser rather than written into the stylesheet, and the button appears only when something
is actually behind it.

The last tag cannot be removed: with none, a page would be findable by nothing but its name. The
editor refuses and says so; the store makes the same guarantee for files written by hand, filling in
the page's own slug for a page that arrives with none — which is what happened to every page that
existed before tags did.

A working file inherits the tags of the page whose task opened it. It is part of that job, and
nobody would think to tag scratch space by hand.

## Document model

A page is one JSON tree. Nesting depth gives header level: the page title is depth 0, so a header
at depth 1 renders as `h1`, depth 2 as `h2`, and so on down to `h6`.

```json
{
  "title": "Gym",
  "tags": ["trening", "utstyr"],
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

**`Oppgåver` is pinned to the top of every page, and it is the app's line rather than yours**
([ADR-0008](adr/0008-the-tasks-heading-is-furniture.md)). It
cannot be typed in, renamed, moved or deleted: it is drawn as a label in the accent colour, with no
field to put a caret in, and the caret steps over it on its way up the page. `doc.Normalise` puts it
back — at the front, made from the template if there is none — so a file edited by hand or written
before this rule arrives with one all the same. The editor does the same for the one document that
never goes past the server, a draft coming back out of `localStorage`.

**The read view leaves the whole section out** — the heading and every task under it. `Les` is for
reading the page, and the task list is working state rather than something anyone reads: it is where
the page gets worked on, not what the page says. A query still reaches it, since
`@side/oppgåver[@kari]` is somebody asking for the tasks rather than the page offering them.

Nothing can sit above it, which took away `↑`-at-the-top as a way to open a line before the first
one. That gesture is gone rather than pointed somewhere else.

**A page opens with the caret on the first line after the section.** Opening a page to write on it is
the common case; reviewing what is still to do is the one you scroll up half a screen for. The whole
block is stepped over, not just the heading — landing among the tasks would be landing in the part
of the page that is not the page.

**A new page starts with one empty text line under the tasks**, and that is where the caret lands.
A `text` line, not a heading: the caret is already in it, so it has to be the thing you most often
want to write next, and clearing a heading before typing a sentence is backwards. A heading is one
`#` away. It is also the only body text a page carries before anything is typed — every other line
finds its place under a heading.

`doc.Template` is that whole starting content; `doc.TasksBlock` is the heading and its one task
without the body line, and it is what `Normalise` puts back when a document arrives without a tasks
heading. Repairing somebody's existing page must not also drop a blank line into it.

A task's **owner** is a person — a field of kind `user`, picked from the people who have signed in,
written without a `#` and linking to their page ([ADR-0020](adr/0020-a-person-is-not-a-tag.md)). A
task made by somebody is theirs until they say otherwise; there is no default name in the software.

**Todos live under `Oppgåver`, and `Oppgåver` holds nothing else.** Both halves are enforced the
same way: the type picker greys out what may not be made, as do `⌘1`–`⌘6` and the `#`/`-`/`[]`/`= `
shortcuts. The two task types are the exception — outside `Oppgåver` the menu **leaves them out**
rather than greying them: a task is made where tasks live, so a permanent pair of disabled rows was
the menu explaining a rule instead of offering anything. Inside `Oppgåver` the one that applies is
listed, because that is where it is real. A page's tasks are `task` lines; a working file's are plain `todo`s, since a working file
cannot open working files of its own.

The heading carries a `+` button, because it has no caret to press `Enter` in. Without it, ticking
or deleting the last open task would leave nowhere to write the next one — the button puts a fresh
task at the end of the open list, before `Arkiv`.

This is enforced on creation only — lines that already sit somewhere they could not now be made
(`gym.json` has three todos under "Gym equipment") keep working untouched.

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

`parent` is written by the store and never read from a save. The editor sends only title, tags and
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

### A person, and everything that is theirs

**`/kari` is Kari** ([ADR-0020](adr/0020-a-person-is-not-a-tag.md)): who she is, and every task and
todo carrying her name, gathered from every page and grouped by the page it was written on. Open
first, finished folded away behind a count. Working files are included — a todo on one is still
somebody's job — and each says which task it hangs off, since that is most of what it means.

It is the one view that starts somewhere other than a page. A task is written where the work is,
which is right for writing it and no use at all for answering *what am I meant to be doing*. The
address is the name, the way a page's address is its title, and it keeps its Norwegian letters:
`/øyvind`, not `oyvind`.

**Who counts as a name is the field kind, not the field name.** Any field of kind `user` holding a
value puts its line on that person's page — the same rule `@side[@kari]` matches on, so a type that
grows such a field is picked up without being told about, and the two cannot drift apart.

**An owner is a person, not a word.** The field is a menu of the people there are, so a name that is
not one of them cannot be typed. A name written before that person had an account is kept and
offered back as "(ukjend)" rather than dropped — content is never discarded to make a rule true —
and their page still renders, as somebody who has not logged in.

**Nothing is stored for it.** The pages are read and the answer assembled per request, the same
bargain as backlinks and the unpublished set: an answer worked out on demand cannot disagree with
the files it was worked out from.

Two ways in: your own name in the footer, and the name beside a task in the read view, which is a
link straight to that person's page.

### Signing in

**Pocket ID, over OIDC** — discovery, the authorization code flow with PKCE, and one call to
`userinfo`. The ID token is deliberately not verified locally, which is what would otherwise drag
JWKS and RS256 into a module with no dependencies: the access token is exchanged over TLS with the
provider and then spent at the provider, so nothing here is trusted on its own signature.

**The sign-in screen is a page of ours, not a bounce.** `/logg-inn` is drawn with the app's own
chrome and carries one button; `/logg-inn/start` is the step that actually leaves. A redirect
straight out would be one fewer click and worse: a session that runs out mid-sentence would throw
you at another site with no explanation, there would be nowhere to put "that took too long, try
again", and the app would never once say whose door it is sending you to.

**Signed out, the chrome is a name and a way in.** No sidebar, no search, no footer — and the
navigation data is not even assembled, so a stranger cannot be shown what the wiki has in it by a
template edit somebody makes later. Signed in, the right of the header carries your name and leads
to your page.

Configured with `AUTH_ISSUER`, `AUTH_CLIENT_ID`, `AUTH_CLIENT_SECRET` and, behind a proxy,
`AUTH_BASE_URL`. The redirect URI to register with the provider is `<AUTH_BASE_URL>/logg-inn/attende`. **With no issuer the app runs as one local user** and no login screen, exactly as
it did before any of this — `AUTH_LOCAL` names them. That is not a way in to a configured instance:
with an issuer set every request is checked and there is no local user to fall back to. It is what
keeps the app runnable, and testable, without an identity provider standing behind it.

Sessions live in memory, so a restart signs everybody out — for a handful of people that is a login,
not an outage, and it keeps sessions from being a second thing to persist and expire. Signing out
ends this app's session and not the provider's; ending that one from here would sign somebody out of
everything else they had open.

**Who has signed in is remembered beside the pages, never in them** — `USERS_PATH`, defaulting to
`brukarar.json` next to the page folder. The page folder is a git repository with a public remote,
and an email address is not something to publish by accident; a `.json` file in there would also be
counted as a page.

## Line types

Defined in `internal/doc/types.json`, embedded as the default. Set `TYPES_PATH` to your own copy to
change them; `/typar` shows what is currently loaded.

| Type | Fields | Nests | Enter |
|---|---|---|---|
| `header` | text | yes, incl. headers | new text line |
| `text` | text | no | new text line |
| `todo` | done, text, owner | yes, no headers | new todo, owner carried over |
| `task` | done, text, owner | no — its page is the nesting | new task, owner carried over |
| `list` | text | yes, no headers | new list item |
| `ordered` | text | yes, no headers | new numbered item |
| `data` | name, value, unit | no | new data line; a blank one becomes a text line |
| `table` | name, plus columns and rows of its own | no | new row; a blank row leaves the table |
| `file` | file, name | no | new text line |

A field's **kind** decides both the control the editor draws and how a query reads the value:
`richtext`, `text`, `slug`, `number`, `bool`, `tag`, `user`, `file`, `url`. Two of those are near
neighbours and are deliberately not the same thing — a `tag` is a subject, what a line is *about*; a
`user` is a person, who a line is *for*.

A type declares `nestable` and `allowsHeaders`, and between them they pick one of three shapes:

- `header` (`nestable` + `allowsHeaders`) holds **children** — this is what gives a page its outline.
- `list` and `todo` (`nestable` only) hold **items** — sub-lines kept *inside* the line.
- `text`, `data`, `image` hold nothing.

**Only headers nest** ([ADR-0003](adr/0003-only-headers-nest.md)). A list's or todo's sub-lines are
part of the line rather than lines of their own: they live in an `items` array, carry the same fields
as their parent and inherit its type.

A sub-line may hold sub-lines of its own, and there it stops — **two levels, `doc.MaxItemDepth`**
([ADR-0015](adr/0015-sub-lines-go-two-deep.md)). Below that is what a heading is for. Anything
arriving deeper — a hand-written file, a paste — is lifted into the deepest level there is rather
than dropped, the same bargain `Normalise` makes everywhere else.

```json
{ "id": "n_gy06", "type": "list", "text": "Måndag - beina",
  "items": [ { "id": "n_gy07", "text": "knebøy",
               "items": [ { "id": "n_gy09", "text": "5 × 5" } ] },
             { "id": "n_gy08", "text": "markløft" } ] }
```

This is deliberately structural rather than a rule the editor enforces. "Only headers nest" used to
be policed on every Tab, type change and save — and the one time policing failed, five lines were
silently deleted. With sub-lines held inside their parent there is no `children` field on a leaf to
fill wrongly, so the invalid shape cannot be built at all.

Items are still walked like any other line, so tags, `@`-queries and backlinks reach them.

### Files

A `file` line is an **upload**, not a link. The file is stored, the page keeps its name, and the two
travel together ([ADR-0013](adr/0013-files-are-uploads-beside-the-pages.md)).

Attachments live in `filer/` **inside the page folder**, so they are in the same backup, the same git
repository and the same publish as the pages that show them. Publishing a page stages the files it
shows — a published page with a picture nobody else has is worse than no picture.

They are kept under a readable name, the one they arrived with, slugged the way a page title is:
`Skisse av Disken.PNG` becomes `skisse-av-disken.png`. A name already taken gets a number, the same
as a page slug does. The folder is meant to be opened in a file manager and read in a diff like
everything else here, which is what rules out content-addressed names.

**Drop a file on the editor and it attaches.** Dropped on a `file` line that is still empty, it
fills that line; anywhere else the dropped files become new `file` lines under the line they landed
on, in the order they were dropped. "Under" means the same place `Enter` opens a line: the first line
*inside* a heading, and directly after anything else — a file dropped on a heading joins that
section rather than being pushed out the far end of it. A whole drop is one edit, so one `⌘Z` takes it all back. The
caption defaults to what the file was called before it was uploaded, without the extension.

The handlers sit on the document rather than on the editor, and that is the important half: without
them the browser treats a stray drop as "open this file", which navigates away from the editor and
takes any unsaved work with it.

**A file that is not a picture gets a box**, with what kind it is, what it is called, and how big it
is — and, where the file is a PDF, the document itself. The preview is an `<iframe>` pointing at the
file: every browser has a PDF viewer built in, so it costs no dependency and no conversion step, and
the viewer runs the document in its own sandbox rather than as script on this origin. That is the
same reasoning that put PDF on the inline allowlist and keeps SVG off it.

A box whose file is missing from disk says so rather than offering a link that does nothing.

**What is drawn in the page is a short allowlist** — PNG, JPEG, GIF, WebP, AVIF, PDF — served with
that exact content type and `nosniff`. Everything else is sent as a download. SVG is deliberately not
on the list: it is an image to look at *and* a document that can run script, and an attachment is
served from the same origin as the app, so an inline SVG would be running script here. It uploads,
stores and links like any other file; it just does not draw.

A stored name has to round-trip through `files.StoredName` before anything opens it, so a request
cannot address a file outside the folder — the same guard, and the same reasoning, as `Store.path`
for pages. A page that names an attachment outside the folder is ignored rather than obeyed, which
also keeps such a name out of a commit.

`image` was the type before this, and held a URL. It is migrated on load: the type becomes `file`,
`alt` becomes `name`, and the old `src` is kept on the node and still drawn, so a page written
before uploads existed keeps its picture.

### Tables

A `table` is one node and one grid. It carries its own shape beside its fields
([ADR-0011](adr/0011-a-table-is-its-own-type.md)):

```json
{ "id": "n_tb", "type": "table", "name": "leverandorar",
  "columns": ["Leverandør", "Vare", "Pris"],
  "rows": [
    { "id": "n_r1", "cells": ["Solberg brenneri", "bønner", "**189** kr/kg"] },
    { "id": "n_r2", "cells": ["Nordbakst", "brød", ""] } ] }
```

**The columns are declared once, for the whole table.** That is the whole point of the type: a table
whose columns lived on each row would be rows that can disagree, and renaming a column on row five
would silently make it a different column. Declared once, the read view also has a header row to
draw — and only draws one when some column is actually named.

**Cells are positional**, which is a deliberate exception to "named fields, not positional lists"
above. The rule there guards against a value drifting from a schema kept somewhere else; here the
columns are on the *same node* as the cells, so a column change rewrites the table in one step and
nothing can drift. Keying cells by column name would also make two columns with the same heading
impossible, and tables have those.

A table is made rectangular on load and on save: at least one column and one row, every row as wide
as the columns. A short row is padded and a long one trimmed — repair, not refusal, so a table typed
into the file by hand still opens.

The table's `name` is what a query addresses it by (`@prisar/leverandorar` pulls the whole table in)
and what the read view prints as a caption. It is optional; a table without one is a layout rather
than a thing you point at.

`data` is unchanged and is still the right type for a **named scalar** — `budsjett = 10000 kr`, the
thing `@hytta/budsjett` resolves to inline. A table is for a grid; a data line is for a number with
a name. Widening `data` into a table would have left the query with no answer to what
`@side/bolk/namn` returns.

Field kinds: `richtext`, `text`, `slug`, `number`, `bool`, `tag`, `url`. The kind picks the editor
control and tells the query resolver how to match.

**A `number` field is a preference, not a cage.** What was typed is stored as a number when it *is*
one, and kept as it was typed when it is not — so `budsjett` lands in the file as `10000`, readable
and sortable and something a future sum can add, while `adresse` keeps `"Storgata 4B"`. A data line
is a name and a value, and plenty of values are not numbers.

An empty value stays empty rather than becoming `0`, and renders as nothing. It used to become a
literal zero on save, so a line meant to read `epost oyvind@me.com` read `epost 0 oyvind@me.com`
instead. A *stored* zero is a real value and still prints; it is the empty field that prints nothing.
Either half of `value unit` may be missing, and what is left stands on its own.

## @-queries

Read-only. Nothing pulled in by a query can be edited where it appears.

```
@gym                               → a link to the page, named by its title
@gym()                             → the same
@gym(alt om gymmen)                → a link named by you
@gym/gym-equipment/budsjett        → 10000 kr, inline
@gym.gym_equipment.budsjett        → the same; . and / are interchangeable
@gym/gym-equipment                 → the whole section, transcluded
@gym/gym-equipment[#utstyr]        → every node under it tagged #utstyr
@gym.gym_equipment.#utstyr         → the same, tag as a trailing segment
@gym/oppgåver[@kari]               → every node whose named person is kari
@gym/oppgåver[owner=kari]          → matching that field explicitly
```

**One segment points; more than one pulls.** A page on its own is a *link*. It used to transclude
the whole page, and that was never something anyone wanted — it was what happened by accident when
a page was merely mentioned in a sentence. Across every page in use at the time of the change, the
bare form appeared exactly **zero** times deliberately, so nothing had to be migrated.

The name comes from the parentheses, or from the page's own title when they are empty or absent.
The title is read **at render time**, never stored, so it cannot drift from what the page is
actually called — and renaming a page will need no propagation to keep every link to it honest.

Parentheses on anything else are refused with an error chip. A field is a value and a filter is a
set; neither is a place, so there would be nothing for a name to point at. Headings will qualify
once a `#fragment` has something to land on — today only the read view emits heading ids, and it
has no URL of its own.

A link records its target like any other query, so **linking to a page puts you in its backlinks**,
which a markdown link never did.

**The editor helps you write one, segment by segment.** Typing `@` and a character or two offers the
pages whose slug or title starts with it; a `/` then offers what is inside whatever the path has
reached so far, as deep as you care to go. `Tab` completes the selected entry, `↑`/`↓` move, `Esc`
dismisses, and while the list is open `Tab` completes rather than indents.

Only headings and data lines are offered past the first segment. Any node is addressable by its
slugged label, but nobody writes `@side/ei-heil-setning`, and the whole body of a page in the list
would bury the two things people actually address.

Only `/` opens the next level, though a query may equally be written with dots — `@hytta.` at the
end of a sentence would otherwise offer a list of sections every time someone finished a thought.

Working files are left out of the page list: they are reachable through their task and nowhere else,
so offering one here would be a way into a page the front page hides.

**A query naming a page that does not exist offers to make it.** In the read view it is drawn as a
dashed button rather than an error chip, and clicking it asks whether to write that page, then makes
it and goes there. A link to a page that is not written yet is nearly always a page waiting to be
written, not a mistake. Only a missing *page* is offered this way — a path into a page that exists
but has no such section is an ordinary error, because there is nothing to create.

**A query that resolves is marked as you write it**, so you can see that it took. The page is checked
against the list the editor already holds; anything deeper is checked by **asking the server**, which
is also what fills the completion list. Matching a segment means "a direct child, else a descendant
deeper down", and a second copy of that rule in the editor would drift from the one in `query.go` the
first time either changed. Answers are cached — the same few paths are checked on every marking pass
— and forgotten for a page when it is saved, or for every page when the window regains focus.

Nothing is ever marked as *broken*. While you are still typing `@s`, `@sk`, `@ska`, not resolving yet
is the ordinary state, and a path waiting on an answer from the server looks exactly the same as it
did a keystroke earlier, so nothing flickers.

Marking replaces the field's nodes, so it runs a moment after typing stops rather than on every
keystroke — under a moving caret, per-character rewriting is how a contenteditable starts fighting
you.

- The first segment is a page slug; each later segment matches a direct child by slugged label,
  falling back to a deeper descendant so a path may skip a level.
- Slugs normalise spaces, underscores and hyphens alike, so `Gym equipment`, `gym_equipment` and
  `gym-equipment` are the same segment.
- A tag matches either a field of kind `tag` or a `#hashtag` written anywhere in the node's text.
- A filter returns a **flat** set — a matching descendant appears in its own right rather than
  nested under another match.
- A trailing `.` is not swallowed into the path, so a query can end a sentence — and neither the
  filter nor the parentheses are read once that has happened, so `@gym. (som nemnt)` stays prose.
- The parentheses must touch what comes before them. `@gym (ikkje eit namn)` is a link followed by
  an ordinary aside.
- Failures render as an inline error chip naming the reason; they never break the page.
- Cycles are caught by page (`render.ctx.visiting`) and expansion stops at 6 levels deep.

The label on a transclusion is a link back to the page the content was borrowed from, so you can
follow a number to where it is kept.

Inline markdown is `**bold**`, `*italic*`, `` `code` ``, `[text](url)` and `#tag`. Each construct is
parked behind a placeholder as soon as it is rendered, so a later rule cannot reach inside what an
earlier one produced — the hashtag rule used to rewrite the `#` *inside* an `href`, which ends the
attribute early and destroys the link, and it reached into `` `code` `` and changed what the code
said. Emphasis still applies to a link's text; only the URL is sealed.

An `@` that follows a letter or digit is not a query. Without that rule `mailto:ein@stad.no` was
read as a query for a page called `stad`, and `post@menyen.no` would quietly have become a link to
a page that happens to exist. RE2 has no lookbehind, so the preceding character is checked in
`queryAt` rather than in the pattern.

Block markdown is
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
@gym.gym_equipment.#utstyr    →  @gym/utstyr/#utstyr
@gym/gym-equipment[@kari]     →  @gym/utstyr[@kari]
```

**Backlinks are computed, never stored.** `Store.Backlinks` scans the files on demand. An earlier
design had rendering a page POST a backlink register into the pages it read
([ADR-0001](adr/0001-pages-are-files.md)); that was dropped
because it makes reads write to other files, leaves dangling entries when a link or page is deleted,
and is bypassed entirely by hand-editing. A computed answer cannot go stale, and at this size the
scan is far cheaper than keeping a register honest.

## Saving and publishing

Saving and history want different rhythms — durability constantly, history at the points you
choose — so they are two acts, not one.

**Saving is automatic.** The editor writes the file about a second after you stop typing. A copy
still goes to `localStorage`, because autosave is a timer and the gap between the last keystroke
and the next tick is exactly where a crash would land; a draft newer than the file is offered back
on the next visit rather than silently kept or silently dropped.

**Publishing is deliberate, and it lives in the sidebar**
([ADR-0014](adr/0014-publishing-lives-on-the-home-page.md),
[ADR-0019](adr/0019-the-front-page-is-a-page.md)). `Publiser` commits every page that has changed —
each on its own, with its own message and its own place in that page's history — and then pushes
once.

It is not on the page itself, because a button there would be lying. A commit can be limited to one
page; a push sends the whole branch, and git has no way to send part of one. That is the same limit
that gave the pages a repository of their own. So the button belongs where "everything" is true,
which is the chrome — it used to be the home page, and is the sidebar now that there is no home
page. The editor still says where a page stands — `Lagra · ikkje publisert` — it just has no button
claiming to change that alone.

Each commit carries its author, so `git log` in the pages repository says who wrote a page rather
than crediting the machine the app runs on. With nobody signed in it says nothing and git's own
identity stands.

**`⌘S` is bound everywhere except the editor.** There, hands press it meaning "save what I typed",
which happened on a timer a second ago; making that push to everybody would be the most expensive
misunderstanding in the app.

Until then the work exists on this machine and nowhere else, and the sidebar says so: a page whose
file differs from what has been published carries a dot, and the button carries the count. That
state is worked out from git on every request and never stored — the same reasoning as backlinks,
and it covers both kinds of unpublished work, edited-but-not-committed and committed-but-not-pushed,
because both are equally invisible to everyone else.

The mark is deliberately **not** red. Red already means a file that will not parse, and with
autosave "unpublished" is the ordinary state of every page you have touched — a wall of red would
teach you to ignore the colour exactly where it needs to alarm you.

### When two people are on one page

**A save answers for the version it started from** ([ADR-0021](adr/0021-a-save-answers-for-what-it-read.md)).
The editor is handed the file's stamp when it loads a page and sends it back with every save; a save
whose version is no longer what is on disk is refused, before anything is written, before task pages
are created or removed, and before links are rewritten. The check and the write are held under one
lock, or the two saves this exists to separate could both check, both pass and both write.

The version is modification time and size — the pair the page cache already trusts to decide whether
a file has changed. Not a content hash: a hash would say "the same bytes are back" after an undo,
which sounds better and is worse, since two people would then both be allowed to save having each
seen a different middle.

**A refusal stops the editor rather than retrying.** Autosave halts, a bar says who saved and that
nothing of yours was written, and the draft in `localStorage` stands. Take theirs, or keep yours —
the second being a deliberate overwrite, made once, by somebody who has been told what it costs.
Retrying would either lose their work or spend the afternoon failing once a second.

**Presence makes it rare.** An open editor says hello every twenty seconds and is told who else is on
the page. In memory, advisory, forgotten on restart: a courtesy that makes a collision unlikely,
while the version check is what makes one harmless.

Two people editing *different lines* of the same page is not a conflict in this data model — nodes
have permanent ids and fields have names, so a three-way merge would be per-node set arithmetic
rather than text diffing. That is the elegant answer, and it is deliberately **not built**: the
refusals are what will say whether it is needed, and how often.

A save with no version — the restore path, and anything writing a whole document without having had
one open — still goes through unchecked. It is not answering for anything it read.

Because autosave takes away "just don't save" as the way to change your mind, **the history is the
way back**. `Historikk` lists the commits that touched the page; opening one renders the page as it
stood then, and `Hent tilbake denne versjonen` writes that content back.

The list is a **panel above the document**, not a section under it, and the same button closes it —
it stays lit while it is open, so it reads as the way back out rather than as the same offer again.
It opens where you are already looking; underneath the page it was as far from the button as it
could be, and on a long page you had to go and find whether anything had happened.

Getting an old version back is a step *forward*, never a rewrite: the content returns as an ordinary
unpublished change, which you then publish like anything else. Nothing in the history is removed, so
every commit made since is still there — including the one you just moved away from. It reads out of
git with `show` rather than `checkout`, so the index is left alone too and a restore cannot leak
into a hand-run `git commit`.

A short-lived "discard back to published" button was built first and then removed: it was this same
operation with the version fixed to the newest one, and one mechanism that can reach any version is
better than two that overlap.

The version you are shown is that page alone, read out of history. Its `@`-queries are resolved
against the other pages as they are **now** — showing them as they were would mean rebuilding the
whole folder at that commit.

Git is the historian, never the database ([ADR-0004](adr/0004-git-is-the-historian.md)), and the
layering says so at each step:

- The file is written and safe **before** a commit is attempted; a failed commit is reported but can
  never turn into a failed save.
- The commit is made and safe **before** a push is attempted; a failed push is reported but can
  never turn into a failed commit. The work is in the history either way, it just has not left the
  machine, and the page stays marked unpublished until it does.
- With no remote configured, publishing commits and says there was nowhere to send it. "Published"
  then falls back to meaning committed, so nothing is left marked forever.
- Only paths inside `PAGES_DIR` are ever staged — never `git add -A`. The app cannot commit its own
  source.
- Commits fall back to an identity of their own when git has none configured, rather than failing.
- Commits are serialised; git's index takes one writer at a time.
- A message names the page and which version of it this is: `Kafeen v3`. The number counts publishes
  under the page's *current* name, so a title that changes starts again at v1 — «Hytta v9» meaning
  the ninth version of something once called «Hyttebok» would be a quiet lie. Only the previous
  commit is consulted, which is all that rule needs.
- A rename that happened along the way is named after the version: `Kafeen v3 — «Gym equipment» →
  «Utstyr»`. It is one commit across every file it touched. Reverting that one commit undoes the heading and every
  link together. One publish now covers many saves, so the renames are accumulated as they happen
  and spent when you publish; losing that on a restart costs a good message and nothing else.
  Only **heading** renames count. Links are matched against every node, since a query can point at
  any of them, but calling an edited text line a renamed heading would be false — and with autosave
  it would be the common case.
- Publishing a page takes its working files with it. A published page whose task pages stayed
  behind would show links into nothing.

History is optional. If `PAGES_DIR` is not inside a repository the app runs without it and the home
page offers to start one.

**The pages have a repository of their own** ([ADR-0006](adr/0006-pages-in-their-own-repository.md)).
`pages/` is its own git repo and is ignored by the project's, so the two histories never touch.

This was not the original arrangement, and the reason for changing it is worth recording. A commit
only ever stages paths under `PAGES_DIR`, so it could never *contain* source — but a **push sends
the whole branch**, and git offers no way to push part of one. While the two shared a repository,
publishing a page therefore also published every local code commit that had not been pushed yet.
Tested and confirmed before the split, and there is no setting that fixes it.

Gitignoring `pages/` without a second repository does not work either: `git add` refuses ignored
paths, so publishing fails outright with *"The following paths are ignored"* while saving carries
on. That would leave autosave with no history at all.

Nothing in the code changed for this. `vcs.Open` runs `rev-parse --show-toplevel` from `PAGES_DIR`,
so git walks up and finds the nearest `.git` — which is now the pages one. Pointing `PAGES_DIR`
somewhere else entirely still works the same way.

Two things follow. The notes can be **private while the code is public**, which they now are. And a
fresh clone of the project has no pages at all, which is why `examples/` holds a small invented set
that shows what the app does.

## Getting around

The app is called **Wiki for Verftet**. The name is the user's rather than the software's and lives
in exactly one place, `base.html`; the editor reads it off the brand link when it sets the document
title, so there is nothing to keep in step. `Marksheets` is what the program is called, and stays
the name of the module, the folder and these documents.

**The index is chrome, not a page** ([ADR-0019](adr/0019-the-front-page-is-a-page.md)). It is the
whole left-hand side of the window, from top to bottom and against the edge. It does not move when
the window grows — an edge is what it is for — and the room the text needs is the page's own
padding rather than a margin shared with the list.

In order down it: the name of the wiki, centred; `Publiser endringar`; the tags; a `+` that unfolds
the form for a new page; and the pages themselves. The order is what each is worth on arrival —
whether anything is waiting to be published is the one thing to know before you have decided where
you are going, and making a page is the rarest of the three, so its two fields are folded away
behind the `+` (a `<details>`, so the browser does the folding).

**The publish button is the accent when there is something to publish** and a quiet outline when
there is not, with the number of unsent pages in brackets. That is the same accent the dot beside a
page in the list uses, and it means the same thing in both places: work that exists here and
nowhere else.

It is drawn on the server inside `base.html` for every full page, so it is there on the first paint
rather than arriving after it: `render` fills a `Nav` field on the page's own data struct, and a
handler that forgot would lose its navigation rather than fail loudly.

**`/` opens the most recently edited page.** There is no screen that lists pages: with the index
beside every page, a page whose whole job was to list them was a stop on the way to a page and
nothing else. A folder with nothing openable in it gets a short empty page instead.

The header belongs to the page rather than to the window — it begins where the sidebar ends, and
the search box in it shares a left edge and a measure with the text below, so the two line up. The
`☰` that shuts the sidebar is lifted out of that row, against the window, for the same reason.

Clicking a tag in the sidebar swaps **only the sidebar's list**, so narrowing the index never takes
you off the page you were reading. That swap is HTMX asking for `/sidemeny`, which answers with two
fragments in one response: the tags the click was on, and — out of band — the pages they narrow.
Two, because they are not adjacent: the new-page form sits between them and may be open with
something typed in it, and filtering the index is no reason to throw that away.

Which way you left the sidebar is remembered per browser like the folded headings are — read in
`<head>`, before the first paint, so it is never drawn open and then shut in front of you. Below
62rem it stops being a column and becomes a band above the page, on the same toggle: an overlay
would need a second piece of state for the same one bit.

**Deleting a page is in the editor bar**, on the page it deletes, and only on pages that are not
working files — a working file belongs to its task and goes when the task does. It used to be a
button on each card of the index, and there is no index; on the page itself it is one screen
further from an accident and plainly about the page in front of you.

**`Ansvarleg` is deliberately not in the sidebar.** It is heading for a profile view of its own once
there is somebody to be logged in as; "who is this about" is not the question a page index answers.
Your own name is in the footer, and it leads to your page.

### Search

`Søk` in the topbar is two things sharing a box.

**As you type**, it offers page names — what you typed the start of first, then what merely contains
it, then pages carrying a matching tag. `↑`/`↓` walk the list, `Enter` takes the one you are on,
`Esc` closes it, and `⌘K` puts the caret in the box from anywhere, including from inside the editor
where every other key belongs to the document.

**On `Enter` with nothing picked**, it searches everything written *on* the pages: every line of
every page, matched case-insensitively, grouped by the page it was found on, with the trail of
headings above it and the match marked. A page matched by its own name or tag comes first, then the
most recently edited. Working files are searched too — a line on one is still something somebody
wrote — and each says which task it hangs off.

A line is searched as it reads. A data line is `budsjett 25000 kr`, so it matches on any part of
that (and numbers are stored as numbers, so the value has to be formatted before it can be matched
at all); a task carries the name of whoever it is for, so typing a name finds their tasks here as
well as on their page; a table is searched a row at a time, since its content is in cells rather than
fields. Five lines per page are shown and the rest are counted.

**There is no index** ([ADR-0018](adr/0018-search-is-a-scan.md)). The files are read on the request
that asks, the same bargain as backlinks and the unpublished set.

## Editor keys

| Key | Does |
|---|---|
| `Enter` | new line below, same level; splits the line at the caret |
| `Enter` at the start of a line | makes room above: the line moves down, a heading takes its section with it |
| `Enter` on an empty line | steps out one step: sub-line → line → text line → heading, one level out |
| `Tab` on a text line | it becomes a heading where it stands — what `#` does |
| `Tab` on a data or image line | move to the next field, then on to the next line |
| `Tab` in a table | next cell; off the right-hand edge it makes a new column |
| `Enter` in a table | next row; on a blank row, leave the table for a text line |
| `Backspace` in an empty column heading | remove that column, if nothing in it would go too |
| `Tab` / `⇧Tab` elsewhere | in / out one level, carrying any children along |
| `⇧Enter` | start a new heading beside the one you are in, after its contents |
| `⌥Enter` | soft line break inside the line |
| `Backspace` at the start of an empty line | delete it, caret to the line above; a heading gives its contents up a level |
| `#`, `-`, `1.`, `[]`, `= `, `\|` at line start | switch type |
| `⇧↑` / `⇧↓` | select whole lines, and extend the selection |
| `⌘C` / `⌘X` / `⌘V` | copy, cut and paste selected lines |
| `⌫` with lines selected | delete them |
| `↑` / `↓` | move to the line above or below, caret at the end, stepping over the pinned heading |
| `⌘Z` / `⇧⌘Z` | undo / redo |
| `@` + a letter or two | offers matching pages; `/` goes a level deeper; `Tab` completes |
| `⌘⏎` | switch between reading and editing |
| `⌘S` | outside the editor: publish everything changed |
| `⌘K` | jump to the search box, from anywhere |

The brief specified double-Enter to outdent and Shift+Enter for a new header. Both were changed:
double-Enter cannot fire without Enter firing first, it left no way to type a blank line, and it gave
no outdent for the ordinary case. Tab/Shift+Tab is what every outliner uses and what hands expect.

**`Enter` on an empty line does not leave one behind**
([ADR-0010](adr/0010-depth-belongs-to-headings.md)). It steps the line out of what it is, one step
per press, and never leaves a blank line lying there:

```
sub-sub-line → sub-line → line → text line → heading, one level out
```

The last step starts the next section. An empty line is somebody who has finished what they were
writing, and what follows that is nearly always a heading — so the blank line *is* the heading, and
they type its name rather than a `#` first.

**Losing the type is a step of its own, and comes first**
([ADR-0016](adr/0016-an-empty-line-loses-its-type-first.md)). An emptied list line becomes a plain
text line, and only the `Enter` after that makes it a heading. Going straight to a heading skipped
what people mean far more often — done with the list, carrying on in prose — and left no way back to
a plain line but to make the heading and unmake it. The step is taken only when the line is blank in
**every** field, so a data line with a value but no name keeps what a text line has nowhere to put;
a table with cells in it refuses and says so, the same guard the type menu uses.

A heading is exempt: it is already the type the ladder ends at, so it rises a level instead of
shedding a type it would take straight back. A `data` line no longer needs a rule of its own — the
ladder is what its exception asked for, applied to every type.

**`Enter` at the very start makes room above.** The line keeps what it holds and moves down, leaving
a line of its own kind where it stood — a list item above a list item, a table row above a table
row, and a text line above anything that does not continue. A heading takes its **whole section**
down with it: splitting a heading at offset zero used to empty it and
drop its own title into a text line *inside* it, which is nobody's intent. A split is only a split
when there is something to the left of the caret to keep.

The line is moved rather than copied, so it keeps its id — and with it the working file a task owns
and the link hints recorded on it. Emptying the line and putting its text into a fresh one below
looks identical on screen and quietly separates the text from everything attached to it.

This is what replaced conjuring a line with `↑` at the top of the page. That gesture went with the
pinned tasks heading: nothing can sit above it, and a rule that works on every line is worth more
than one that works only on the first.

A field holding nothing but whitespace counts as empty and is stored as empty. Emptying a
contenteditable with `Backspace` leaves a stray `<br>` behind, which reads back as `"\n"`; without
this every rule that asks whether a line is empty quietly answered no on the line that most obviously
was.

Arrow keys move by one press. A single-line field has nowhere for the browser to move the caret, so
it parks it at the edge of the field and the move costs a second press; the editor takes over
instead and lands at the end of the target line. In a field holding soft line breaks the browser
still handles movement until the caret reaches the field's first or last line. The pinned heading is
stepped over, holding no field for a caret to land in.

Indentation is validated as you go — the type menu greys out any type the current parent cannot hold.

**A bullet and a number are always drawn.** The type icon in the gutter is a control and fades in
with the row it belongs to, along with the twisty and the `+`. A list's bullet is not: it is the
line's own mark, and a list you can only see the bullets of while the pointer is over it does not
read as a list at all. `list` and `ordered` keep theirs at every level.

**A data line is a row, so `Tab` walks across it.** A line with more than one field and no
indentation of its own — `data`, `image` — has nothing else competing for the key, so `Tab` moves to
the next field and `⇧Tab` back, carrying on to the next line at either end the way a form does.
Moving the caret is not an edit, so it takes no undo step.

`Enter` on a data line opens another one, and a data line that is **blank in every field** becomes a
text line instead — a blank row means the table is finished, and what follows a table is prose far
more often than a new section. This used to be this type's own exception to the empty-line rule; it
is now simply the first rung of the ladder every type climbs, and a heading is still one more
`Enter` away.

An empty number field shows its placeholder rather than a `0` nobody typed; `coerce` turns it back
into `0` on the way to disk, so nothing downstream sees the difference — and "is this line blank"
becomes a question with an answer.

**A table is driven like a spreadsheet, not like an outline.** `Tab` walks the cells left to right,
headings first, and off the right-hand edge it *makes a column* — that is how a table grows, and the
only gesture that adds one. `Enter` opens the next row; on a row that is blank in every cell it drops
the row and leaves a text line after the table, the same rule a blank data line follows. `↑` and `↓`
move between rows before leaving the table, so it is neither a trap nor something the caret jumps
over in one press. `Backspace` in an empty column heading removes that column, and only while every
cell in it is empty too.

Cells are addressed as `col:2` and `cell:1:2` rather than by a field name, which is what lets
`here()`, `restore()` and the undo focus reach them like any other field — one caret mechanism, not
two.

Changing a table into another type, or deleting it with `Backspace`, is refused while it holds
anything: its content lives in cells, and no other type has anywhere to put them. The same bargain as
a task whose working file still holds work.

**Depth belongs to headings alone** ([ADR-0010](adr/0010-depth-belongs-to-headings.md)). A leaf —
text, data, image, or a list or todo that is not an item — has no depth of its own: it sits exactly
one level inside the heading above it. A leaf standing *beside* a heading is a shape nobody can type
but anybody can arrive at, by writing a line and then putting a heading above it, so the depths are
recomputed after every edit rather than policed during one. `reflow` runs on the way to the screen,
which is why the invalid shape cannot be drawn even once.

The pinned tasks heading encloses its tasks and nothing else. It is furniture rather than a section,
so the line after it is back at the top level — which is what lets a new page open on a text line
outside any heading.

**Tab never moves a line out of its heading.** What it does depends on the line:

- a **header** moves between outline levels, taking its contents with it;
- a **list or todo** becomes an item of the nearest line above that is not itself an item, provided
  that line is of the same type — an item inherits its parent's type, so a todo becoming an item of
  a list would lose its checkbox and owner;
- an **item** goes one level further in, inside the nearest sub-line above it at its own level —
  anything deeper in between already belongs to that sub-line, and this joins it. Two levels is as
  deep as it goes, and a sub-line that already holds sub-lines cannot become one;
- a **text line** has no indentation to gain, so the only way it can go in a level is to *become*
  the heading that makes one. `Tab` on it does exactly what typing `#` does, from wherever the
  caret already is.

`⇧Tab` on an item steps it out one level, and from the first level makes it a line again, directly
after the line it belonged to — taking its own sub-lines with it, which become its.

**Deleting a heading never deletes what it held.** `Backspace` on an emptied heading removes the
heading and moves its contents up a level, into whatever the heading itself belonged to. This is
`doc.Normalise`'s rule for orphans applied to the one gesture that makes them: content is never lost
to keep a tree tidy.

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

**A line is moved by its gutter.** The type icon doubles as a handle: press it and drag, and the line
goes where you drop it — with everything under it, so a heading carries its whole section. A press
that does not move is still a click, and still opens the type menu; movement is what tells the two
apart. Dragging a handle inside a block selection moves the whole selection.

This is done with plain pointer events rather than the browser's own drag-and-drop, for two reasons.
The rows are full of `contenteditable` fields, and native dragging inside one of those fights the
caret. And **dropping a file is** native drag-and-drop — keeping lines on pointer events means the
two gestures can never be mistaken for one another.

Where a moved heading lands decides its level: no deeper than one inside the heading above it, never
above the top, and the section keeps its shape because the whole block shifts together. Every other
line has no level of its own and is put right by `reflow`.

**The tasks section and the page do not mix.** A line dragged out of `Oppgåver`, or into it, stays
where it was; reordering *within* either side works normally. The tasks are the app's section, and a
heading dropped inside it would be a section inside the furniture.

**Several lines at once.** `⇧↑` and `⇧↓` select whole lines, and dragging the mouse across rows does
the same. Lines rather than characters: what anyone does with several lines at once is copy, move or
delete them, and none of those wants half a line. Every selected line is drawn in the same colour,
including the one the caret is in — a selection is one thing, and a line that looked different for
holding the caret would read as a different thing.

The mouse route is tracked by hand rather than read off the browser's selection. **Each field is its
own `contenteditable`, which makes it its own editing host, and a native selection cannot leave one
and enter the next** — however far you drag, the browser reports a selection inside a single row. So
the drag is followed directly: the row it began in, the row under the pointer now, and the span
between. The native selection inside the starting row is left alone rather than cleared, because
clearing it would take the caret with it and the caret is what says which lines are meant; the CSS
hides its paint instead.

**Copying writes text, and keeps the structure beside it.** The clipboard carries a markdown-ish
rendering — `#` for a heading, `- ` for a list line, `1. ` for a numbered one, indented by depth —
because that is what another program can read. The rows themselves are held in the page. A paste
whose text is exactly what was written is the same block coming home and comes back whole, with its
types, depths and table contents; anything else is text from somewhere else and is read through the
same line-start prefixes. A single line with no newline in it is just text and goes in at the caret.

Pasted lines are new lines: ids are not carried across, because two lines sharing an id would make
every `@`-query pointing at one of them ambiguous.

Cutting and deleting a block are refused while it holds a task whose working file has content, or a
table with anything in it, or the pinned heading — the same guards as removing those one at a time.

**`⌘1`–`⌘6` are gone.** macOS and most browsers claim them for switching tabs and desktops, so the
binding worked in some windows and not others, which is worse than not existing. The line-start
shortcuts and the gutter menu are how a type changes.

**`⌘⏎` switches between reading and editing**, from either side. The handler is on the document,
because in reading mode there is no field to hold the key, and it listens in the **capture** phase so
that the `Enter` handlers on the rows, the title and the tag field never see it — bubbling would have
let one press both open a line and change mode.

`⇧→` was tried first and given up: it is the browser's own extend-selection-by-one-character, and
taking it left `⇧←` still selecting backwards while `⇧→` did not. `⌘⏎` costs nothing that macOS or
the browser already uses.

**Indent guides start one level in.** A row nested under a sub-heading gets a hairline down its left
edge; a row at the first level under a top-level heading does not. Everything on a page is inside
*something*, so a rule beside all of it distinguished nothing — where the guides now appear they
mean "this sits under a sub-heading".

**Headings fold.** A row with children gets a twisty; folded rows show how many lines are hidden.
Folding is a view preference, kept per page in `localStorage` and never written into the document.
Arrow keys skip what is folded away. On a page you have never opened, `Arkiv` headings start folded
— finished work should be out of the way by default — but the moment you fold or unfold anything
there, your own choice is stored and takes over.

## Implementation notes

Go + HTMX, following `../mystuff/STACK.md`, on port 3003. No auth: single user. Pages are JSON
files (see **Storage** above), so the SQLite dependency from `STACK.md` is gone — `go.mod` lists no
requirements at all.

`go.mod` lists no requirements at all ([ADR-0002](adr/0002-no-dependencies.md)).

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

Table cells hold inline markdown but not `@`-queries, and are not reached by a `[#tag]` filter.
Both come from the same place: links are recorded per *field* (`Store.recordLinks`), and a cell is
not a field. A query in a cell would resolve by path, break silently on a rename and never show up in
a backlink — half a feature, so none. Addressing a column or a single cell (`@prisar/leverandorar/pris`)
is the natural next step and is not built either.

Nothing reaches outside the folder. Fetching from an API — a page showing a building's power use,
say — is designed but unbuilt: a **source page** whose content the app writes, refreshed on a ticker,
projecting the response into ordinary nodes so that `@` needs nothing new
([ADR-0012](adr/0012-fetched-data-lives-on-a-source-page.md)). A cron job writing a page file does
most of it today with no code, and that is the thing to try first.

Nothing sweeps up attachments. Deleting a file line, or a page, leaves the file in `filer/` — losing
a file to a deleted line would be the worse mistake, so the orphan stays and a tidy-up is a job for
later. Only PDFs preview; anything else is a box with a link in it.

No `@`-query takes a tag as its subject. `@side/bolk[#emne]` filters *within* a page, but there is
no way to ask for every page carrying `#hytta` — the front page filters on exactly that, and the
query language cannot yet say it.

**No merge when two people save the same page.** The second save is refused rather than merged
([ADR-0021](adr/0021-a-save-answers-for-what-it-read.md)). The data model makes the merge unusually
tractable — permanent node ids and named fields mean it is set arithmetic per node, not text
diffing — and it is still not built, on purpose: the refusals are what will say whether it is worth
it. Nothing else in the app should grow a second way of resolving this in the meantime.

Also absent: renaming a *page* — only headings propagate today, since a page rename is a file
rename and needs its own handling.

`Normalise` **lifts orphans rather than dropping them**: a node whose type cannot nest hands its
children up to its own level instead of losing them. This exists because it once did drop them — an
outdent bug produced a tree with todos under a text line, and the save silently deleted five lines.
Malformed input should be repaired, never quietly discarded.

Undo history lives only in the page: it does not survive a reload, and it is not written to the
file. Git history is the durable record, per save; `⌘Z` is the working one, per edit.
