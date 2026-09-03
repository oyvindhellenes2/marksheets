# ADR-0023: Sessions outlive the process

- **Status:** Accepted
- **Date:** 2026-09-03
- **Supersedes:** the storage half of [ADR-0017](0017-a-person-is-a-way-in.md) — signing in is
  unchanged; where the resulting session lives is not
- **Superseded by:** —

## Context

Sessions were a map in memory, and the reasoning was written down beside them: a restart signs
everybody out, and for a handful of people that is a login rather than an outage. It also kept the
session store from being a second thing to persist and expire, next to the user list.

That was true while restarts were rare. It stopped being true the day the wiki was set up: the
service restarted nine times in one afternoon and once more the next morning, every one of them a
deploy, and each signed out everyone who was reading. The lifetime was already thirty days — nobody
was being timed out. They were being interrupted by a binary being replaced.

The question that surfaced it was whether this was Pocket ID configuration. It was not, and that
distinction is the useful part: two lifetimes sit in series here, and only the second is the
provider's.

- **This app's session** decides whether you are signed in at all. Thirty days, and until now
  thirty days of *uptime*.
- **Pocket ID's own session** decides what the sign-in screen costs when you land on it — a click
  and a redirect that comes straight back, or a real passkey prompt.

Raising Pocket ID's duration would only have made the interruption cheaper. It could not stop the
interruption, because the thing being lost was on this side.

## Decision

**The session store is written to a file and read back at boot.** `SESSIONS_PATH`, defaulting to
`sesjonar.json` beside the page folder — the same place, and for the same reason, as the user list:
outside `PAGES_DIR`, so it can never be committed to a repository with a remote, and never counted
as a page.

**A session is filed under the SHA-256 of its token, never the token.** The cookie carries the real
one. The file is then a list of expiries and names rather than a ring of working keys: reading it
tells you who is signed in and until when, and lends you nobody's session. It costs one hash per
request against a map lookup, which is nothing.

**Written on sign-in and on sign-out**, whole, through a temp file and a rename, `0600`. Signing out
has to outlive the process too, or a restart would hand the session back to whoever still held the
cookie.

**A file that will not load is not fatal.** Missing is the ordinary first boot. Unreadable or
unparseable is logged and stepped over, and everybody signs in again — which is exactly the state
this replaced, so the failure mode of the fix is the behaviour it fixed. Refusing to start a wiki
because a cache of logins would not parse would be the wrong trade in every direction.

**Expired entries are dropped on load and on every write**, which is now the only thing pruning
them. A restart used to do it for free.

## Consequences

A deploy is no longer a login. Thirty days means thirty days of wall clock.

**Signing out and expiry are now the only ways a session ends.** There is no longer a restart
quietly clearing the decks, so a token that leaks is good until one of those two, and `sesjonar.json`
is a file worth the `0600` it is written with. This is the real cost of the change and it is
accepted deliberately: the alternative was interrupting everybody, constantly, for a security
property nobody was relying on.

The file is one more thing beside the pages that is not the pages — `brukarar.json`, and now this.
Both are the app's own state about people, neither belongs in the page folder, and the rule for
where such a thing goes is now written once in `beside()` rather than twice.

## Alternatives considered

**Raise Pocket ID's session duration and leave this alone.** Rejected as an answer to the question
asked, though still worth doing on its own merits. It makes the re-login silent; it does not stop
the sign-in screen appearing after every deploy, because what is lost is this app's session and not
the provider's.

**Deploy less often.** True, and not a mechanism. The deploy rate was high because the wiki was new;
it will fall on its own. But "do not touch it" is not a property of the system, and the next busy
week would bring the problem back unchanged.

**Signed cookies with no server store at all** — put the user and expiry in the cookie, sign it, keep
nothing. Genuinely stateless, survives restarts by construction, and rejected because it takes away
sign-out: a signed cookie is valid until it expires, and revoking one means keeping a list of the
revoked, which is a server store with the logic inverted and the awkward half kept.

**Store raw tokens.** Rejected for the obvious reason and one less obvious: the file is the sort of
thing that gets copied into a backup, a paste, or a bug report, and a hash makes all three harmless.
