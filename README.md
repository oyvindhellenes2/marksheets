# Marksheets

A hybrid of a spreadsheet and a markdown editor. Each page is a JSON tree: how deeply a
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

**Files are uploaded, not linked.** A `file` line stores the file in `filer/` beside the pages, so
it travels with them — same backup, same repository, and publishing a page takes its attachments
along.

**Tasks open working files.** A `task` line owns a page of its own: scratch space for that
job, reachable only through the task, never listed on the front page. Every page starts with a
pinned `Oppgåver` heading that holds them — the app's line, not yours: it cannot be renamed or
moved, holds tasks and nothing else, and the whole section is left out of the read view. Finishing
a task files it under a folded `Arkiv` heading, so the main page stays clean.

Every page also carries at least one **hashtag**, asked for when the page is made and edited
under its title. The index lists a page by them.

## Storage

One page is one JSON file. `pages/gym.json` is the page you reach with `@gym`. There is no
database — the folder is the whole thing, files are pretty-printed and meant to be edited by
hand, and the app re-reads them when they change on disk. Saving is automatic; publishing
(`Publiser`, or `⌘S`) commits and pushes, so history is git's.

`pages/` is a **separate git repository**, ignored by this one, so publishing a page can never push
source and the notes can stay private. A fresh clone therefore has no pages —
[`examples/`](examples/) holds an invented set that shows what the app does:

```sh
PAGES_DIR=examples /opt/homebrew/bin/go run ./cmd/marksheets
```

`go.mod` has **zero dependencies**.

## Running it

```sh
go run ./cmd/marksheets      # http://localhost:3003
```

`PAGES_DIR` moves the page folder, `TYPES_PATH` points at your own line types, `PORT` changes
the port.

## Editing

| Key | Does |
|---|---|
| `Enter` | new line; on a heading, the first line *inside* it |
| `⇧Enter` | new heading beside the one you are in |
| `⌥Enter` | soft line break |
| `Tab` / `⇧Tab` | heading in/out a level; list or todo becomes a sub-line |
| `⇧↑` / `⇧↓` | select whole lines; `⌘C`/`⌘X`/`⌘V` copy, cut and paste them |
| `⌘Z` / `⇧⌘Z` | undo / redo |
| `⌘⏎` | read / edit |
| `⌘S` | publish (commit and push) |

Write `@side` to link to a page, `@side(eige namn)` to name the link yourself, and
`@side/bolk` to pull that section in instead.

The interface is in Norwegian (nynorsk). [`SPEC.md`](SPEC.md) is the design document — what
the format is and why it ended up this way. [`Marksheets.md`](Marksheets.md) is the original
brief, kept for the record; where the two disagree, `SPEC.md` is what was built.
