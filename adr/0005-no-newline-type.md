# ADR-0005: No `newline` line type

- **Status:** Rejected
- **Date:** 2026-08-28
- **Superseded by:** —

## Context

Pressing Enter on an empty text line adds another empty row rather than doing anything useful, and
pages felt cramped. A `newline` line type was proposed: assigned automatically when Enter is pressed
on an empty line, rendering as blank vertical space, so you could put air between content.

## Decision

Rejected. Spacing is presentation and belongs in the stylesheet.

## Why

**It puts presentation into the data**, which is the one trade this project has refused throughout.
It is the same trade as storing `-[ ]` instead of a `done` boolean and rendering the checkbox from
it — rejected then for the same reason.

**Nodes are not inert.** A spacer would be pulled in when its section is transcluded, inflate the
"lines hidden" count on a folded heading, be scanned by `Store.Backlinks`, and mint a permanent
`n_xxxx` id for a piece of whitespace — in a system whose entire addressing scheme is built on those
ids meaning something.

**It is not expressible.** `doc/types.go` only fills in `Primary` when a type has fields, and the
editor dereferences `primary` throughout as "the field the caret lands in". A fieldless type would
need a dummy always-empty field, at which point it simply *is* an empty text line wearing a costume.

## What was done instead

The actual cause was `.rows { gap: 3px }` — one uniform gap between every row whatever it was, with a
single flat `margin-top` on headers. It was replaced with a spacing scale and pairwise rules, so the
gap between two lines is decided by the pair they make. Separately, an empty line was given
`min-height: 1lh` so it occupies a line in the read view instead of collapsing to zero height and
taking its margins with it.

## Still open

Enter on an empty **text** line still adds another empty row. `outdent` succeeds only for items and
for headers, so `text` is the one type with no fallback. Promoting it to a heading was considered and
set aside; nothing has been built.
