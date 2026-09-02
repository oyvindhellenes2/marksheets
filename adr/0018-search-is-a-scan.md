# ADR-0018: Search is a scan of the files

- **Status:** Accepted
- **Date:** 2026-09-02
- **Supersedes:** —
- **Superseded by:** —

## Context

Search was on the "not built yet" list from the beginning, and the reason it stayed there was never
the searching — it was the fear of the index. [ADR-0001](0001-pages-are-files.md) put the pages in
files with no database, and the note under it said that when something eventually needed an index,
the right shape would be a cache rebuilt from the files rather than a second source of truth.

By the time search was actually asked for, three things had been built that would have needed such
an index and did without one: backlinks, the unpublished set, and the people index
([ADR-0017](0017-a-person-is-a-way-in.md)). Each of them reads the folder on the request that asks
the question. None of them can be stale, because there is nothing to be stale.

The pages are also edited by hand, arrive over git, and change while the app is not running
([ADR-0001](0001-pages-are-files.md), [ADR-0006](0006-pages-in-their-own-repository.md)). An index
would have to be honest across all three, which is where the work in an index actually is.

## Decision

**Search reads the files and looks.** `Store.Search` walks every page, every line, and every table
row, matching case-insensitively, and returns what it found grouped by page. Nothing is written and
nothing is remembered between requests.

**A line is searched as it reads on the page.** The fields of a line are joined in the order the
type declares them, so a data line is `budsjett 25000 kr` and matches on any part of it; a task
carries its owner as `#øyvind`; a table is searched a row at a time, because a table's content is in
cells rather than fields.

**The box does two things.** Typing offers page *names*, which answers "take me to the page I am
thinking of"; `Enter` runs the scan, which answers "where did I write that". They are different
questions and it would be a worse box if it guessed which one was meant.

## Consequences

Search cannot disagree with the pages, cannot need rebuilding, and needed no schema, no migration
and no invalidation rule. The cost is linear in the size of the folder on every search: a few dozen
JSON files parsed — and mostly already parsed, since `Store` caches each page against its file
stamp and only re-reads what changed on disk.

There is a size at which this stops being true. It is not near: at a thousand pages the scan is
still milliseconds, and the honest signal to revisit this is a search that feels slow, not a number
guessed in advance. When that day comes, the shape is already decided — a cache rebuilt from the
files.

Matching is a plain case-insensitive substring. No stemming, no word boundaries, no ranking beyond
"a page whose name matched comes first". `kaffi` finds `filterkaffi`, which is usually what somebody
wants from notes and occasionally noise.

## Alternatives considered

**SQLite with FTS5.** The obvious answer, and it would be a good one for a different app. Rejected
here because it means a dependency in a module that has none ([ADR-0002](0002-no-dependencies.md)),
a second copy of every note, and a rule for keeping that copy honest against hand edits and `git
pull`. The scan has none of those and answers the same question.

**An in-memory index built at startup and updated on save.** Cheaper to justify — no dependency —
but it has the same invalidation problem for anything that changes the files without going through
a save, which here includes a text editor, a `git pull` and a restore from history. It would be
fast and occasionally wrong, and being occasionally wrong is what makes people stop trusting a
search box.

**Search only page names.** Rejected as too little: the name is what you already remember. The
whole value is finding the line whose page you have forgotten. Names are still offered first, as
you type, because that half is the fast path.

**`grep` shelled out.** Tempting, since `vcs` already shells out to git. Rejected: the files are
JSON, so a raw grep matches escapes, keys and ids, and would have to be un-escaped and mapped back
to lines anyway — which is the parsing this does, with none of the control.
