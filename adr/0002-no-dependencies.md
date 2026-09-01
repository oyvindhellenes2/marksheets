# ADR-0002: The module has no dependencies

- **Status:** Accepted
- **Date:** 2026-08-24
- **Superseded by:** —

## Context

`../mystuff/STACK.md` is the house stack: Go stdlib HTTP, `embed.FS`, HTMX, vanilla CSS — plus
SQLite for storage and `github.com/go-webauthn/webauthn` for passkeys. Marksheets needed neither of
the last two on the first day: storage went to files (ADR-0001), and there is one user and no login.

## Decision

`go.mod` lists no requirements. Everything the server does comes from the standard library, and git
is driven by shelling out to the `git` binary rather than through a library (ADR-0004).

## Consequences

The app builds wherever Go builds, with nothing to fetch. Upgrades are Go upgrades. Nothing breaks
because something upstream changed underneath it.

Against that, whatever the standard library does not do gets written here: the `@`-query resolver,
the whole editor, the inline markdown pass. Each of those is a few hundred lines that a dependency
would have supplied, and each is now ours to maintain and to get wrong.

One qualification worth stating plainly: **HTMX is loaded from unpkg at page load.** That is not a
build dependency, but it is a runtime one, and the read view and home page stop working without it.
Vendoring it into `static/` would close the gap and cost nothing.

## What would reopen this

Authentication. Passkeys through `github.com/go-webauthn/webauthn` would be the first dependency this
project has ever taken, and hand-rolling WebAuthn is not a reasonable thing to do. If multi-user
happens, this decision should be revisited on purpose rather than drift — and note that it is only
the *auth library* under question. User records themselves can stay files, consistent with ADR-0001.

## Alternatives considered

**Vendoring source into the tree.** Rejected as the worst of both: a dependency, without the tooling
that makes dependencies manageable.
