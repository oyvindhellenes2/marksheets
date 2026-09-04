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
- **Reference a static file with `{{asset "/static/x"}}`, never as a bare path.** The helper hangs
  a hash of everything under `static/` off the URL. The deployment at `wiki.verftet.info` sits
  behind Cloudflare, which caches CSS and JS at its edge for four hours and tells the browser to
  hold them for as long — so an unstamped asset means a deploy that visibly does nothing, which is
  the same phantom as the paragraph above and much harder to spot from the server.
- **There is no test suite.** Verification is by running the app: `curl` for the server side,
  a browser for the editor. `internal/render/query.go` is the piece most worth unit-testing if
  you ever add tests — path resolution, filters and the cycle guard are pure functions.
- **The page folder is snapshotted daily** by `deploy/backup-pages.sh`, run from a systemd timer at
  04:30 into `/opt/backup/pages/`, fourteen kept, hard-linked so unchanged files cost nothing. It
  copies the folder *whole* — working tree, `.git`, attachments, untracked files — because git and
  GitHub already hold everything **published**, and the gap is exactly what has not been. It is on
  the same disk, so it answers "I deleted the wrong thing" and nothing about a dead disk.
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
| `cmd/marksheets/static/present.js` | the presentation: slides cut out of the read view, on a page and on a share link alike |
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

**A page must keep a line below the tasks section**, or it cannot be typed into: the page proper
begins at `bodyStart`, and a document that is only its tasks has nowhere to put a caret. Guards that
count *all* rows — `rows.length === 1`, `!rows.length` — do not catch this, because the pinned
heading and its tasks are rows too and `blockCanGo` will not let them go. Every such check has to ask
about rows at or below `bodyStart`. `doc.ensureBody` repairs it on load as well, since a page file is
hand-editable and reachable by restore.

**Machine-owned keys must survive a round trip.** The editor sends only `title`, `tags` and
`children`, so:

- **`RESERVED` in `editor.js` is the list of node-level keys**, and a machine-owned key missing from
  it is silently demoted to a *user field*: `fieldsOf` copies every unreserved key into `fields`, so
  it shows up in the editor and is written back as though somebody had typed it. Adding `num`
  without adding it here leaked the task number into every task's fields — caught by running the
  real `flatten`/`nest` in goja, not by reading them.
- node-level `links`, `page` and `num` have to be carried through `flatten` → `nest` *and* undo
  snapshots.
  Dropping `page` used to make the server open a duplicate task page on every save; since
  [ADR-0025](adr/0025-a-task-page-is-made-on-the-way-to-it.md) a save creates nothing, so the same
  bug now shows up quieter — the arrow beside a task stops knowing it has a page and offers to make
  a second one.
- doc-level `parent` is the store's, set in `Store.Save` from what is on disk and ignored if a
  request supplies it. Taking it from the request wiped it on every save and dumped all twelve
  working files onto the front page.
- **node-level `num` is the store's too, and "absent" never means "new".** A task typed since the
  page loaded has no number in the editor, so it arrives with none in every save until somebody
  reloads. `numberTasks` therefore looks a missing number up in `prev` **by node id** before it
  spends a fresh one. Reading absent as unnumbered gave the same task a new number on every
  autosave — about one a second while typing — so a task written over two minutes climbed a hundred,
  and the next task on that page started from there. If you ever make `numberTasks` stop taking
  `prev`, this comes straight back, silently, and the numbers are the one thing that must stay put.
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

**Every git command runs with `core.quotepath=false`, and that is load-bearing.** It is set once in
`vcs.runFor`, so nothing has to remember it per call. With git's default, a path holding any byte
above ASCII comes back C-quoted — `blåboksen.json` prints as `"bl\303\245boksen.json"` — and this is
a Norwegian wiki where every page name is a slug of something somebody typed. It cost a page:
`Unpublished` reads names out of `diff --name-only` and `ls-files --others`, a quoted name does not
end in `.json`, and the caller filtering for pages dropped it without a word. No dot in the sidebar,
never committed by a publish, live on the site and absent from the repository for hours. What made
it look arbitrary is that non-ASCII *task* pages published fine — they are dragged in by their
parent through `publishSet`, which builds names from the store rather than from git. Never parse a
path out of git output on the assumption it is the name on disk.

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

**Nothing about the pages reaches a signed-out request, except one page at a time through a share
link.** `Server.nav` returns an empty `navData` when there is no user, so the sidebar, the search
and the footer are not merely hidden — they have nothing to draw. The cheap version of this leaks
page titles to whoever knocks.

