# ADR-0029: The panel is a tablist, not a row of buttons

- **Status:** Accepted
- **Date:** 2026-09-09
- **Supersedes:** —
- **Superseded by:** —

## Context

The right-hand panel began as one list — the contents of the document — and grew a second, the
history, reached by a `Historikk` button that filled the panel and emptied it again on a second
press. That shape carried an argument, written into [`SPEC.md`](../SPEC.md) and into the markup:

> There is no `ToC` button. The contents are what the panel shows unless something has replaced
> them, so a button to ask for the default was a button to undo the only other button.

It was right at the time, and it stopped being right the moment there was more than one other thing.
By 2026-09-09 the row under the panel's head read `Historikk | KI | Vis | Del`, and those four were
not the same kind of thing at all:

- `Historikk` **replaced** what the panel showed, and was a toggle.
- `KI` **added** a field above the contents, and was a separate toggle with its own state.
- `Vis` took over the whole screen and gave it back.
- `Del` wrote to the clipboard and popped a toast.

Four words in a row, four different behaviours, and no way to tell from looking which was which. The
contents — the thing the panel is mostly for — had no name at all, and the way back to them was to
press `Historikk` a second time, which is not a thing anybody would guess. `KI` and `Historikk`
could both be open at once, showing a field over a list, which is neither of them.

## Decision

**The row is a tablist and the panel shows one pane.** `ToC`, `Historikk`, `KI`, `Del`. Each is a
way of looking at this document; exactly one is selected; the panel below holds its pane.

**`ToC` gets a name.** The contents are a pane like the others now, not a default that other things
temporarily displace. First in the row, because it is where the panel starts.

**`Vis` leaves the row.** It is the one entry that neither changes what the panel shows nor asks
anything of the document, so under a row of tabs it would be the one that was not a tab. It moves
to the far end of the head row, mirroring the toggle at the near end, with the read/write switch
centred between them.

**`Del` shows the address instead of copying it.** A tab whose pane is a copyable field and a line
saying what the link does and how long it lasts.

**Which pane is up is `data-pane` on the panel**, one attribute, read back by anything that needs
it. The `showing-history` class is gone. A pane change fires `marksheets:pane` carrying both the
new pane and the one left, which is how the editor knows to clear the history.

## Consequences

**Nothing about the panel is remembered.** Which way you are looking at one document is not a way
you like the window laid out — unlike the two sidebars, which are, and which are stored. Every
document opens on `ToC`.

**Leaving `Historikk` empties it, and takes the version on the page with it.** That was already the
rule; what changed is where it hangs. It is on the pane event rather than on the button, so it holds
however the pane was left, including when the editor asks for a pane itself.

**`KI` takes the panel rather than sitting above the contents.** That is a real loss — a question
about the document and the shape of the document were worth having on screen together — and it is
the price of the four being one row. A pane that left another one showing would be the one that did
not behave like a tab.

**Opening `Del` mints a share token.** Asking the server for the address is what creates it, so a
document that has never been shared grows a live credential when the tab is opened. Pressing the old
button always meant that too; a tab is a cheaper press, and that is worth having written down.

**The clipboard code is gone** — the write, the toast, and the `execCommand` fallback for browsers
without the clipboard API. The toast's styling went with it rather than waiting to be found unused.

**A document with no headings needs an empty state.** While the contents were the default they could
be blank without anybody having asked for them; a tab that opens on nothing reads as broken.

## Alternatives considered

**Keep the buttons and just add `ToC`.** Rejected: it fixes the missing name and leaves the four
behaviours. `Historikk` would still be a toggle while `ToC` was not, and `KI` could still be open
over either.

**Make them a `<select>`.** Rejected. Four short words fit the column, and a menu hides three of
them behind a press — the row is also how you find out the panel can do these things at all.

**An accordion: all four stacked, each expanding in place.** Rejected: the panel is sixteen rem
wide and already scrolls on its long axis. Four collapsible sections in that column would mean the
thing you wanted was usually below the fold, and the contents list — the longest of them — would
push the rest off the bottom whenever it was open.

**Leave `Del` on the clipboard and give it no pane.** Rejected once the others became tabs: it would
be a second entry in the row that was not a tab, and `Vis` had already earned the one exemption by
not touching the panel at all. Showing the address also fixed something the copy never could —
there was no way to see what you were about to send, or to tell a shared document from an unshared
one.

**Keep the toast for `Del` anyway**, saying "address shown". Rejected as a notice that the thing you
just pressed did the thing it says it does.
