# ADR-0010: Depth belongs to headings alone

- **Status:** Accepted
- **Date:** 2026-09-01
- **Supersedes:** the empty-line half of the editor keys in [SPEC](../SPEC.md), *Editor keys*
- **Superseded by:** —

## Context

[ADR-0003](0003-only-headers-nest.md) settled that only headers nest, and the editor was built so a
leaf could not change its own depth: `Tab` did nothing on a text line, and where a line sat was
"decided by the heading it is under". That was true of every gesture the editor offered.

It was not true of the document. Depth is a number on a row, and two ordinary edits could leave a
leaf carrying a depth its heading did not give it:

- Write a line, then put a heading above it. The line keeps the depth it had, and is now a *sibling*
  of the heading rather than its child — visibly beside it, and saved that way.
- A hand-edited file can simply say so, since in JSON a leaf beside a header is a leaf beside a
  header.

So "a leaf has no depth of its own" was a property of the *input*, enforced gesture by gesture, and
one gesture nobody had thought about was enough to break it.

Separately, the empty line was in an odd position. `Enter` on one stepped out a level and left the
line behind; `Enter` again turned it back into a text line. Neither did anything with the line, and
an empty line that nothing resolves is dead weight in a format with no `newline` type
([ADR-0005](0005-no-newline-type.md)) — spacing is the renderer's job here, so a blank line means
nothing on the page and only something to the person still typing.

And `Enter` at the start of a heading was plainly wrong: it emptied the heading and put its own
title into a text line inside it.

## Decision

**Depth is recomputed, not maintained.** `reflow` walks the rows before every render and puts each
leaf exactly one level inside the heading above it. Items follow their host. Heading depths are the
only ones anybody sets, and the only ones anybody can set.

Four gestures follow from it:

- **`Enter` on an empty line makes it a heading, one level out.** An empty line is somebody who has
  finished writing something, and what comes next is nearly always a new section. So the blank line
  becomes the heading and they type its name, rather than being left lying there.
- **`Enter` at the very start makes room above**, and a heading takes its whole section down with
  it. Splitting at offset zero is not a split: there is nothing to the left of the caret to keep. On
  a heading it used to empty the heading and drop its own title into a text line inside it. The line
  moves rather than being copied, so it keeps its id — and with it a task's working file and the
  link hints recorded on it.
- **`Tab` on a text line turns it into a heading**, where it stands — the same thing `#` does. A leaf
  has no indentation to gain, so the only way it can go in a level is to become the thing that makes
  one.
- **Deleting a heading lifts what it held.** `Backspace` on an emptied heading removes the heading
  and moves its contents up a level. Refusing to delete it, which is what happened before, protected
  the content by refusing the gesture; lifting protects it and lets the gesture through.

## Consequences

The invariant now holds by construction rather than by vigilance, including for files written by
hand: opening such a page reflows it, and the next save writes it back correct. Reflow is not marked
as an edit, so merely *looking* at a page never rewrites it.

A leaf that follows a new heading falls inside it. Turn a line into a heading mid-section and the
lines after it become its contents — the ordinary outliner behaviour, and the only thing "one level
inside the heading above" can mean.

`setType` split into `changeType` (model) and `setType` (model, then draw), because two of the
gestures above change a type as one step of something larger.

`Enter` at the start still leaves an empty line behind — that is the point of it. "No blank lines"
was never the rule and would be a bad one; what is true is narrower and worth stating precisely: no
gesture leaves a blank line *you did not ask for*, and `Enter` on one turns it into something.

`data` is the one type that turns it into something else. A blank data line becomes a **text** line
rather than a heading, because a data line is a row of a table and a blank row means the table is
over — and prose follows a table far more often than a new section does. The rule is otherwise
unchanged: the line becomes something, and is not left lying there.

One bug surfaced while testing and was fixed with it: emptying a contenteditable with `Backspace`
leaves a stray `<br>`, which reads back as `"\n"`. Every rule asking "is this line empty" answered no
on the line that most obviously was. A field of nothing but whitespace is now stored as empty.

## Alternatives considered

**Keep policing depth at each gesture, and add a check to the one that was missing.** Rejected: that
is the argument that produced the hole in the first place. Every new gesture is another place to
remember, and the one time policing failed in this codebase it silently deleted five lines
([ADR-0003](0003-only-headers-nest.md)). Recomputing cannot be got right in one place and wrong in
another.

**Enforce it in `doc.Normalise` too**, moving a leaf that follows a header in the JSON inside that
header. Rejected for now: in a tree a sibling genuinely is a sibling, so this would be the server
silently moving content in files it was only asked to read. The editor's reflow already fixes such a
page the moment anybody edits it, which is gentler and arrives at the same place.

**Let `Enter` on an empty line delete it instead.** Rejected: it destroys the line you are standing
on, and the caret then has nowhere obvious to go. Becoming a heading keeps the line and gives it a
job.

**Keep conjuring a line with `↑` at the top of the page** ([ADR-0008](0008-the-tasks-heading-is-furniture.md))
rather than making `Enter` at the start work. Rejected: the conjured line only ever helped on the
first line, it existed conditionally and had to be swept up from five places when it did not survive,
and `Enter` at the start is the gesture hands already have for this. One rule that works everywhere
beat one special case plus its cleanup.

**Keep refusing to delete a heading with content.** Rejected: the refusal is silent from where the
user sits — `Backspace` simply stops working — and the way out was to delete the contents first,
which is more dangerous than what the refusal was protecting against.
