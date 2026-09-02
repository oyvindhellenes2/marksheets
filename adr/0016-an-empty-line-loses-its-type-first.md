# ADR-0016: An empty line loses its type before it becomes a heading

- **Status:** Accepted
- **Date:** 2026-09-02
- **Supersedes:** the empty-line rule in [ADR-0010](0010-depth-belongs-to-headings.md), and the
  `data` exception to it
- **Superseded by:** —

## Context

[ADR-0010](0010-depth-belongs-to-headings.md) settled that `Enter` on an empty line never leaves the
line lying there: it becomes a heading, one level out. The argument holds — an empty line is
somebody who has finished writing something, and what follows is usually a new section.

But it was one press from *anything* to a heading, and that skipped the more common ending. You
write a list, press `Enter` for one more item, decide the list is done, and press `Enter` again on
the empty one. You are not starting a section; you are carrying on in prose. What you got was a
heading, and the way back to a plain line was to type nothing, make the heading, and then unmake it.

That the rule was too coarse was already known, in one place: `data` had an exception, because a
blank row means the table is over and prose follows a table more often than a new section does. The
same sentence is true of a list, of a table's name line, and of a file line. `data` was not special;
it was the one type where the cost had been felt.

## Decision

**`Enter` on an empty line steps out one step per press**, and losing the type is one of the steps:

```
sub-sub-line → sub-line → line → text line → heading, one level out
```

Each press does one thing and stops. Nothing skips ahead, and every rung is somewhere you might have
wanted to stop.

Two exemptions, for the same reason: the rung would undo itself.

- **A heading** is already the type the ladder ends at, so it rises a level instead of shedding a
  type it would take straight back.
- **Under `Oppgåver`** there is no text line to become and nowhere to step out to, so an empty task
  stays an empty task rather than breeding another below it. That is unchanged.

The type step is taken only when the line is blank in **every** field, not just the one the caret is
in. A data line with a value but no name still holds something a text line has nowhere to keep.

## Consequences

`data` no longer needs a rule of its own. What it asked for is now what every type does, and the
exception is gone rather than generalised into a list of types.

Reaching a heading from an emptied list line takes two presses instead of one. That is the price,
and it is paid in the case where you were about to type a heading name anyway — while the case it
buys, carrying on in prose, previously had no gesture at all.

The type step goes through `setType` rather than `changeType`, which closed a hole: `outLevel` used
to call `changeType` directly, so `Enter` in the empty name field of a **table with cells in it**
turned the table into a heading and the cells were dropped on the next save. `setType` refuses that
and says so, the same guard the type menu has always had.

## Alternatives considered

**Keep one press to a heading and add a way back.** Rejected: the way back would be another gesture
to know about, for a state you did not ask to be in.

**Make the type step a setting, or apply it only to lists.** Rejected: `data` proved that the types
where it matters are found one complaint at a time. A ladder every type climbs is one rule; a list
of types that behave differently is a table to maintain and to remember.

**Delete the line instead, on the second press.** Rejected for the reason ADR-0010 gave: it destroys
the line you are standing on and leaves the caret nowhere obvious. The ladder ends at a heading,
which is a line with a job.
