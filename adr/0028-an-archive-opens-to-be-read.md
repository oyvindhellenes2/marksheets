# ADR-0028: An archive opens to be read

- **Status:** Accepted
- **Date:** 2026-09-09
- **Supersedes:** —
- **Superseded by:** —

## Context

Three separate defaults decided what somebody with nothing in `localStorage` met on their first
visit, and all three had been arrived at rather than chosen.

**The editor.** A document arrives as the editor — that is what the server sends, and the read view
is fetched afterwards. So `wasReading()` answered `localStorage.getItem(MODE_KEY) === 'les'`, and a
person who had never pressed anything got the editor: contenteditable rows, a gutter handle on every
line, a footer of keyboard shortcuts. Nothing about that was decided. It is what the implementation
does when nobody has said otherwise, and it showed.

It matters more since the rename ([2026-09-08](../SPEC.md)). The thing is an *archive* now, and the
document a new person lands on at their first sign-in is *Velkommen til ditt nye arkiv*, which is an
essay about what an archive is for. Meeting an essay in an editor is meeting the machinery instead
of the writing.

**The contents panel.** With nothing stored it started shut under 75rem, on the reasoning that three
columns want the room and somebody on a small laptop should meet the page rather than a squeezed
one. The column sacrificed to that was the panel.

**The index.** It had no width rule at all — open at every width above 62rem.

So the default arrangement between 62rem and 75rem was: the index standing, the panel gone, the
document in the editor. Every one of the three is the opposite of what a person who has come to read
something wants.

## Decision

**With nothing stored, a document opens in the read view.** Only the literal `skriv` — written by
the mode toggle or by `⌘⏎`, pressed once — opens the editor. A thrown `getItem` is private mode,
where nothing was ever stored, so it answers the same as no value at all.

**With nothing stored, both sidebars stand.** The index and the panel are columns at every width
above 62rem, which is where [ADR-0022](0022-narrow-is-two-views.md) takes over and the three views
begin taking turns.

**Under 75rem the index is the column that gives way, not the panel.** Three columns still want the
room; what changes is which one is asked for it. The index answers *which document*, a question you
ask between documents. The panel answers *where in this one*, and it now carries the read/write
switch and the document's own menu — `Historikk`, `KI`, `Vis`, `Del`. Beside a document you are
reading, the panel is the one that is about the document.

**A stored opinion still beats all of it** at every width above 62rem, read in `<head>` before the
first paint exactly as before. Nothing here writes a preference; these are only what is used when
there is none.

## Consequences

The width rule moved from one column to the other, so the two implementations of it moved too: the
`<head>` script and `chrome.js` each decide these classes, and both had to change. They are now
covered by a throwaway harness that extracts both verbatim and asks them the same twelve questions,
because a rule changed in one and not the other would show up as a sidebar that shuts itself when
the window is nudged — which is not something anybody would think to test by hand.

Crossing 75rem now has to be *watched*. It never was: the panel's default was read once at load and
the breakpoint had no listener, so dragging a window across it did nothing until the next page load.
`chrome.js` now listens on both queries and runs the same pass, with `remember` false throughout —
dragging a window is not a preference, and writing one down there would overwrite the opinion the
person actually holds.

**Every document load now fetches the read view.** It is one request per page for everybody who has
not chosen to write, where before it was one for the few who had. That is the cost of the decision
rather than a side effect of it: the read view is rendered server-side and there is no other way to
have it.

**A document nobody has written in yet opens in the editor**, not the read view. The read view of a
brand new document is a blank screen with a title over it, and whoever has just typed a name into
the form and pressed `Lag dokument` is plainly here to write. This was left out of the first draft
of this record on the reasoning that a default with an exception is harder to hold in your head than
the one press that fixes it; that was wrong about which of the two is the exception. Reading is the
default for a document that *says* something, and one that says nothing yet is not a document you
can be reading.

The question asked is of the document, not of where you came from. A marker on the URL would cover
only the one route through the create form, and would then sit in the address bar, get bookmarked,
and travel with a shared link. Asking whether anything is written covers a working file made on the
way to a task as well, and gives the right answer for a document somebody has emptied — which is
also a document you are plainly writing in.

The pinned `Oppgåver` heading does not count as writing: the server pins it on every load, so it is
there before anybody has typed a word ([ADR-0008](0008-the-tasks-heading-is-furniture.md)). Neither
does the creator's own name on the first task — `owner` is a `user` field, and `isBlank` already
ignores those, which is what keeps a line with a person on it and nothing else from counting as
content.

Between 62rem and 75rem a screen with no headings and no menu — a search, a profile, the type list —
now has neither column: the index by the new rule, the panel by `toc-none`. The rail keeps both
buttons, so it is a keystroke back rather than a dead end.

## Alternatives considered

**Leave the mode default alone and put a bigger `Les` in the chrome.** This was the shape of the
original request — the button was hard to find, so make it findable. Rejected as only half the
problem: a control you can find is still a control you have to press before the archive looks like
one. The button did get bigger and move (it is the centred pill in the panel's head now), but that
is the smaller half of this.

**Remember the mode per document rather than per browser.** Rejected, and it was rejected once
already when the preference was introduced: somebody going through the archive to read it would have
to press `Les` again on every document. It also makes the default question worse rather than better,
since every document would ask it afresh.

**Default to reading only on the welcome document.** Rejected: it is the one document where a new
person is guaranteed to be reading, so a special case there would be least useful exactly where the
general rule costs least. It would also leave the second document they open in an editor, which is
the surprise moved rather than removed.

**Mark the new document in its URL** — `?ny=1` off the create handler's redirect — rather than
asking the document whether it is empty. Rejected: it answers a different question. It knows how you
arrived, not what is in front of you, so it misses every other route to an empty document and it
sticks around afterwards, in the address bar and in anything anybody copies out of it.

**Let the panel be the column that yields under 75rem, and open the index by default instead.** This
is what the code did and it is worth saying why it was wrong rather than merely old. It reads the
sidebars as *index first, page second, panel last*, which is the order they were built in. On a
document that order is backwards: the panel holds the controls for the thing on screen, and the
index holds a list you are not currently using.

**Drop one of the two width rules and let 62rem do all the work.** Rejected: below 62rem the three
views take turns, which is a different behaviour and not a smaller one. Between the two widths there
is genuinely room for two columns and not three, and that is worth a rule of its own.
