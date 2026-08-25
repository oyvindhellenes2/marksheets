# Marksheets — working notes

A hybrid of a spreadsheet and a markdown editor. Pages are JSON files; there is no database.
Go + HTMX, **zero dependencies**, port 3003.

**[`SPEC.md`](SPEC.md) is the authority on the design** — the data model, the query language, the
editor rules, and *why* each is the way it is. Read it before changing behaviour.
[`Marksheets.md`](Marksheets.md) is Øyvind's original brief, kept for the record; several of its
decisions were deliberately overturned, and where the two disagree **SPEC.md is what was built**.

## Running and checking work

```sh
/opt/homebrew/bin/go run ./cmd/marksheets     # http://localhost:3003
```

- **`go` is not on `PATH` in non-interactive shells.** Use the absolute path above, or builds
  will fail with "command not found" for no obvious reason.
- **Templates, CSS and JS are embedded with `embed.FS`.** Editing them changes nothing until you
  **restart the server**. This has caused more than one phantom "the fix didn't work".
- **There is no test suite.** Verification is by running the app: `curl` for the server side,
  a browser for the editor. `internal/render/query.go` is the piece most worth unit-testing if
  you ever add tests — path resolution, filters and the cycle guard are pure functions.
- **Running the app writes commits.** `pages/` is inside this repo, so every save in the UI
  commits `pages/*.json` here. Expect the working tree to move while you test, and clean up
  test edits before finishing.

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
| `internal/vcs/` | git, shelled out (keeps `go.mod` at zero dependencies) |
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
attempted; a failed commit is reported but never becomes a failed save. Only paths under
`PAGES_DIR` are staged — the app must not be able to commit its own source.

## Conventions

- UI text is **Norwegian (nynorsk)**. Code, comments and these docs are English.
- Follows `../mystuff/STACK.md` (Go stdlib HTTP, `embed.FS`, HTMX, vanilla CSS) with one
  deliberate departure: **the page editor is vanilla JS, not HTMX**, because a keyboard-driven
  outliner cannot round-trip per keystroke. Don't "fix" that.
- Published at <https://github.com/oyvindhellenes2/marksheets> (public).

## Not built yet

Formulas and aggregates (`=sum(@gym.*.budsjett)`) — the dependency graph would hook into the
existing cycle guard in `render.ctx`. Also: search, drag-to-reorder, restoring a page from a
history entry, and renaming a *page* (only headings propagate; a page rename is a file rename and
needs its own handling). `SPEC.md` has the full list.
