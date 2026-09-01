# ADR-0008: The tasks heading is furniture, not content

- **Status:** Accepted
- **Date:** 2026-09-01
- **Supersedes:** —
- **Superseded by:** —

## Context

Every page started from a template holding an `Oppgåver` heading and one empty task, and todos were
allowed under that heading and nowhere else ([SPEC](../SPEC.md), *Tasks and working files*). The
heading was otherwise an ordinary header: it could be renamed, indented, dragged down the page by
`⇧Enter`, or deleted, and anything at all could be written under it.

Nothing broke when that happened — but everything downstream quietly stopped working. `inTasks`
matches the heading by its slugged label, so renaming it to `Ting å gjere` moved every task on the
page out of the only place a task may be made. A page whose heading had been deleted could grow no
new tasks at all. And a heading that could sit anywhere meant the tasks could be anywhere, so
"the tasks are at the top" was a convention rather than something the page could rely on.

Meanwhile the section earned nothing in the read view. Every page opened, in `Les`, on the word
"Oppgåver" and a block of unticked checkboxes — the working state of the page standing in front of
the page.

## Decision

The heading stops being a line you own.

- **Pinned to the top.** `doc.Normalise` moves it to the front of the document, and makes one from
  `doc.TasksBlock` when the file has none. It runs on every load and every save, so a file written by
  hand arrives with the heading in place like any other malformed input that gets repaired.
- **Not editable.** The editor draws it as a label in the accent colour with no field in it, so
  there is nothing to type over, no type menu on its gutter, and no caret for the arrow keys to
  land in — they step over it.
- **Tasks and nothing else under it.** The type picker, `⌘1`–`⌘6` and the line-start shortcuts all
  gate on the same `allowedType`, which now reads both ways: only a task may be made inside the
  heading, and no task may be made outside it.
- **The whole section is left out of the read view**, tasks and all. `render.Page` drops the node.
  `Les` is for reading the page; a to-do list is where the page gets worked on rather than anything
  the page says. A query still reaches the tasks, because `@side/oppgåver[#øyvind]` is somebody
  asking for them rather than the page offering them unbidden.
- **A new page starts with a `text` line under the section**, and the caret opens there rather than
  in the task list. If the heading is not yours, neither is the first line of the page, and the page
  has to start somewhere you can type. That line is a text line and not a heading, because the caret
  is already in it and clearing a heading before writing a sentence is backwards.

Because it has no caret, it carries a `+` button that starts a task.

## Consequences

The invariant is now real: the tasks of a page are the children of its first node, and they are
tasks. Code that wants them no longer has to hope, and both the read view and the caret can act on
"the page proper" by taking everything after that node.

`doc.Template` and `doc.TasksBlock` split apart, which is a trap worth naming: the template carries
the body line and the block does not, and `Normalise` must repair a missing heading with the block.
Using the template there would drop a blank line into every hand-edited page it touched.

`↑` on the first line used to conjure a provisional line above it — one that vanished again if you
left without typing. Nothing can sit above the pinned heading, so it went, and what replaced it is
`Enter` at the start of a line making room above
([ADR-0010](0010-depth-belongs-to-headings.md)): a rule that works on every line is worth more than
one that only works on the first, and it needs no line that exists conditionally.

Pages made before this decision are unaffected: all of them already carried the heading at the top,
because the template put it there. Todos that sit outside it, or stray lines that sit inside it,
keep working — the rules are enforced on creation, never retrospectively, which is how ADR-0003 has
always been applied.

## Alternatives considered

**Leave it editable and just pin the position.** Rejected: renaming it is the failure that costs
the most and the one most easily done by accident, since it looks exactly like every other heading.
Pinning without freezing fixes the cheap half of the problem.

**Match the heading by node id rather than by its name**, so renaming would be harmless. Rejected:
the id would have to be recorded somewhere doc-level, and a hand-written file would still have no
way to say which heading it meant. Matching by name is what makes a file written in a text editor
work, and freezing the name is what makes matching by name safe.

**Unwrap the heading but keep the tasks in the read view.** Built first, on the reasoning that the
label says nothing but the list is content. Replaced within the hour: what the read view is *for* is
reading the page, and a list of things not yet done is the one part that is never read that way. It
is also the bulky part, so the half-measure left the read view opening on a block of checkboxes and
saved nothing.

**Keep at least one open task alive instead of adding a `+` button**, so there is always somewhere
to type. Rejected: ticking the last task archives it, which would leave the rule fighting the
archive — and refusing to let somebody delete their own last task is worse than a button.
