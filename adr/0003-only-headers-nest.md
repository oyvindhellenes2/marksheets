# ADR-0003: Only headers nest

- **Status:** Accepted
- **Date:** 2026-08-24
- **Superseded by:** —

## Context

The document is a tree and nesting depth gives heading level. At first any line could hold children,
and "only headers nest" was a rule enforced on every Tab, every type change, and every save.

The enforcement failed once. An outdent bug produced a tree with todos sitting under a text line, and
the save silently deleted five lines of real content. The rule was correct; policing it in five
places meant it would eventually not be policed in a sixth.

## Decision

Make it structural rather than enforced.

- A **header** holds `children`. This is what gives a page its outline.
- A **list** or **todo** holds `items` — sub-lines kept *inside* the line, carrying the same fields,
  inheriting its type, and unable to nest further.
- **text**, **data** and **image** hold nothing.

There is no `children` field on a leaf to fill in wrongly, so the invalid shape cannot be built.

## Consequences

The rule cannot be broken by a bug, because the shape it would produce does not exist.

Tab is correspondingly narrow, and deliberately so: a header moves between outline levels carrying
its contents; a list or todo becomes an item of the nearest line above it, provided that line is the
same type; everything else has no indentation of its own, and where it sits is decided by the heading
it is under. That is why `Enter` on a header opens the first line *inside* it rather than a sibling
beside it, and why `⇧Enter` exists at all — without it, moving to a new section meant typing `#` and
then outdenting.

Items are still walked like any other line, so tags, `@`-queries and backlinks reach them.

## Paired with

`doc.Normalise` **lifts** orphans to the level above rather than deleting them. A node whose type
cannot nest hands its children upward instead of losing them. That is the other half of the same
lesson: malformed input gets repaired, never quietly discarded.

## Alternatives considered

**Keep the rule and fix the outdent bug.** Rejected. The bug was noticed only because it destroyed
content, which is a poor detector. A constraint that can only be maintained by remembering to check
it will eventually not be checked.
