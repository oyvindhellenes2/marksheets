# ADR-0026: A callout is one type with a flavour, not four types

- **Status:** Accepted
- **Date:** 2026-09-04
- **Supersedes:** —
- **Superseded by:** —

## Context

Pages had no way to say "stop and read this". A page about a sprinkler system
needs to mark the difference between a note, a hint, an unverified section and a
genuine hazard, and prose cannot carry that: by the time the reader has read the
sentence saying it is dangerous, they have read past it.

Obsidian's callouts are the reference — `> [!warning]` and a coloured box — and
they are one syntax with a type parameter, because markdown has exactly one
blockquote to hang everything off.

This wiki has a type registry instead, so the obvious move was four types:
`info`, `tips`, `atvaring`, `fare`. That needs no new machinery at all. Types
*are* the mechanism here; the picker, the gutter icons and `⌘1`–`⌘6` all exist
already, and [ADR-0011](0011-a-table-is-its-own-type.md) settled that a thing
with its own shape gets its own type.

## Decision

**One `callout` type with a `slag` field**, and a new field kind — `choice` —
to hold it.

Four types would have been cheaper today and wrong by next month. The four
flavours are not four kinds of line: they are one kind of line in four voices,
with identical fields, identical nesting and identical behaviour, differing in a
colour and a glyph. Splitting them would put that sameness in four places and
make "this warning is really just a note" a type change rather than picking a
different word from a menu. It would also add four entries to a picker for one
concept.

`choice` is the smallest thing that expresses it: a field with a fixed set of
values, rendered as a real `<select>`. The browser already has the keyboard
handling, the touch behaviour and the accessible name; a line type is a poor
place to reinvent any of them.

**The first option is the default**, so a choice field is never empty and the
control always has something selected. **An unrecognised value falls back to the
first option** rather than drawing a blank control or vanishing — `types.json`
is editable, so a page can outlive the choice it was written with, and the same
reasoning that makes `Normalise` lift orphans rather than drop them applies to a
flavour that has been renamed.

**The word is drawn as well as the colour.** A coloured box with a triangle in
it means "careful" to somebody who already knows the convention and nothing to
anybody else, and colour alone is not available to every reader. Info, Tips,
Åtvaring and Fare are written above the text.

## Consequences

`choice` is now available to any future type, which is the main reason it was
worth building rather than hard-coding four flavours. The cost is one more field
kind that the editor, `Defaults` and anything reading a node have to know about
— the same tax `bool`, `user` and `file` already pay.

A callout nests like a list: `nestable`, no headers. A callout holding several
lines is a common thing to want, and "only headers nest" stays true because the
extra lines are `items` inside the line rather than children beside it.

What this does not do is let a callout hold a heading, a table or a picture.
Obsidian's callouts can. If that turns out to be wanted, the answer is probably
to let a header carry a flavour rather than to make callouts nestable-with-
headers, since the outline is the header's job and there should be one of it.
