# ADR-0022: Narrow is two views, not two things stacked

- **Status:** Accepted
- **Date:** 2026-09-02
- **Supersedes:** the narrow-window half of [ADR-0019](0019-the-front-page-is-a-page.md) — the
  sidebar is still the index, but below 62rem it is no longer drawn beside or above the page
- **Superseded by:** —

## Context

[ADR-0019](0019-the-front-page-is-a-page.md) removed the index screen and made the sidebar the only
list of pages. On a wide window that is a column against the left edge with the page beside it, and
it works: both are legible at once and neither is in the other's way.

Below 62rem there is no room for two columns, and what the stylesheet did instead was turn the
sidebar into a band above the page, capped at `max-height: 16rem`. The comment beside that rule
said an overlay "would need a second piece of state for the same one bit", and avoiding a second
bit was the whole argument for the band.

On a phone the band was wrong in two ways, and only one of them is about taste:

- **Both halves were half-shown.** Sixteen rem of index and whatever was left of a short screen for
  the page, the two scrolling against each other. Neither had enough room to be read.
- **The index sat above the header.** `<aside>` came before `<div class="page">` in the markup, and
  in the flow that put it above the very `☰` meant to control it. To reach the toggle you scrolled
  *past* the thing the toggle hides. The band was not merely cramped, it was in front of its own
  switch.

## Decision

**Below 62rem the index and the page take turns, and `☰` switches between them.** The header stays
where it is; only what is under it changes.

**The header comes first in the markup.** `<aside class="sidebar">` moves inside `.page`, after
`<header>`. On a wide window the sidebar is `position: fixed` and its DOM position never showed; in
the narrow flow this is exactly what puts it below the bar holding the toggle instead of above it.
The fix is markup order, not a `top` offset measured in script.

**It stays one bit of state**, read in `<head>` before the first paint as it always was. What
changes is what the bit means at each width:

- **Wide** — a preference, remembered in `localStorage`.
- **Narrow** — a place you are. It starts shut on every load and is never written down. Opening the
  index on a phone must not follow you back to the desk, and more importantly a remembered "open"
  would land you on the index every time you followed a link *to a page*.

Crossing the breakpoint sets the class again from scratch rather than carrying it across: going
narrow starts shut, going wide restores the preference.

**Taking turns is gated on a `js` class** that the same `<head>` script adds. The toggle is a
script; with no script it does nothing, and hiding the page behind a button that cannot be pressed
would be worse than showing both. Without JavaScript the two stack, as before.

**The header becomes `position: sticky` at this width.** It holds the only way back to the other
view, so it cannot be something you scroll past.

## Consequences

The `16rem` cap is gone and the index is as long as it is — it is the whole view now, so there is
nothing for it to crowd.

`clamp()` in `chrome.js` measures a real chip to find five rows, and a hidden chip is zero high. On
a narrow window the sidebar starts hidden, so the measurement is now also taken when the index is
opened; before, five rows and one row were the same number and the clamp silently never engaged.

`html.js:not(.side-off) .page:not(.is-alone) main` carries an `:not(.is-alone)` for a reason worth
keeping: signed out there is no sidebar and no toggle, so the page must never be the half that gets
hidden — that would be a blank screen with no way off it.

## Alternatives considered

**Keep the band, just make it shorter.** Rejected: the height was the smaller problem. A band above
the header is above its own toggle at any height.

**A fixed overlay under the header, page left in place behind it.** This is what the old comment
ruled out, and its reasoning was sound about state but wrong about the cost — it needs the header's
height as a `top` offset, which depends on its content and so has to be measured in script and kept
in sync on resize. Hiding the page instead needs no measurement at all, and the two arrive at the
same thing on screen.

**A second state bit, `side-on`, with narrow defaulting to off.** Rejected: it is the same one
question — is the index showing — and two bits can disagree. Defaulting the single bit per width
gets the same behaviour with nothing to reconcile.

**Persist the narrow state too.** Rejected, and this is the one that would look like a courtesy.
Every page link in the index is a real navigation; with "open" remembered, each one would reload
into the index rather than the page just asked for.
