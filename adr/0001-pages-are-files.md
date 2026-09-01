# ADR-0001: A page is a JSON file, not a row in a database

- **Status:** Accepted
- **Date:** 2026-08-24
- **Superseded by:** —

## Context

A page is a tree of typed lines. The obvious shape for it is SQLite, which is what
`../mystuff/STACK.md` prescribes for this stack and what the two sibling projects use.

Before reaching for it, we asked what the database would actually be doing. Pages were the only
table. `updated` is already on the filesystem, as the modification time. Slug uniqueness is already
enforced by the filesystem, through `O_EXCL`. Nothing else needed a query.

## Decision

One page is one pretty-printed JSON file in the folder named by `PAGES_DIR`. The file name *is* the
slug, so `pages/gym.json` is the page reached with `@gym`, and renaming a page is renaming a file.
Writes go to a temp file in the same folder and are renamed into place.

## Consequences

The folder is the whole database. The files are greppable, diffable, hand-editable, and readable by
every tool that reads text — which is what makes ADR-0004 possible at all. No driver is needed, so
`go.mod` stays empty (ADR-0002). Each file is stat'd per request and re-read when it changes, so an
edit made outside the app shows up on the next page load. A crash mid-save cannot leave a
half-written page, which matters more when the file is the only copy.

Against that: there are no indexes. Anything that needs one — full-text search, backlinks, revision
history — has to be a cache rebuilt from the files, never a second source of truth. Backlinks are
computed by scanning every file on demand; at this size that is far cheaper than keeping a register
honest, but it is linear and will not stay free forever.

Multiple users will stress this. `NewStore(dir, reg)` binds one directory at construction, so
per-user page sets would mean a store per user rather than a `WHERE` clause.

## Alternatives considered

**SQLite, as STACK.md prescribes.** Rejected: nothing needed it, and it would have made the data
opaque to everything except the app. The point of this project is that the notes outlive the program
that edits them.

**A stored backlink register, written when a page is read.** Rejected: reads would write to other
files, entries dangle when a link or a page is deleted, and a hand-edit bypasses the register
entirely. A computed answer cannot go stale.
