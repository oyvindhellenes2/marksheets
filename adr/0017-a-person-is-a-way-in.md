# ADR-0017: A person is a way into the pages

- **Status:** Accepted
- **Date:** 2026-09-02
- **Supersedes:** —
- **Superseded by:** —

## Context

Every way into this app starts from a page. The front page lists pages, a tag filters that list, an
`@`-query names a page and walks down it, a backlink says which page points here. That is right for
writing: a task belongs on the page the work is about, and `Oppgåver` puts it at the top of that
page where it cannot be missed.

It is no use at all for the other question. *What am I meant to be doing?* is not about a page, and
answering it meant opening every page in turn and reading the box at the top.

The information was already there and already addressable. A task's `owner` is a field of kind
`tag`, and `@side/oppgåver[#øyvind]` has always filtered on it. What was missing was the same filter
without a page in front of it.

## Decision

**`/ansvarleg/{namn}` is everything one person is named on**, gathered from every page and grouped by
the page it was written on. Open tasks first; finished ones folded behind a count.

**A name is a field of kind `tag`, not a field called `owner`.** The page and the query language
match on the same rule, so a type that grows such a field turns up here without being told about,
and the two cannot drift into disagreeing.

**Nothing is stored.** The pages are read and the answer assembled on each request — the same
bargain as backlinks and the unpublished set. At this size the scan is far cheaper than keeping a
register honest, and an answer worked out on demand cannot be stale.

**Working files are included.** A task page is left off the front page because it is reached through
its task and nowhere else; but the todos written on one are still somebody's job, and leaving them
out would make the list quietly wrong. Each such group says which task it hangs off.

Two ways in, both of them links that already read as questions: the home page indexes the names
beside the tags, and the `#namn` beside a task in the read view points at that person's page.

## Consequences

There are now two indexes on the home page — tags and names — and they answer different questions
about the same pages. The name row says what it is, because two rows of hashtags with nothing to
tell them apart would be a puzzle.

The address is the name's slug, so `/ansvarleg/øyvind` and not `oyvind`: `doc.Slug` keeps Norwegian
letters, exactly as it does for tags and page names. Every link to the page is generated, so this is
only visible to somebody typing the URL by hand.

The list is read-only. Ticking a task off has to happen where the task lives, which is a real cost
and the honest one: a checkbox here would be an edit to another page, made from a view that is not
that page, with no undo of its own.

## Alternatives considered

**Make `@#øyvind` — a query with no page in it — resolve across all pages.** Tempting, since it is
the syntax the request was written in. Rejected for now: every other query resolves inside one named
page, which is what makes the cycle guard, the recorded link ids and the backlinks work. A query
whose scope is "everything" would have a different meaning for each of those, and getting it wrong
would break the ones that work. The rendered page is the cheaper half of the idea; the query can be
built on top of it later if it is still wanted.

**A saved "my tasks" page in the pages folder.** Rejected: it would be a second copy of what the
pages already say, and would go stale the moment a task moved — the same reason nothing else here is
stored.

**Filter the front page by owner instead**, alongside `?emne=`. Rejected: the front page lists
*pages*, and this is a list of *lines*. Pretending they are the same list would either hide which
page a task came from or turn the page list into something else.
