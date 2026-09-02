# ADR-0021: A save answers for what it read

- **Status:** Accepted
- **Date:** 2026-09-02
- **Supersedes:** the last-write-wins note in [SPEC](../SPEC.md), *Implementation notes*
- **Superseded by:** —

## Context

The editor owns the tree and saves it whole, on a timer, about a second after you stop typing. There
was no check on the way in: the file was overwritten with whatever arrived. With one person that is
almost always right, and the one hole in it was documented and lived with — editing a page in the
app and in a text editor at once meant last-write-wins.

With more than one person it stops being a corner case. Two people on the same page, both typing:
every second, each save silently erases whatever the other has written since. Nobody sees an error.
The work is simply not there afterwards.

Worth separating from the other thing "merge conflict" can mean here. Git conflicts in the pages
repository can only happen if something *other than this server* writes it — a second clone, an edit
on GitHub, a second instance. With one server as the sole writer there are none, and the answer is
to keep it that way rather than to teach the app to merge.

## Decision

**Every save answers for the version it started from.** The editor is given the file's stamp when it
loads a page and sends it back with each save; `Store.Save` refuses a save whose version is no longer
what is on disk, and refuses it *before* writing anything, before task pages are created or removed,
and before links are rewritten. The check and the write are held under one lock, or the two saves
this exists to separate could both check, both pass, and both write.

The version is modification time and size — the same pair the page cache already trusts to decide
whether a file has changed. Not a content hash: a hash would say "the same bytes are back" after an
undo, which sounds better and is worse, since two people would then both be allowed to save having
each seen a different middle.

**A refusal stops the editor rather than retrying.** Autosave halts, a bar says who saved and that
nothing of yours was written, and the draft in `localStorage` stands. Two buttons: take theirs, or
keep mine — the second being a deliberate overwrite, made once, by somebody who has been told what
it costs. Retrying automatically would either lose their work or spend the afternoon failing once a
second.

**Presence makes it rare.** An open editor says hello every twenty seconds and is told who else is
on the page. In memory, advisory, and forgotten on restart: it is a courtesy that makes a collision
unlikely, and the version check is what makes one harmless.

**No merge — yet.** Two people editing *different lines* of the same page is not a conflict in this
data model: nodes have permanent ids and fields have names, so a three-way merge is per-node set
arithmetic rather than text diffing. That is the elegant answer and it is deliberately not built.
Build it when the refusals become annoying, and let them say how often that is.

## Consequences

The hole is closed for the case it was written for, too: the app and a text editor can no longer
overwrite each other silently.

Nothing is locked. Two people can still open the same page, and the second one to save is the one
who has to decide — which is the right way round, since they are the one who knows what they were
writing.

The refusal is per *page*, not per line, so two people typing in different sections of one page will
interrupt each other even though nothing they wrote overlaps. That is the cost of not merging, and
it is the signal to build the merge.

A save with no version — the restore path, and anything that writes a whole document without having
had one open — still goes through unchecked. That is deliberate: it is not answering for anything
it read.

Sessions, presence and last-saver are all in memory. A restart forgets who was where, and the worst
that costs is a save that goes through without a name attached to the refusal it never caused.

## Alternatives considered

**Locking a page while somebody has it open.** Rejected: locks in a wiki get left on. Somebody
closes a laptop and a page is unwritable until a timeout nobody can see. Presence says the same
thing without taking anything away.

**Commit every save to git and let git merge.** Tempting, since the repository is right there.
Rejected: git merges *text*, JSON merges badly line-wise and can produce a file that will not parse,
it would flood the history, and it contradicts [ADR-0004](0004-git-is-the-historian.md) — git is the
historian, never the database.

**A CRDT and live collaborative editing** (Yjs and friends). Rejected: a dependency
([ADR-0002](0002-no-dependencies.md)), a websocket layer, and a rewrite of the editor's model —
permanently — to solve a rare problem for a handful of people. If this wiki ever becomes a place
where two people write the same paragraph at the same time on purpose, that is the day to reopen it.

**Per-node saves instead of the whole document.** It would make most conflicts impossible, and it is
a different editor: the whole design is that the editor owns the tree and rebuilds it on save. Worth
remembering as the shape this would take if it were being started again.

**Merge now rather than later.** Rejected on evidence, not on principle: nobody knows yet how often
two people will be on one page. The refusal is what will say.
