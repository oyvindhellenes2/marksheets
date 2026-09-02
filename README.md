# Marksheets

A hybrid of a spreadsheet and a markdown editor. (The instance this was built for calls itself
*Wiki for Verftet* — the site's name lives in `base.html` and is one edit to change.) Each page is a JSON tree: how deeply a
line is nested gives its heading level, and every other line is a **typed record** — text,
list, todo, task, data, image — with fields defined in an editable `types.json`.

Three things make it more than an outliner:

**`@`-queries pull data across pages.**

```
@treningsrommet/budsjett/totalt   →  25000 kr        a value, inline
@hytta/oppgåver[#øyvind]          →  every task tagged øyvind
@treningsrommet/utstyr            →  the whole section, transcluded
```

Queries are written as readable paths but resolved by node id, so renaming a heading never
breaks one — even when you rename it by hand with the app closed.

**Tables have as many columns as you want.** A `table` line declares its columns once and holds
rows of cells under them. `Tab` walks the cells and makes a new column off the right-hand edge,
`Enter` opens a row, and a blank row drops you back into prose.

**Files are uploaded, not linked.** Drop one on the page and it attaches. The file is stored in
`filer/` beside the pages, so it travels with them — same backup, same repository, and publishing a
page takes its attachments along. Pictures draw inline; anything else gets a box, and a PDF shows
its first page in it.

**Tasks open working files.** A `task` line owns a page of its own: scratch space for that
job, reachable only through the task, never listed on the front page. Every page starts with a
pinned `Oppgåver` heading that holds them — the app's line, not yours: it cannot be renamed or
moved, holds tasks and nothing else, and the whole section is left out of the read view. Finishing
a task files it under a folded `Arkiv` heading, so the main page stays clean.

A task also carries a **person** — picked from the people who have signed in, not typed — and
`/kari` is that person: everything with their name on it, gathered from every page and grouped by
the page it was written on. Nothing is stored for it; the pages are read when you open it.

Every page also carries at least one **hashtag**, asked for when the page is made and edited
under its title. The index lists a page by them.

**The index is a sidebar**, down the left of every page: the pages, the tags that narrow the list in
place, the form that makes a new one, and `Publiser`. `☰` shuts it and it stays shut. There is no
index *page* — `/` opens whatever you edited last.

**Search is a scan, not an index.** The box in the header offers page names as you type (`⌘K` to
reach it, `↑`/`↓` to pick); `Enter` reads every line of every page and shows what matched, grouped
by page, with the headings above it. Nothing is stored to make that work
([ADR-0018](adr/0018-search-is-a-scan.md)).

## Storage

One page is one JSON file. `pages/gym.json` is the page you reach with `@gym`. There is no
database — the folder is the whole thing, files are pretty-printed and meant to be edited by
hand, and the app re-reads them when they change on disk. Uploaded files sit in `pages/filer/`, so
they travel with the pages. Saving is automatic; publishing (`Publiser` in the sidebar, or `⌘S` outside the
editor) commits every changed page and pushes, so history is git's.

`pages/` is a **separate git repository**, ignored by this one, so publishing a page can never push
source and the notes can stay private. A fresh clone therefore has no pages —
[`examples/`](examples/) holds an invented set that shows what the app does:

```sh
PAGES_DIR=examples /opt/homebrew/bin/go run ./cmd/marksheets
```

`go.mod` has **zero dependencies**.

**Several people can use it.** Sign-in is OIDC against [Pocket ID](https://pocket-id.org), written
against the protocol rather than a library, so `go.mod` still lists nothing. With no provider
configured the app runs as one local user and no login screen, exactly as it did before.

Two people on one page do not overwrite each other: a save answers for the version it started from,
and one that would land on top of somebody else's work is refused rather than silently winning
([ADR-0021](adr/0021-a-save-answers-for-what-it-read.md)). An open editor also shows who else has
the page open.

## Running it

```sh
go run ./cmd/marksheets      # http://localhost:3003
```

`PAGES_DIR` moves the page folder, `TYPES_PATH` points at your own line types, `PORT` changes
the port.

To put it behind Pocket ID:

```sh
AUTH_ISSUER=https://tilgang.verftet.info \
AUTH_CLIENT_ID=… AUTH_CLIENT_SECRET=… \
AUTH_BASE_URL=https://wiki.verftet.info \
go run ./cmd/marksheets
```

The redirect URI to register with the provider is `<AUTH_BASE_URL>/logg-inn/attende`. `AUTH_LOCAL`
names the single user when there is no provider; `USERS_PATH` moves the list of people, which is
kept beside the pages rather than in them.

## Editing

| Key | Does |
|---|---|
| `Enter` | new line; on a heading, the first line *inside* it |
| `⇧Enter` | new heading beside the one you are in |
| `⌥Enter` | soft line break |
| `Tab` / `⇧Tab` | heading in/out a level; list or todo becomes a sub-line, two levels deep |
| `⇧↑` / `⇧↓` | select whole lines; `⌘C`/`⌘X`/`⌘V` copy, cut and paste them |
| drag the gutter | move a line, and everything under it, somewhere else |
| `⌘Z` / `⇧⌘Z` | undo / redo |
| `⌘⏎` | read / edit |
| `⌘K` | jump to the search box, from anywhere |
| `⌘S` | outside the editor: publish everything changed |

Write `@side` to link to a page, `@side(eige namn)` to name the link yourself, and
`@side/bolk` to pull that section in instead.

The interface is in Norwegian (nynorsk). [`SPEC.md`](SPEC.md) is the design document — what
the format is and why it ended up this way. [`Marksheets.md`](Marksheets.md) is the original
brief, kept for the record; where the two disagree, `SPEC.md` is what was built.
