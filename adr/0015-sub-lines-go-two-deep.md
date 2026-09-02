# ADR-0015: Sub-lines go two deep

- **Status:** Accepted
- **Date:** 2026-09-02
- **Supersedes:** the "cannot nest further" half of [ADR-0003](0003-only-headers-nest.md)
- **Superseded by:** —

## Context

[ADR-0003](0003-only-headers-nest.md) put a list's sub-lines *inside* the line, in an `items` array,
rather than beside it as children. That decision was about where sub-lines live, and it stands. It
came with a second clause that was never argued for on its own: items are one level deep and cannot
nest.

One level is not enough for a list. "Måndag — beina" with "knebøy" under it wants "5 × 5" under
*that*, and the only things the editor offered instead were a heading — which is a section of the
page, not a detail of a line — or nothing.

The one-level limit was also not quite real. `flatten` in the editor already recursed into nested
`items`; `doc.Normalise` already read them. Both then flattened what they found into a single level,
so a file that said two levels lost one on the next save, silently.

## Decision

**A sub-line may hold sub-lines. Two levels, and there it stops** — `doc.MaxItemDepth`.

`Tab` on a sub-line puts it inside the nearest sub-line above it at its own level; anything deeper
in between already belongs to that line, and this joins it. `⇧Tab` steps back out, and from the
first level makes the line a line again, taking its own sub-lines with it. A sub-line that already
holds sub-lines cannot itself become one — that would need a third level.

The level is carried on the row as a number rather than a flag: `item: 0 | 1 | 2`. Truthiness reads
the same as the old boolean everywhere it was tested, and the level is then available where it is
needed.

Anything arriving deeper than two — a hand-written file, a paste, a save from an older editor — is
**lifted into the deepest level there is**, never dropped. `asItems` does it on the way in and
`reflow` does it on the way to the screen, so the file, the model and the picture agree.

## Consequences

Two levels of sub-line are what a list gets, and the third level is a heading. That is the same
sentence as ADR-0003 with a different number in it, and the number is the whole decision.

`nest()` keeps a small stack of hosts by level instead of one host. `reflow` recomputes a sub-line's
level as well as its depth, which caught an old sloppiness for free: a sub-line pasted after a line
that cannot hold one used to draw as a sub-line and save as an ordinary line. It is now a line
straight away, on screen, where you can see it.

Numbering in the gutter had to learn to skip: `ordinalOf` counted upwards until it met a row that
was not part of its run, and a sub-line under the previous item *is* such a row. Every numbered line
after one with sub-lines restarted at 1. That was already wrong before this change and is fixed with
it.

## Alternatives considered

**Unlimited nesting inside a line.** Rejected: it is an outline, and the app already has one — with
levels that carry through to `h1`–`h6`, to `@`-paths, to renaming and to backlinks. A second,
parallel outline hidden inside a list line would have none of that.

**Let a sub-line be a real child, so the ordinary depth machinery handles it.** Rejected: that is
exactly what ADR-0003 removed, and the reason it removed it — a `children` field on a leaf that can
be filled in wrongly, and once was, losing five lines — has not changed.

**Three levels, or a configurable limit.** Rejected: nobody asked for three, a limit that is a
setting is a limit nobody has thought about, and every level costs a rule in `indent`, `outdent`,
`reflow` and `nest`. Two is the number that was missing.
