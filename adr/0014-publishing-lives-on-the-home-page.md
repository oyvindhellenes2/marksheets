# ADR-0014: Publishing lives on the home page

- **Status:** Accepted
- **Date:** 2026-09-02
- **Supersedes:** the per-page `Publiser` button and `⌘S` in the editor, from
  [ADR-0007](0007-saving-and-publishing-are-separate.md)
- **Superseded by:** —

## Context

`Publiser` sat in the editor, on the page you were reading, and `POST /p/{slug}/publiser` committed
that page and pushed. It looked like publishing one page.

It was not, and could not be. `Repo.Commit` stages only the paths it is given, so the *commit* really
was that page — but the push that follows sends the whole branch, and git offers no way to send part
of one. That is not an implementation gap; it is the limit that gave the pages a repository of their
own in the first place ([ADR-0006](0006-pages-in-their-own-repository.md)), after publishing a page
was found to push unrelated code commits with it.

So a button on one page was making a promise the app could not keep. In practice the gap was small —
publishing always pushed everything committed, and everything committed was usually already pushed —
but "usually" is the wrong word for what a button says it does.

## Decision

`Publiser` moves to the home page, where publishing everything is what it plainly is. `⌘S` moves
with it.

`POST /publiser` walks every page that differs from what has been published, **commits each on its
own**, with its own message and its own place in that page's history, and then pushes the branch
once. The per-page endpoint is gone.

The editor keeps the state it always had — `Lagra`, `Lagra · ikkje publisert` — so you can still see
where a page stands while you are writing it. It simply has no button that claims to change that on
its own.

## Consequences

The per-page history is unchanged, which was the part worth keeping. `git log kafeen.json` still
reads as the story of that page, one commit per publish, because the commits are still scoped even
though the push is not.

Publishing is now a trip to the front page. That is a real cost in ergonomics and the honest price of
the button telling the truth — and the front page is where you can see what is unpublished anyway,
since every changed page is already marked there.

`⌘S` in the editor is gone. Nothing else claimed the key, so an old habit now does nothing rather
than something surprising.

`Repo.Unpublished` had to start returning paths relative to the page folder instead of
`filepath.Base`. With attachments in `filer/`, `Base` made `filer/skisse.png` look like a page called
`skisse.png`, which put a phantom entry in the unpublished set. Only a top-level `.json` marks a page
now; an attachment travels with the page that shows it.

## Alternatives considered

**Keep the per-page button and make the push page-scoped.** Rejected because it is not possible.
There is no partial push in git, and there is no arrangement of one repository that gives one.

**A repository per page**, which would make a per-page push real. Rejected without much thought: it
turns a folder of notes into dozens of repositories, and the whole point of ADR-0006 was to have
exactly two.

**Keep the button where it is and reword it** — "Publiser alt", say, in the editor. Rejected: it
would still be a global action reached from a page, which invites the same wrong assumption a moment
later. Where a button lives is part of what it says.

**Keep both**: per-page in the editor, everything on the home page. Rejected as the worst of the
three — two buttons that do the same thing, one of them still misleading about scope.
