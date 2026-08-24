# Marksheets

A hybrid of a spreadsheet and a markdown editor. Each page is a JSON tree: how deeply a
line is nested gives its heading level, and every other line is a **typed record** — text,
list, todo, task, data, image — with fields defined in an editable `types.json`.

Two things make it more than an outliner:

**`@`-queries pull data across pages.**

```
@treningsrommet/budsjett/totalt   →  25000 kr        a value, inline
@hytta/oppgåver[#øyvind]          →  every task tagged øyvind
@treningsrommet/utstyr            →  the whole section, transcluded
```

Queries are written as readable paths but resolved by node id, so renaming a heading never
breaks one — even when you rename it by hand with the app closed.

**Tasks open working files.** A `task` line owns a page of its own: scratch space for that
job, reachable only through the task, never listed on the front page. Finishing a task files
it under a folded `Arkiv` heading, so the main page stays clean.

## Storage

One page is one JSON file. `pages/gym.json` is the page you reach with `@gym`. There is no
database — the folder is the whole thing, files are pretty-printed and meant to be edited by
hand, and the app re-reads them when they change on disk. Saving is manual (`⌘S`) and makes a
git commit, so history is git's.

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
| `⌘Z` / `⇧⌘Z` | undo / redo |
| `⌘S` | save and commit |

The interface is in Norwegian (nynorsk). [`SPEC.md`](SPEC.md) is the design document — what
the format is and why it ended up this way. [`Marksheets.md`](Marksheets.md) is the original
brief, kept for the record; where the two disagree, `SPEC.md` is what was built.
