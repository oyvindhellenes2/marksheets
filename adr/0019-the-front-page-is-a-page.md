# ADR-0019: The front page is a page

- **Status:** Accepted
- **Date:** 2026-09-02
- **Supersedes:** the *placement* half of [ADR-0014](0014-publishing-lives-on-the-home-page.md) —
  publishing moves from the home page to the sidebar, and the home page itself is gone
- **Superseded by:** —

## Context

The index of pages became a sidebar, drawn beside every page. That left the screen at `/` doing the
same job a second time, an inch to the right, and the first thing it lost was its reason to exist:
its tag row and its "new page" form were the sidebar's two controls drawn twice, so they went, and
what remained was a list of the same pages with a line count on each.

A list you always arrive at and never stay on is a stop on the way to somewhere. Every visit to it
ended in a click on a page.

Two things still lived there, though, and neither was about listing pages:

- **`Publiser`**, put there by [ADR-0014](0014-publishing-lives-on-the-home-page.md) because a push
  sends the whole branch and a button on one page could not honestly say otherwise. The reasoning
  was about *scope*, and the home page was where global scope happened to be true.
- **`Slett`**, on each card — the only place a page could be deleted.

## Decision

**`/` opens the most recently edited page.** No index screen. A folder with nothing openable in it
gets a short empty page instead, which is the one screen left where the list used to be.

**`Publiser` moves into the sidebar**, between the tags and the pages. That is the same argument
ADR-0014 made, not a reversal of it: publishing everything belongs where "everything" is true, and
the sidebar is chrome rather than a page. It is now reachable from anywhere, which the home page
never was.

**`⌘S` stays unbound while the editor is on screen.** With the button global the shortcut could be
too, and that is exactly where it should not be: in the editor, hands press `⌘S` meaning "save what
I typed", which happened on a timer a second ago. Making that push to everybody would be the most
expensive misunderstanding in the app. Outside the editor it works.

**`Slett` moves into the editor bar**, on the page it deletes, and only on pages that are not
working files — a working file belongs to its task and goes when the task does. Deleting redirects
to `/`, which finds the next page to open.

**The people index gets a door of its own**, `/ansvarleg`, linked from the footer. It was reachable
only from the home page, and would otherwise have gone with it.

## Consequences

There is now no screen that lists pages with how long they are and when they changed. Nobody used
it for that; if that ever turns out to be wrong, the answer is a page that says it — not the index
coming back.

`?emne=` on the front page no longer filters anything, because there is no list there to filter.
Tags filter the sidebar, in place, beside the list they narrow.

Four templates and a script went with it: `home.html`, `page-list-partial.html`, its out-of-band
twin, and `home.js`, whose one job — publishing — moved into `chrome.js` where the button now lives.
`DELETE /pages/{slug}` answers with a redirect rather than a list fragment, since there is no list
to hand back.

The offer to `git init` in a folder that is not yet a repository moved into the sidebar too, in the
place the publish button takes once there is one. It was in the home page's markup and would
otherwise have been quietly lost — the endpoint was still there, with nothing left to call it.

## Alternatives considered

**Keep the index page as well.** Rejected: that was the state this replaced, and the duplication was
visible on the screen — two tag rows, two forms, two lists of the same pages.

**Make `/` a dashboard: what changed, what is unpublished, who owes what.** Tempting, and it is the
option that was offered first. Rejected for now because the sidebar already marks unpublished pages
with a dot and lists them newest first, so a dashboard would mostly restate the furniture. The
newest page is what somebody opening the wiki actually wants to see.

**Leave `Slett` off entirely.** Rejected as data loss by omission: the only way to remove a page
would have been deleting the file by hand, and the app knows things the filesystem does not —
a page's working files go with it.

**Put `Slett` in the sidebar, on each row.** Rejected: a destructive control on every row of a list
you click through all day, next to the thing you actually want to press. On the page itself it is
one screen further from an accident and reads as being about the page you are looking at.
