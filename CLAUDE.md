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
  (`⌘S`) commits **and pushes**, so never press it from a test run unless you mean to publish.
- **To exercise publishing, use a throwaway clone**, not this checkout:
  `git clone --bare . /tmp/x/origin.git && git clone /tmp/x/origin.git /tmp/x/work`, then run with
  `PORT=3014 PAGES_DIR=/tmp/x/work/pages`. Push then goes to a local bare repo and GitHub never
  sees it. The no-remote and push-failure paths are reachable there too, with `git remote remove`
  and `git remote set-url origin /nonexistent`.

When driving the editor in a browser: **setting focus from injected JavaScript is not real
browser focus**, and synthesised key events will not reach the page handlers. Several "bugs" that
were only test-harness artefacts came from this. Click for real, then send keys.

## Layout

| Path | What lives there |
|---|---|
| `cmd/marksheets/main.go` | wiring: types, store, git, server |
| `cmd/marksheets/static/editor.js` | the whole editor — the biggest and trickiest file |
| `internal/doc/` | node/document model, `types.json` registry, JSON shape, `Normalise` |
| `internal/render/` | read-view HTML, `@`-query parsing and resolution, link helpers |
| `internal/pages/` | the file store, task pages, backlinks, rename propagation |
| `internal/vcs/` | git, shelled out (keeps `go.mod` at zero dependencies): commit, push, what is unpublished, restore |
| `pages/` | the data — one JSON file per page |

## Invariants worth protecting

These were each learned the hard way. Breaking one is how content gets lost.

**Never discard content to make a tree valid.** `doc.Normalise` *lifts* orphans to the level above
rather than deleting them. It used to delete them, and an outdent bug silently destroyed five lines
of real content. Malformed input gets repaired, never dropped.

**Machine-owned keys must survive a round trip.** The editor sends only `title` and `children`, so:

- node-level `links` and `page` have to be carried through `flatten` → `nest` *and* undo snapshots.
  Dropping `page` once made the server open a duplicate task page on every save.
- doc-level `parent` is the store's, set in `Store.Save` from what is on disk and ignored if a
  request supplies it. Taking it from the request wiped it on every save and dumped all twelve
  working files onto the front page.

**Only headers nest.** A list's or todo's sub-lines are `items` *inside* the line, not children
beside it. This is structural, not a rule to police — there is no `children` field on a leaf to
fill in wrongly.

**The editor works on a flat list of rows with a depth**, and rebuilds the tree only on save.
Indent, outdent and block moves are then slices and arithmetic. Structural edits re-render the
rows, which is why undo is implemented over the model rather than left to the browser.

**Git is the historian, never the database.** The file is written and safe before a commit is
attempted, and the commit is made and safe before a push is attempted; each failure is reported
and never becomes a failure of the step below it. Only paths under `PAGES_DIR` are staged — the
app must not be able to commit its own source.

**A commit can be limited to the pages; a push cannot.** `git push` sends the whole branch, so
while code and pages shared a repository, publishing a page also published every unpushed code
commit. That is why `pages/` is a repository of its own — not a preference, a limit of git. Do not
merge them back.

**Going back to an old version moves forward.** `POST /p/{slug}/gjenopprett/{hash}` reads the file
out of git with `show` and writes it back through the store — never `checkout`, which would stage
the change and could leak into a hand-run commit. The restored content is an ordinary unpublished
edit; history is never rewritten and no commit is ever removed.

**Saving and publishing are separate acts.** `PUT /p/{slug}` writes the file and nothing else;
`POST /p/{slug}/publiser` commits and pushes. Do not put a commit back in the save path — the whole
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
