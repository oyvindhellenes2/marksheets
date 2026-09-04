# ADR-0027: A presentation is a way of reading a page, not a kind of link

- **Status:** Accepted
- **Date:** 2026-09-04
- **Supersedes:** —
- **Superseded by:** —

## Context

The presentation was built as part of the share view. The reasoning was that
showing a page to a room is what you do with a page you have handed to somebody
— you send the link, you put it on the screen, you talk through it — so the
slides lived in `del.js`, beside the code that strikes out the links leading
further into the wiki, and the button that opened them floated over the shared
article in a bar of its own.

That put a wall through the middle of a feature. Presenting a page you had not
shared meant sharing it first: press `Del`, mint a token that lasts a fortnight
and that anybody it is forwarded to can use ([ADR-0024](0024-a-share-link-is-the-credential.md)),
open the address it gave you, and present from there. A credential handed out
because somebody wanted a bigger typeface is a credential handed out for the
wrong reason, and the wrong reason is the one that gets forwarded.

The alternative considered first was to leave it where it was and let the page
link to its own share view — a `Vis` that opened `/p/{slug}/del`. That is worse
than it sounds: it is the same token, minted the same way, for a person who is
already signed in and already looking at the page.

## Decision

**The presentation is a way of reading a page, available wherever a page is.**
`Vis` sits in the panel menu next to `Les`, and the share view keeps
`Presentasjonsvisning` in its floating bar. Both open the same deck, built by
the same file.

Three things follow from that.

**One file, `present.js`, loaded by both screens.** `del.js` is gone; it was two
hundred lines, and a second copy that had to keep agreeing with the first is the
thing this codebase avoids everywhere else. What is left of the share view's own
behaviour is on the server, in `render.Shared`, which is where it belonged
anyway.

**The slides are cut afresh each time, out of the read view.** On a shared page
the article never changes and caching the deck cost nothing; in the editor it
changes as you type, and a deck built once would show the page as it was the
first time anybody pressed the button. So `Vis` in the editor saves, switches to
reading, and opens the deck on what came back — which also keeps the rule that
the slides and the article can never disagree, since there is still only one
rendering and the slides are clones of it.

It deliberately does **not** remember the mode the way `Les` does. Reading is a
preference and follows you from page to page; showing a page to a room is
something you are doing once, and leaving the presentation should not quietly
change how the next page opens.

**The way out is inside the deck.** A `×` in the corner, beside the counter. On
the share view the opening button used to stay on screen at reduced opacity and
flip its own label, which worked because that bar floats over everything; in the
editor the button is in the right-hand panel, which the deck is drawn on top of,
so the same design would have left `Escape` as the only exit. A key nobody was
told about is not an exit. One control, always in the same corner, on both
screens.

## Consequences

`Vis` reaches the read view, which means it reaches `save`. Pressing it with
unsaved work saves that work, exactly as `Les` does — presenting is not a
read-only act, and a page that presented stale text would be worse.

The backlinks had to be excluded from the partition. They are a top-level
section of an ordinary page's read view and would have been swept onto whatever
slide came last; the share view never had to think about it, because a shared
page is drawn without them.

The share view's `ToC` button went at the same time, though for its own reason:
a bare page draws the same right-hand panel every other screen does, so the
button asked for a list already standing open beside it.

What this does not do is give the presentation anything the read view has not
got — presenter notes, a timer, a second screen. Those are real things a
presentation eventually wants, and each of them is a second structure to keep in
step with the page, which is the thing
[the heading-is-a-slide rule](../SPEC.md) exists to avoid. If they are ever
wanted, that is the trade to argue then.