**What a stranger may read is `Server.publicRequest` and nothing else**
([ADR-0024](adr/0024-a-share-link-is-the-credential.md)). `auth.Middleware` asks that one hook, so
there is one place to look and one place to change. Do not add a path to the middleware's own
prefix list to make something public — that is the same decision made where it cannot be justified,
and it will drift from the function that was supposed to hold it. A shared page's outward links are
killed in `render.Shared`, on the server: "disabled in the browser" is not disabled for a reader
with no script, and the client-side version of this was written first and deleted for that reason.

**Authentication is optional configuration, and failing open is not.** With no `AUTH_ISSUER` the app
runs as one local user and every screen works — that is what makes it testable without an identity
provider. With an issuer set there is no local user at all: the middleware refuses, and a bug that
makes it fall back to one is the worst bug this app could have. The list of people lives *outside*
`PAGES_DIR` (`USERS_PATH`) — inside, it would be pushed to a public remote and counted as a page.
So does the session store (`SESSIONS_PATH`), for the same reason and one more: it says who is signed
in until when.

**Sessions are kept by the hash of their token, never the token**
([ADR-0023](adr/0023-sessions-outlive-the-process.md)). They are written to disk now, so a deploy is
no longer a login for everybody — which also means nothing clears them behind your back: signing out
and expiry are the only two ways a session ends, and both have to keep working. `persist` runs on
sign-in *and* sign-out; drop the second and a restart hands a signed-out session back to whoever
still holds the cookie. Never write the raw token to the file, and never make a load failure fatal —
the fallback is everybody signing in again, which is survivable, and a wiki that will not boot
because a login cache is corrupt is not.

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
carrying a copy, so renaming the site is one edit. `.brand` is in the header now, not at the top of
the sidebar — if you move it again, check what reads it.

**`--header-h` and the height of `.topbar-inner` are a pair.** The header is `position: fixed`; the
sidebars start at `--header-h` and `.page` is padded by it. Nothing measures the header at runtime,
which is what keeps the layout right with no JavaScript. Change one of the two and the sidebars sit
over the header or float below it. There is no test that catches this — it is a number agreeing with
a number. The narrow media query must **not** set `position` on `.topbar` again: it comes later in
the file, so a `sticky` there overrides the `fixed` above, puts the header back in the flow, and the
page's padding then pushes everything down by a second header's worth.

**A flex row that pins children to opposite edges must use `auto` margins, not `space-between`.**
The rail holds two toggles and shows one at a time; with `space-between` the single visible child
went to the *start* of the row, which put the right-hand button at the far left, underneath the open
sidebar. It looks correct in every screenshot where both are showing.

**A flex item that may grow ignores the width you give it.** `alignSearch` sets `flex: none`
alongside the measured width, because `.search` is `flex: 1` in the stylesheet and would otherwise
fill the row regardless — which reads as a measurement that came out wrong rather than as a
declaration that was overruled.

**goja's parser catches typos, not names.** This has now cost two bugs: a rename left the swipe
handler reading `toggle`, and a block appended to the wrong IIFE lost sight of `narrow`, which meant
the search alignment threw on every load and never worked at all. Both are ordinary free variables —
legal syntax, `ReferenceError` on execution.

So **run the file, do not just parse it**: goja against a stub browser (a permissive `Proxy` for
`document`/`window`/…) and fail on `ReferenceError` alone, since every other error is the stub
meeting real logic. That catches anything evaluated at load, which is where the second bug was. It
does **not** catch the first: `toggle` was read inside a `touchstart` handler no stub will fire.
Handlers still have to be read by a person, and after a rename, grep for the old identifier.

**The presentation finds its article by `.read`, on two screens that disagree about the id.**
`present.js` does `document.querySelector('.read')`: on the share view `#read-view` *is* the
article, and on a page it is the box HTMX swaps the article into. The class is the contract, not the
id — rename it in `read-partial.html` or `del.html` and `Vis` silently opens nothing. It is also why
`editor.js` sets `window.marksheetsPresent.prepare`: in the editor the article is fetched rather
than sent with the page, so the button has to wait for it, and `setMode` therefore returns a promise
that settles once the read view is on screen. Anything that stops it returning one stops `Vis`
working while leaving `Les` looking fine.

**Appending to `chrome.js` means picking an IIFE.** It is four of them — sidebar/search, tags,
publishing, sharing — and a regex that matches "the last `})();`" lands in the sharing one, whose
scope has none of the sidebar's names.

**Data that the running code maintains has to be tidied *after* the code ships, not before.**
Cleaning the eighteen empty task pages against the old binary deleted them and cleared each task's
`page` field, and the running server — still creating eagerly — made every one of them again on the
next save of the parent page. Deploy first, then clean.

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
