# Marksheets — working notes

A hybrid of a spreadsheet and a markdown editor. Pages are JSON files; there is no database.
Go + HTMX, **zero dependencies**, port 3003.

**[`SPEC.md`](SPEC.md) is the authority on the design** — the data model, the query language, the
editor rules, and *why* each is the way it is. Read it before changing behaviour.
[`adr/`](adr/) holds the decisions themselves: what the alternatives were and what was true at the
time. SPEC says what the design is; an ADR says why it is not something else. Records there are
written once and superseded, never edited.
[`Marksheets.md`](Marksheets.md) is Øyvind's original brief, kept for the record; several of its
decisions were deliberately overturned, and where the two disagree **SPEC.md is what was built**.

## Running and checking work

```sh
/opt/homebrew/bin/go run ./cmd/marksheets     # http://localhost:3003
```

- **Turn the hooks on once per clone:** `git config core.hooksPath .githooks`. They are not
  installed by cloning, because git does not version `.git/hooks`. The pre-commit hook regenerates
  `adr/README.md` and warns — never blocks — when a commit changes code and no prose.
- **`go` is not on `PATH` in non-interactive shells.** Use the absolute path above, or builds
  will fail with "command not found" for no obvious reason.
- **Templates, CSS and JS are embedded with `embed.FS`.** Editing them changes nothing until you
  **restart the server**. This has caused more than one phantom "the fix didn't work".
- **There is no test suite.** Verification is by running the app: `curl` for the server side,
  a browser for the editor. `internal/render/query.go` is the piece most worth unit-testing if
  you ever add tests — path resolution, filters and the cycle guard are pure functions.
- **`pages/` is its own git repository**, ignored by this one. Page edits never show in this repo's
  `git status`, and publishing a page cannot touch the code history. `examples/` holds an invented
  page set for running the app without your own notes: `PAGES_DIR=examples`.
- **Running the app rewrites page files; publishing commits to the pages repo and pushes it.**
  Saving is automatic, so typing in the UI rewrites `pages/*.json` about a second later. `Publiser`
  (`⌘S`) is on the **home page**, commits every changed page and **pushes**, so never press it from
  a test run unless you mean to publish. Uploading a file writes into `PAGES_DIR/filer/` the same
  way — which on a run with `PAGES_DIR=examples` means writing into this repository.
- **To exercise publishing, use a throwaway clone**, not this checkout:
  `git clone --bare . /tmp/x/origin.git && git clone /tmp/x/origin.git /tmp/x/work`, then run with
  `PORT=3014 PAGES_DIR=/tmp/x/work/pages`. Push then goes to a local bare repo and GitHub never
  sees it. The no-remote and push-failure paths are reachable there too, with `git remote remove`
  and `git remote set-url origin /nonexistent`.

When driving the editor in a browser: **setting focus from injected JavaScript is not real
browser focus**, and synthesised key events will not reach the page handlers. Several "bugs" that
were only test-harness artefacts came from this. Click for real, then send keys. Note also that some
automation harnesses do not deliver modifier+arrow (`⇧↑`) as the page sees it — dispatch a real
`KeyboardEvent` from the page to tell a broken binding from a broken harness apart.

**Every field is its own editing host.** Each is a separate `contenteditable`, so a native selection
can never span two rows: drag as far as you like and `window.getSelection()` still reports a range
inside one field. Anything that wants to know about several lines at once has to track the pointer
itself — see the drag handling beside `paintSelection`. This is not a bug to fix; it is what the
platform does with sibling editing hosts.

## Layout

| Path | What lives there |
|---|---|
| `cmd/marksheets/main.go` | wiring: types, store, git, server |
| `cmd/marksheets/static/editor.js` | the whole editor — the biggest and trickiest file |
| `cmd/marksheets/static/chrome.js` | everything outside the document: sidebar toggle, search box, tag clamp, publishing |
| `internal/doc/` | node/document model, `types.json` registry, JSON shape, `Normalise` |
| `internal/render/` | read-view HTML, `@`-query parsing and resolution, link helpers |
| `internal/pages/` | the file store, task pages, backlinks, rename propagation, attachments on disk, who is down for what (`owners.go`), search (`search.go`) |
| `internal/auth/` | OIDC against Pocket ID, sessions, the middleware — and the local-user mode that runs without any of it |
| `internal/users/` | who has signed in, in a file beside the pages |
| `internal/files/` | how an attachment is named and what it may be served as — below both the store and the renderer, because the store imports the renderer |
| `internal/vcs/` | git, shelled out (keeps `go.mod` at zero dependencies): commit, push, what is unpublished, restore |
| `pages/` | the data — one JSON file per page |

