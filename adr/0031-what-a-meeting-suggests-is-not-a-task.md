# ADR-0031: What a meeting suggests is not a task

- **Status:** Accepted
- **Date:** 2026-09-09
- **Supersedes:** —
- **Superseded by:** —

## Context

Fjernmøte hands over a finished meeting ([ADR-0030](0030-a-meeting-belongs-to-a-working-document.md)),
and part of it is a list of *innspel* — things a model reading the transcript thought somebody
might want to follow up. The brief asks for "transcript, referat and possible suggestions written
into the working document of that task".

This archive has a task type. A suggestion out of a meeting is exactly the shape of one, and
writing them in as unowned tasks is a line of code.

## Decision

**They are written as `list` lines under a `callout` that says they came from a machine and are
for consideration.** Never `task`, never `todo`, and nothing carrying an owner.

## Consequences

Somebody has to retype a suggestion to make it a task. That friction is the decision, not a
shortcoming of it.

A task here is a commitment. It is numbered, the number is permanent and meant to be said out loud
([ADR-0008](0008-the-tasks-heading-is-furniture.md) and the numbering rules in SPEC), it lands on
somebody's profile under what they are down for ([ADR-0020](0020-a-person-is-not-a-tag.md)), and
the archive counts it as open work. None of that should be created by a model's reading of a
transcript that was itself produced by a model — whisper hallucinates on silence and loops on
repetitive audio, and both were seen in the first hour of testing.

The failure to avoid is not one wrong task. It is that after a month of meetings nobody trusts the
task list, because some of it was agreed by people and some of it was inferred. A task list that
is only mostly real is worse than one that is short.

The same reasoning is why the referat is labelled as machine-written where it appears, and why the
transcript arrives as an attachment rather than as page content.
