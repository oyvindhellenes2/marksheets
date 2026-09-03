# ADR-0024: A share link is the credential

- **Status:** Accepted
- **Date:** 2026-09-03
- **Supersedes:** the "nothing reaches a signed-out request" half of
  [ADR-0017](0017-a-person-is-a-way-in.md) — signing in is unchanged and is still the only way to
  reach the wiki; one page at a time can now be handed out without it
- **Superseded by:** —

## Context

Until now every route but the sign-in screen and the stylesheet required a session, and that was
written down as a property worth keeping: nothing about the pages reaches a signed-out request, and
`Server.nav` does not even *assemble* the index for one, so a later template edit cannot leak it.

Sharing was built one step earlier as a view rather than a permission — `/p/{slug}/del`, behind the
same login, useful for presenting your own page and for sending a colleague a clean address. The
next step was the one that changes the security model: a link that opens for somebody with no
account at all.

That is a real change and not a settings toggle. It needs an address nobody can guess, somewhere to
keep it, a way to take it back, and — the part easy to get wrong — the page's links dead on the
*server*, because "disabled in the browser" is not disabled for a reader who has no script.

## Decision

**The token is the credential.** 32 bytes of `crypto/rand` in URL-safe base64, one per page. Anybody
the link reaches may read that page; there is no second check, because there is nobody to check
against. Everything else follows from saying that plainly rather than half-implying it.

**One token per page, minted once.** Pressing `Del` again returns the same address. A page that grew
a new link on every press would be a page nobody could un-share, because nobody would know how many
were out there.

**Kept in `SHARES_PATH`** — `deling.json`, beside the pages and never inside them. The user list and
the sessions live there for good reasons; this one has a sharper one, since a token committed to the
page repository would be a way in published to the remote.

**Written down, like the sessions.** A link that stopped working when the service restarted would be
worse than no link: somebody would have sent it to a room an hour earlier. `For` refuses to hand back
a token it could not persist, rather than returning one a restart would forget.

**Revocable, and revoked by deletion.** `DELETE /p/{slug}/del` drops it; the address stops working at
once and the next press mints a *different* one. Deleting a page forgets its link too, so a token
cannot outlive what it pointed at and come back pointing at whatever is written under that slug next.

**What a stranger may read is one function.** `auth.Middleware` gained a single hook, `Auth.Open`,
and `Server.publicRequest` is what it asks. Two things are open: a share address, and an attachment
shown on a page that is shared right now. The second is worked out by reading those pages rather
than from a stored list, because the answer changes when somebody edits a page or revokes a link,
and a stored one would be wrong in the direction that matters.

**Links out of a shared page are dead on the server.** `render.Shared` draws `ms-link`,
`ms-tx-source`, `ms-owner` and `ms-task-open` as text instead of anchors. The words stay — they are
part of a sentence somebody wrote — but there is no href. The signed-in preview renders the same
way, or it would be a preview of something else.

## Consequences

**A transclusion is shared with the page it is on.** `@ei-anna-side/bolk` renders into this page, so
sharing this page publishes that section of the other one. That is what transclusion is, and no
amount of care here changes it. It is documented rather than prevented, because preventing it would
mean a shared page that silently says less than the page it claims to be.

**There is no expiry.** A link lasts until it is revoked. Adding a date is easy and was left out
deliberately: an expiry nobody set is a link that dies in the middle of a meeting, and one everybody
sets to "never" is a field that taught people to ignore it.

**There is no UI for revoking yet.** The endpoint exists and `deling.json` is readable by hand. This
is the obvious next thing and is called out rather than pretended away: shipping a public link with
no visible off-switch is the part of this change most worth finishing.

A stranger reaching a share address gets the page, the contents list, and the presentation. They get
no header, no index, no search, no footer, no theme button, and no way to any other page.

## Alternatives considered

**Leave it behind the login.** What the previous step did, and it is genuinely enough for sharing
inside the team. Rejected because it does not answer the thing actually asked: sending a page to
somebody who has no account here.

**A capability in the URL fragment** (`/delt#token`), so the token never reaches the server logs.
Rejected: it cannot be checked before the page is served, so the page would have to be fetched by
script after load, which breaks reading with no JavaScript and makes the whole view depend on it.

**Sign the token instead of storing it**, so there is nothing to keep. Rejected for the same reason
signed session cookies were: it takes away revocation, and a public link you cannot take back is the
one property this must not have.

**Publish to a static file** somewhere outside the app. Rejected: it makes a second copy of the page
that goes stale the moment the page is edited, and the whole design here computes answers rather
than storing them.
