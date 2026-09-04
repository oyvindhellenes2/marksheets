# ADR-0025: A task page is made on the way to it

- **Status:** Accepted
- **Date:** 2026-09-04
- **Supersedes:** — (no record decided this; that a save creates the working file was written only
  in [SPEC](../SPEC.md), *Tasks and working files*, which this changes)
- **Superseded by:** —

## Context

A task-todo owns a working file: somewhere to write about the one thing without filling up the page
the task hangs off. Until now `Store.Save` made that file, for every task that had text and no page
yet, on the save that gave it its text — which is to say, about a second after somebody finished
typing the task.

That was chosen because it makes the arrow beside a task always mean the same thing: it is a link,
and the page it points at exists. The cost was invisible at first and then obvious. Writing a task
*is* writing a task; it is not a decision to open a page about it. Most tasks are one line, done and
crossed off without anybody ever wanting somewhere to elaborate. But every one of them got a file,
with the template's `Oppgåver` heading and a blank line in it, sitting in the folder for ever.

Eighteen of them had accumulated by the time anybody counted — against four task pages that held
anything at all. They are hidden from the index and from the tag list, so they cost nothing to look
at; they cost something to *have*. Every one is a file in the repository, a name that can collide
with a page somebody wants to make, and a row in every scan the wiki does — search, backlinks, the
unpublished set, all of which read the folder on the request that asks
([ADR-0018](0018-search-is-a-scan.md)).

There was a second cost, subtler. Because the file existed, the editor's arrow was a plain link, and
a task's page was something that had *happened to you* rather than something you asked for. The
question "does this task need a page?" was answered by the system, always yes, before anybody had
thought about it.

## Decision

**Nothing creates a task page except somebody going to one.** `syncTasks` keeps the half that
removes an emptied page along with its task, and the half that lets a page holding real work
graduate to an ordinary page rather than be deleted. Its creation half is gone. `Store.OpenTask` is
the new and only way a working file is made, and it is called from one place:
`POST /p/{slug}/oppgåve/{node}`, which the editor asks for when the arrow is followed.

**The arrow is an offer, not a report.** Before, an arrow meant "this page exists"; a task without
one showed an inert `→` and the words *Lagre for å opprette arbeidssida*. Now the arrow is live from
the moment the task has text, and following it makes the page. A task with no text still has no
arrow — there would be no name to give the page and no job for it to be about — and that rule is
held in `OpenTask` as well as in the editor, because a rule the client enforces is not enforced.

**Opening is idempotent.** A task that already has a page gets that page back. Two clicks, a retry
after a dropped connection, or two people pressing at once cannot make a second working file for one
task. A task naming a page that has since gone gets a new one under the same task rather than an
error — the caller's question was "where do I go", and there is an answer.

**`OpenTask` writes the link back itself, outside `Save`.** It takes the store's write lock, makes
the page, sets `page` on the task and writes the parent file. It deliberately does not go through
`Save`: this is the store's own bookkeeping rather than an edit somebody made, and routing it
through the version check would refuse it whenever the editor asking for it was holding the version
it had loaded — which is always. The answer carries the new version instead, and the editor adopts
it exactly as it adopts a save's ([ADR-0021](0021-a-save-answers-for-what-it-read.md)).

## Consequences

The eighteen empty files are gone, along with the `page` field on each task that named one. Clearing
that field matters as much as deleting the file: a task pointing at a page that is not there is what
makes the editor offer a link into nothing.

**The cleanup had to follow the deploy, not lead it.** Run against the old binary, it deleted the
files and cleared the fields — and the running server, still creating eagerly, made them all again
on the next save of the parent page. That is not a race to fix; it is the old behaviour working
exactly as designed. Any similar tidying of data the running code maintains has to ship the code
first.

What is lost is the certainty that an arrow is a link to something that exists. In exchange, a task
is a line of text again, and the folder holds pages somebody meant to write.