## Invariants worth protecting

These were each learned the hard way. Breaking one is how content gets lost.

**Never discard content to make a tree valid.** `doc.Normalise` *lifts* orphans to the level above
rather than deleting them. It used to delete them, and an outdent bug silently destroyed five lines
of real content. Malformed input gets repaired, never dropped.

**Machine-owned keys must survive a round trip.** The editor sends only `title`, `tags` and
`children`, so:

- node-level `links` and `page` have to be carried through `flatten` → `nest` *and* undo snapshots.
  Dropping `page` once made the server open a duplicate task page on every save.
- doc-level `parent` is the store's, set in `Store.Save` from what is on disk and ignored if a
  request supplies it. Taking it from the request wiped it on every save and dumped all twelve
  working files onto the front page.
- doc-level `tags` *is* the editor's — unlike `parent` — so it has to travel in the save body, the
  `localStorage` draft and the undo snapshot alike. Leaving it out of any one of them silently
  reverts the tags on the next save from that path.
- node-level `columns` and `rows` are a table's whole content, and go the same way: `flatten` →
  `nest`, plus the undo snapshot, plus a deep copy in both — a shallow one shares the cell arrays
  with the live rows and makes undo do nothing
  ([ADR-0011](adr/0011-a-table-is-its-own-type.md)).

**Only headers nest.** A list's or todo's sub-lines are `items` *inside* the line, not children
beside it. This is structural, not a rule to police — there is no `children` field on a leaf to
fill in wrongly.

**Sub-lines go two deep and no further** ([ADR-0015](adr/0015-sub-lines-go-two-deep.md)). The limit
is written in two places that must agree — `doc.MaxItemDepth` on the server and `MAX_ITEM` in
`editor.js` — and both must keep *lifting* what arrives deeper rather than dropping it. A row's
`item` is the level, `0 | 1 | 2`, not a flag: it still reads as a boolean everywhere it is tested,
so the old `!!r.item` spellings are gone rather than harmless. `MAX_ITEM` is declared above the
module-level `flatten()` for the same reason `RESERVED` is — below it, the first flatten of the
document throws before a single row is drawn.

**Depth belongs to headings alone, and is recomputed rather than maintained.** `reflow` runs at the
top of `render`, putting every leaf exactly one level inside the heading above it
([ADR-0010](adr/0010-depth-belongs-to-headings.md)). Do not add depth-fixing to individual commands
— that is the design this replaced, and it had a hole in it for months: write a line, put a heading
above it, and the line was a sibling of the heading. A command changes what it means to change and
lets `reflow` settle the rest.

**An emptied contenteditable is not empty, and both halves of that have bitten.** Deleting the last
character leaves a stray `<br>` behind, so:

- it reads back as `"\n"` — the input handler stores `''` for anything that trims to nothing;
- and the caret lands *on the field* rather than in a text node, with `focusOffset` a child index.
  `caret()` has a branch for that. Without it the tree walker never meets the node it is looking for,
  runs to the end, and reports the length of the field as the caret position — so `Backspace` on a
  line you had just emptied read as "not at the start" and did nothing until you reloaded.

Every "is this line empty, and is the caret at its start" test depends on both. `Enter`, `Backspace`
and the empty-line rules all quietly stop working if either goes.

**The tasks heading is the app's, not the user's.** `Oppgåver` is pinned to the front of the
document by `doc.Normalise`, drawn as a label with no field in it, and the whole section — tasks
included — is left out of the read view
([ADR-0008](adr/0008-the-tasks-heading-is-furniture.md)). Three things follow that are easy to undo
by accident: it is matched by its *slugged label*, so nothing may let it be renamed; nothing may sit
above it, so `bodyStart` — not index 0 — is where the page proper begins, and anything inserting at
"the top" means there; and `Normalise` repairs a missing heading with `doc.TasksBlock`, never
with `doc.Template` — the latter carries a body line, and adding one to an existing page while
fixing its heading would be a silent edit of somebody's notes.

