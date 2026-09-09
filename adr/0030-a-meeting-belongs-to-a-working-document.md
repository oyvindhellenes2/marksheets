# ADR-0030: A meeting belongs to a working document

- **Status:** Accepted
- **Date:** 2026-09-09
- **Supersedes:** —
- **Superseded by:** —

## Context

Fjernmøte (`fjernmote.verftet.info`, `github.com/oyvindhellenes2/fjernmote`) is a new app for
video meetings. Its brief says a meeting URL "is made by making a working document for a task, and
shows when you press Del, under the sharing URL. The meeting is in that way tied to a concrete
task."

So this archive has to do two things it has never done: offer an address that belongs to another
app, and accept a write from a machine that has no session.

## Decision

**A meeting's address is derived from the document's slug**, and nothing about a meeting is stored
here. `meetingURL` is `FJERNMOTE_URL + "/m/" + slug`, and it is empty for a document with no
parent and empty everywhere when `FJERNMOTE_URL` is unset — which is what the private archive at
`arkiv.hellenes.it` does.

**It is shown in the Del-panel**, under the share link, on working documents only. Not on a button
of its own: both answer "how do I get somebody else to this piece of work", and they belong next
to each other. The note beside it says the meeting address does not expire, because the address
above it does, and two addresses in one box that behave differently is where somebody gets it
wrong.

**Fjernmøte reaches this app through two endpoints and one shared token**, `FJERNMOTE_TOKEN`:
`GET /api/fjernmote/{slug}` says what a document is called and whether it is a working document,
and `POST /api/fjernmote/{slug}/mote` writes a finished meeting onto it. Unset, both are off and
neither exists.

**The token is checked inside `publicRequest`**, which is the one hook `auth.Middleware` asks
([ADR-0024](0024-a-share-link-is-the-credential.md)).

## Consequences

That last point is the one to defend, because it looks like the thing ADR-0024 forbids. It is not.
The rule there is that there must be **one function** saying what may pass without a session, so
there is one place to read and one place to change; what was forbidden was adding a path to the
middleware's own prefix list, which is the same decision made where it cannot be justified. A
token-bearing request is not public in the sense a share link is — a share link is a credential
somebody chose to hand out, and this is a service on the same machine proving it holds a secret
nobody has typed. It belongs in the same function, with the reasoning attached, and `fjernmote.go`
carries it.

The comparison is `crypto/subtle`, not `==`.

**Nothing about a meeting is kept here.** There is no room id, no expiry, no list. Pressing Del
twice gives the same meeting address, and so does pressing it next year — which is the opposite of
the share link above it and is the reason it needs a sentence of its own.

**A meeting write appends and never rewrites**, under a heading of its own at the end of the
document, and is idempotent: the meeting's id is written into a comment on the page, and a second
delivery of the same one is answered with where the first went rather than with an error. The
check lives here rather than in Fjernmøte because this document is the thing that would end up
with two referats on it, and because a retry after a lost reply is the ordinary case.

It saves with **no version**, the way the restore path does — it is not answering for anything it
read ([ADR-0021](0021-a-save-answers-for-what-it-read.md)) — and with **no author**, because
nobody saved it.

**The transcript arrives as text and becomes an attachment**; the audio never arrives at all. The
page folder is a repository that is pushed and cloned whole, and a blob is kept for ever. An hour
of meeting audio in there would be carried by every clone from then on, so what lands on the page
is an address in the other app, behind the same sign-in.

The other half of this boundary is Fjernmøte's own
[ADR-0009](https://github.com/oyvindhellenes2/fjernmote/blob/main/adr/0009-the-archive-builds-the-lines.md):
it sends facts, this app builds the lines. When a line type changes here, nothing over there has
to know.
