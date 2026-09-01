# ADR-0007: Saving and publishing are separate acts

- **Status:** Accepted
- **Date:** 2026-08-29
- **Amends:** [ADR-0004](0004-git-is-the-historian.md), by adding the push layer
- **Superseded by:** —

## Context

`⌘S` wrote the file and made a git commit in one gesture. Those two jobs want different rhythms:
durability constantly, history at the points you choose. One gesture meaning both forced a choice
between a noisy log and losing work, and the log records which way it went — `Hagen: endra` twice in
a row, `Treningsrommet: endra`, `Kjøpe romaskin: endra` twice. Those are not checkpoints, they are
keystrokes with a hash.

The safety problem was worse than the tidiness one. An hour of unsaved typing existed **only** in
`localStorage`: one browser, one machine, gone if it was cleared. The rest of the design already
believed the file was the only copy (ADR-0001); saving was the one place that was not true.

## Decision

Two acts.

**Saving is automatic.** `PUT /p/{slug}` writes the file and nothing else, about a second after
typing stops. The `localStorage` draft stays, because autosave is a timer and the gap between the
last keystroke and the next tick is exactly where a crash lands.

**Publishing is deliberate.** `Publiser`, or `⌘S`, commits what is on disk and pushes it. Until
then the work exists on this machine and nowhere else, and the home page says so: a page whose file
differs from what has been published carries a marker. That state is computed against
`origin/<branch>` on every request and never stored — the same reasoning as backlinks — so it covers
both kinds of unpublished work, edited-but-not-committed and committed-but-not-pushed.

## Consequences

The durable copy is the file, which is what the rest of the design already assumed. The log becomes
checkpoints you chose rather than a record of typing.

Several things followed that were not obvious going in:

- **The working tree is dirty whenever you are typing.** With pages in the same repository as the
  code that was a hazard; ADR-0006 removed it for a different reason and took this with it.
- **"Just don't save" stopped being the way to back out.** A `Forkast` button was built to replace
  it and then removed in favour of restoring any version from the history, which is the same
  operation with the version fixed to the newest one. One mechanism that reaches any version beats
  two that overlap.
- **Only *heading* renames may count in a commit message.** `findRenames` matches every node,
  correctly, because a query can point at any of them. Feeding that to the message meant an edited
  text line was reported as a renamed heading — one odd commit under manual saving, but under
  autosave a publish accumulates them, and an ordinary session would have claimed "5 overskrifter
  endra namn" about five sentences.
- **Re-rendering on every save became a live cost.** `save()` rebuilt every row whenever the server
  returned task states. That was rare when saving was something you asked for; at one save per
  second it wiped the caret, any open menu and the link marks. It now rebuilds only when the task
  states have actually moved.
- **The marker is accent-coloured, not red.** Red already means a file that will not parse, and with
  autosave "unpublished" is the ordinary state of every page you have touched. A home page of red
  teaches you to ignore the colour exactly where it needs to alarm you.

## Alternatives considered

**Keep manual saving and warn harder about unsaved work.** Rejected: the conflict is that the two
jobs want different frequencies, and a warning does not resolve it — it just makes the losing side
louder.

**Autosave, and commit on every autosave.** Rejected: that is the noisy log, produced faster. It
also makes every commit message meaningless, which is what makes the history worth having.

**Publish without pushing** — commit locally, push by hand. Rejected because the button says
*Publiser*: a local commit changes nothing anyone else can see, and a name that promises otherwise is
false at the exact moment it claims to be true.