**Non-ASCII slugs must be escaped before they go in a header.** HTTP headers are Latin-1, so
`HX-Redirect: /p/blåboksen` reaches the browser as `blÃ¥boksen` and 404s. `handleCreate` uses
`url.PathEscape`. `http.Redirect` already escapes `Location` itself; anything else writing a slug
into a header does not.

**A table's content is in cells, not fields.** So every "is this line empty" check has to ask
`tableHasContent`, and both deleting a table and changing its type are refused while it holds
anything — no other type has anywhere to put cells, so the alternative is silent loss on the next
save. Cells are positional against `columns`, and both `doc.Normalise` and the editor's `reflow`
keep a table rectangular; do not assume a row is as wide as the columns without going through one of
them.

**A name on a task is a `user`-kind field, and three places agree on that**: `render.matches`
(`[@namn]` in queries), `pages.eachAssigned` (whose page it lands on) and the editor's picker. Do not
narrow any of them to the field *called* `owner` — the kind is what makes a new field of that kind
work everywhere at once ([ADR-0020](adr/0020-a-person-is-not-a-tag.md)). And do not merge `user`
back into `tag`: a subject and a person are different questions, which is why one is written with a
`#` and the other is not.

**Nothing about the pages reaches a signed-out request.** `Server.nav` returns an empty `navData`
when there is no user, so the sidebar, the search and the footer are not merely hidden — they have
nothing to draw. The sign-in screen is the one page an anonymous request may render. Keep it that
way: the cheap version of this leaks page titles to whoever knocks.

**Authentication is optional configuration, and failing open is not.** With no `AUTH_ISSUER` the app
runs as one local user and every screen works — that is what makes it testable without an identity
provider. With an issuer set there is no local user at all: the middleware refuses, and a bug that
makes it fall back to one is the worst bug this app could have. The list of people lives *outside*
`PAGES_DIR` (`USERS_PATH`) — inside, it would be pushed to a public remote and counted as a page.

**The version check has to come before the side effects.** `Store.Save` refuses a stale save before
writing, before `syncTasks` creates or deletes working files, and before links are rewritten; the
check and the write are under one lock ([ADR-0021](adr/0021-a-save-answers-for-what-it-read.md)).
Moving any of that around reintroduces exactly what it was built to stop. A save with no version is
unchecked on purpose — that is the restore path, which is not answering for anything it read.

**Every full page render needs `Nav`.** `base.html` draws the sidebar, so it reads `.Nav` off
whatever struct it is given. `render` fills that in through the `navHolder` interface, which means
a struct passed **by value** silently gets an empty sidebar — the type assertion fails and nothing
says so. Page data goes to `render` as a pointer; keep it that way, and give any new page struct a
`Nav navData` field and its one-line `setNav`.

**There is no index page.** `/` redirects to the most recently edited page, and the list of pages
is the sidebar ([ADR-0019](adr/0019-the-front-page-is-a-page.md)). Two controls that used to live on
that page now live in the sidebar — `Publiser` and the offer to `git init` — and `Slett` lives in
the editor bar of the page it deletes. If you find yourself wanting a screen that lists pages, note
that this is the thing that was removed on purpose, and say what the screen is *for* first.

**`⌘S` must stay unbound while the editor is on screen.** `chrome.js` checks for `.editor-shell`
before binding it. In the editor the key means "save what I typed", which already happened on a
timer; publishing pushes to everybody.

**The site's name lives in `base.html` and nowhere else.** `Wiki for Verftet` is the user's name for
their wiki; `Marksheets` is the program. The editor reads the name off the `.brand` link rather than
carrying a copy, so renaming the site is one edit.

