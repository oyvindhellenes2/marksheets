# ADR-0004: Git is the historian, never the database

- **Status:** Accepted
- **Date:** 2026-08-24
- **Amended by:** [ADR-0007](0007-saving-and-publishing-are-separate.md), which added the third layer
- **Superseded by:** —

## Context

The pages are files (ADR-0001), and files in a folder are what git is for. The question was how much
of the app's correctness to hang on it.

## Decision

The app shells out to the `git` binary, and the layers are ordered so that each one is safe before
the next is attempted:

1. The file is written — temp file, then rename — and **safe before a commit is attempted**.
2. The commit is made and **safe before a push is attempted**.
3. A failure at any layer is reported, and never becomes a failure of the layer beneath it.

Only paths under `PAGES_DIR` are ever staged; never `git add -A`. The app must not be able to commit
its own source. Commits are serialised, because git's index takes one writer at a time, and fall back
to an identity of their own when git has none configured rather than failing. History is optional: if
`PAGES_DIR` is not inside a repository the app runs without it and offers to start one.

The third layer arrived on 2026-08-29, when saving and publishing became separate acts.

## Consequences

A page is never lost to a git problem. The log is readable and revertable, and because a rename
cascade is written as one commit across every file it touched, reverting it undoes the heading and
every link to it together.

Two consequences bite:

**A commit can be limited to `PAGES_DIR`; a push cannot be limited at all.** `git push` sends the
whole branch, and git offers no way to send part of one. While code and pages shared a repository,
publishing a page therefore published every unpushed code commit. That asymmetry is the whole reason
for ADR-0006.

**Two writers collide.** The second publisher gets `! [rejected] ... fetch first`. It fails safely —
the commit stands and the page stays marked unpublished — but there is no fetch or rebase anywhere in
the app, so there is no way forward from inside it. Multiple users will have to solve this, and the
"editor owns the tree, saves are a full replace" design has no answer for a genuine conflict on one
page.

## Alternatives considered

**A git library.** Rejected: it would have been the first dependency (ADR-0002), for something the
binary already does well and that is trivially inspectable when it goes wrong.

**Keeping history inside the files.** Rejected: that is a worse version of the database git already
is, and it would put presentation of the past into the data.
