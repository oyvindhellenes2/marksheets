# ADR-0009: Every page carries at least one hashtag

- **Status:** Accepted
- **Date:** 2026-09-01
- **Supersedes:** —
- **Superseded by:** —

## Context

A page had a title, a slug and nothing else to say what it was about. The home page listed the two
of them: the title, and `slug.json` in a monospace chip beside it. The slug is derived from the
title, appears in the URL bar, and is written into every `@`-query pointing at the page, so the chip
repeated something already visible three other ways.

Meanwhile there was no answer at all to "what is this page part of". Twelve of the twenty-three
pages in use are working files named after a job — `bestille-ved-til-vinteren`, `montere-veggspegel`
— and nothing on the index said which of them were about the cabin and which about the garden.

`#hashtag` already existed inside a line's text, matched by `hashtagsIn` and reachable through a
query filter. That is a property of a line, not of a page, and a query cannot address a page by it.

## Decision

A page carries a doc-level `tags` array, and **there is always at least one**.

- Asked for on the form that makes a page, beside the name, and `required` there.
- Edited on the page itself, under the title, as chips with an add field.
- Stored as slugs, so `#` is presentation. Written as words, separated by spaces or commas alike.
- Shown on the home page **in place of** `slug.json`, and under the title in the read view.
- A working file inherits the tags of the page whose task opened it.

The guarantee is kept in two places, deliberately. The editor refuses to remove the last tag and
says why, so the rule is felt where it is broken. `Doc.EnsureTags` fills in the page's own slug for
a document that arrives with none, so a file written by hand — or written before this decision —
is repaired rather than rejected.

## Consequences

Every page that existed before this is tagged with its own slug on first load, and that tag is
written to the file on its next save. That is a weak tag, and the intent is that it gets replaced
by hand; it is there so nothing is ever listed with nothing.

Nothing reads the tags yet beyond printing them: no search, no grouping on the index, no `@`-query
that takes one. This decision is about getting the data recorded while the pages are few enough to
tag properly. Building the filter first and finding twenty-three untagged pages is the order that
does not work.

## Alternatives considered

**Reuse the in-text `#hashtag`.** Rejected: it makes what a page is about depend on where somebody
happened to write a word, it cannot be edited as a set, and a page with no lines yet could not have
one. A page-level fact belongs in a page-level field — the same argument that put `done` in a field
rather than in the text as `-[ ]` ([SPEC](../SPEC.md), *Document model*).

**Make tags optional.** Rejected, and this is the whole decision. Optional metadata on a personal
tool is metadata that exists on the first three pages and nowhere after that, which makes it useless
exactly when there are enough pages to need it.

**Keep the file name on the index alongside the tags.** Rejected: the row then carries two chips
that compete, and the file name is the one already answerable from the URL. If a slug is ever needed
on the index, that is a sign the page needs renaming, which is not built yet either.

**Enforce the minimum only on the server, silently.** Rejected: the editor would show no tags while
the file held one, and the two would drift until a reload. The rule is enforced where it is broken
and repaired where a file arrives from outside.
