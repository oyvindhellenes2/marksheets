# ADR-0006: The pages live in a repository of their own

- **Status:** Accepted
- **Date:** 2026-08-29
- **Supersedes:** the `git subtree add --prefix=pages` import that merged them
- **Superseded by:** —

## Context

`pages/` used to be its own repository. Its history was brought into the project repository with
`git subtree add --prefix=pages`, and `pages/` became an ordinary directory — one repository holding
both the code and the notes, published at `github.com/oyvindhellenes2/marksheets`.

Publishing a page commits only `pages/*.json`; the staging guard in ADR-0004 sees to that. But a
push sends the whole branch. Tested before deciding: with an unpushed `WIP` commit touching
`internal/vcs/git.go` in the repository, publishing an unrelated page put that commit and its
contents on the remote. Uncommitted work stayed local, but anything already committed travelled.

## Decision

`pages/` becomes a git repository of its own, **private**, and is ignored by the project repository.
The code repository stays public.

No code changed. `vcs.Open` runs `rev-parse --show-toplevel` from `PAGES_DIR`, so git walks up and
finds the nearest `.git` — which is now the pages one.

## Consequences

Publishing a page can no longer push source, and the notes can be private while the code is public —
which they now are. Verified: an unauthenticated request for the pages repository returns 404, the
code repository returns 200.

A fresh clone of the project has no pages at all, which is why `examples/` exists: an invented page
set that shows what the app does without shipping anyone's notes.

Two repositories to back up now, and `git status` in the project no longer shows page edits. That is
the point, but it is a habit change.

Multiple users would not change this decision; it reinforces it. It would only raise the question of
whether there is one pages repository or one per user, which is a separate decision.

## Alternatives considered

**Gitignore `pages/` without a second repository.** Tested and rejected: `git add` refuses ignored
paths, so publishing fails outright — *"The following paths are ignored by one of your .gitignore
files"* — while saving carries on. That leaves autosave with no history at all.

**Move the pages outside the project tree entirely**, using `PAGES_DIR`. Conceptually the cleanest —
the data would not be in the code tree in any sense. Rejected because it means setting an environment
variable on every run and rewriting every reference to `pages/` in the docs, for the same separation
the nested repository already gives.
