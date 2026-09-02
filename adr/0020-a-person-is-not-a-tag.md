# ADR-0020: A person is not a tag

- **Status:** Accepted
- **Date:** 2026-09-02
- **Supersedes:** the `/ansvarleg` pages from [ADR-0017](0017-a-person-is-a-way-in.md), and the
  `tag`-kind `owner` field they were built on
- **Superseded by:** —

## Context

[ADR-0017](0017-a-person-is-a-way-in.md) gave people a way in, and did it with what was there: the
owner of a task was a field of kind `tag`, so it was written `#øyvind`, matched by `[#øyvind]`, and
listed on `/ansvarleg/øyvind`. That was the right shape for an app with one user, where "who" is a
word somebody types.

It stops being right the moment there is a second person, for three reasons:

- **A name was a word.** Anything could be typed into it. `#øyvnid` was a person nobody could see,
  on a task nobody would find.
- **A person read as a subject.** `#hytta` is what a page is about; `#øyvind` was who a task is for.
  Two different kinds of thing, written identically, sitting inches apart on the same line.
- **`/ansvarleg/øyvind` was a list, not a person.** With authentication there is somebody to *be*,
  and a page about them is the natural place for what is theirs.

Meanwhile the app needed authentication anyway — Pocket ID, already running at
tilgang.verftet.info.

## Decision

**A `user` field kind.** `owner` on `task` and `todo` is kind `user`, not `tag`. It is drawn without
a `#`, in the accent rather than the tag's flat box, and it is a **menu rather than a field**: you
pick from the people there are, and a name that is not one of them cannot be typed. The hardcoded
`"default": "øyvind"` is gone; a task made by somebody is theirs until they say otherwise.

**`/<namn>` is that person's page.** `/kari` is Kari: who she is, and everything with her name on it,
grouped by the page it was written on. `/ansvarleg` is gone. The address is the name, the way a
page's address is its title.

**`[@namn]` in a query.** `@side/oppgåver[@kari]` matches `user` fields, and `[#tag]` no longer does
— a person and a subject are asked for differently because they are different questions.
`[owner=kari]` still works, as it always did for any field.

**Sign-in is OIDC against Pocket ID**, written against the protocol: discovery, the authorization
code flow with PKCE, and one call to `userinfo`. The ID token is deliberately *not* verified
locally, which is what would otherwise drag JWKS and RS256 into a module with no dependencies
([ADR-0002](0002-no-dependencies.md)) — the access token is exchanged over TLS with the provider and
then spent at the provider, so nothing here is trusted on its own signature.

**Unconfigured, the app runs as one local user**, exactly as it did before any of this. Not a back
door: with an issuer set, every request is checked and there is no local user to fall back to. It is
what keeps the app runnable — and testable — without an identity provider standing behind it.

**People are remembered in a file beside the pages, never in them.** The page folder is a git
repository with a public remote, and an email address is not something to publish by accident; a
`.json` file in there would also be counted as a page.

## Consequences

An owner is now a real person or nothing, which is what makes `/kari` trustworthy: the list is
everything with her name on it, and there is no way to have meant her and missed.

**Names written before this stay put.** A page saying `owner: "øyvind"` when no such login exists
still says it: the picker offers the value back as "øyvind (ukjend)" rather than dropping it, and
`/øyvind` still renders — as a person who has not logged in. That is the same repair-never-discard
rule the rest of the app follows. What it means in practice is that after the first real sign-in the
old names may need re-picking once, and until then those tasks are on nobody's profile.

Sessions live in memory, so a restart signs everybody out. For a handful of people that is a login,
not an outage, and it keeps sessions from being a second thing to persist, expire and clean up.

Publishing now records who did it: `Repo.Commit` takes an author, so `git log` in the pages
repository answers "who wrote this" instead of crediting the machine.

## Alternatives considered

**Keep `tag` and let a name be free text.** Rejected: it is the reason the feature could not be
trusted. A list of "everything that is mine" is worth nothing if a typo silently removes a task
from it.

**Use an OIDC library.** Rejected: `go.mod` has no requirements and this is the short version of the
protocol — about two hundred lines. The one piece a library would really buy is local ID-token
verification, and calling `userinfo` removes the need for it.

**Verify the ID token locally anyway** (JWKS, RS256, clock skew). Rejected for now: it saves one
request per sign-in and costs a signature implementation nobody here wants to own. If sign-ins ever
become frequent enough for that request to matter, this is the thing to change.

**Persist sessions.** Rejected: it is a second store to keep honest for the sake of not typing a
password after a deploy.

**Ask Pocket ID for the list of users** instead of learning them as they log in. Rejected: it needs
admin credentials in this app's configuration, which is a much bigger thing to hold than a list of
names — and somebody who has never signed in has nothing to be given anyway.