**Nothing here has an index, and that is the design** ([ADR-0018](adr/0018-search-is-a-scan.md)).
Backlinks, the unpublished set, the people index and search all read the folder on the request that
asks. Before adding a cache to any of them, note that the files change without going through this
app at all — a text editor, a `git pull`, a restore from history — so an index has to be honest
about all three or it will be fast and quietly wrong.

**A page always has a tag.** The editor refuses to remove the last one and `Doc.EnsureTags` fills in
the page's slug for a file that arrives with none
([ADR-0009](adr/0009-every-page-has-a-hashtag.md)). Both halves are load-bearing: the first is where
the rule is felt, the second is what keeps a hand-written file from being rejected.

**The editor works on a flat list of rows with a depth**, and rebuilds the tree only on save.
Indent, outdent and block moves are then slices and arithmetic. Structural edits re-render the
rows, which is why undo is implemented over the model rather than left to the browser.

**Git is the historian, never the database.** The file is written and safe before a commit is
attempted, and the commit is made and safe before a push is attempted; each failure is reported
and never becomes a failure of the step below it. Only paths under `PAGES_DIR` are staged — the
app must not be able to commit its own source. That used to be enforced with `filepath.Base`, which
also flattened any subdirectory; it is now `Repo.staged`, which joins and then checks containment,
because attachments live in `filer/` *inside* the page folder. Do not go back to `Base` — and do not
drop the containment check either.

**An attachment is only ever addressed by a name that round-trips.** `files.StoredName(name) == name`
is the whole guard, in `Store.filePath`, and `FilesOn` runs every reference through it before a
publish can stage one. A page file is hand-editable, so a `file` node saying `../../.git/config` is a
thing that can exist; it is ignored rather than obeyed.

**A commit can be limited to the pages; a push cannot.** `git push` sends the whole branch, so
while code and pages shared a repository, publishing a page also published every unpushed code
commit. That is why `pages/` is a repository of its own — not a preference, a limit of git. Do not
merge them back.

**Going back to an old version moves forward.** `POST /p/{slug}/gjenopprett/{hash}` reads the file
out of git with `show` and writes it back through the store — never `checkout`, which would stage
the change and could leak into a hand-run commit. The restored content is an ordinary unpublished
edit; history is never rewritten and no commit is ever removed.

**Saving and publishing are separate acts.** `PUT /p/{slug}` writes the file and nothing else;
`POST /publiser` — on the home page, not the editor — commits every changed page and pushes once
([ADR-0014](adr/0014-publishing-lives-on-the-home-page.md)). A commit can be scoped to a page; a push
cannot, so do not put a publish button back on a page. Do not put a commit back in the save path — the whole
point is that durability happens constantly and history happens when asked. "Unpublished" is
computed against `origin/<branch>` on every request and never stored, so it covers both
edited-but-not-committed and committed-but-not-pushed.

## Keeping the prose true

The docs do not update themselves, and no hook can write them: a decision is something a person
made, and the reasoning that matters is mostly not in the diff — [ADR-0005](adr/0005-no-newline-type.md)
records a decision where nothing was built at all. What *is* automated is the index and a reminder.

So, when finishing a change:

- **Behaviour changed** → update `SPEC.md`. It is the authority, and it is present-tense.
- **A decision was made you would not want to argue twice**, or one was reversed → write an
  `adr/` record. Reversals supersede; they do not edit the old record.
- **An invariant or a way of working changed** → update this file.
- Nothing worth saying → say nothing. An `adr/` full of changelog entries is worse than an empty one.

## Conventions

- UI text is **Norwegian (nynorsk)**. Code, comments and these docs are English.
- Follows `../mystuff/STACK.md` (Go stdlib HTTP, `embed.FS`, HTMX, vanilla CSS) with one
  deliberate departure: **the page editor is vanilla JS, not HTMX**, because a keyboard-driven
  outliner cannot round-trip per keystroke. Don't "fix" that.
- Published at <https://github.com/oyvindhellenes2/marksheets> (public).

## Not built yet

Formulas and aggregates (`=sum(@gym.*.budsjett)`) — the dependency graph would hook into the
existing cycle guard in `render.ctx`. Also: search, drag-to-reorder, and renaming a *page* (only
headings propagate; a page rename is a file rename and needs its own handling). `SPEC.md` has the
full list.
