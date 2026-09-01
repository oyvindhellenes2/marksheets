# ADR-0011: A table is its own type, and its cells are positional

- **Status:** Accepted
- **Date:** 2026-09-01
- **Supersedes:** —
- **Superseded by:** —

## Context

There was no way to write a table. `data` was the closest thing — a named scalar with a value and a
unit, `budsjett = 10000 kr` — and the proposal on the table was to widen it: `Tab` on the last field
would add a fourth, `Enter` would start another line carrying the same fields, and consecutive lines
would read as a grid. The goal was right; SPEC has said since the beginning that formulas and
aggregates are "what would make the spreadsheet half literal", and you cannot have those without
somewhere to put a grid.

Widening `data` was the wrong mechanism, for three reasons.

**It breaks `@`-queries into data.** `@menyen/kaffi/espresso` works because a data node is a named
scalar: `Label()` is its `name`, and `expand` renders it as `value unit` inline. Give it n columns
and there is no answer to what that query returns — the row, one cell, or which one is the name.
Data nodes are the main thing worth addressing by query, and this would have made addressing them
ambiguous. That is the feature that justifies the format.

**Columns per row are rows that can disagree.** "The same fields as the line above" copies the column
names onto every row, and nothing keeps them in step afterwards. Renaming a column on row five makes
it a different column, silently. It also leaves the read view with no header row to draw, because
there is no one place the headings live.

**It costs the typing.** `value` is a number and `unit` is text, which is what makes `dataValue`
format properly and what a future `=sum(@gym.*.budsjett)` would have to sum. Arbitrary columns are
strings unless the column itself declares a kind — which again wants columns somewhere they can be
declared.

## Decision

A `table` type, declared in `types.json` like every other, carrying its own shape beside its fields:

```json
{ "type": "table", "name": "leverandorar",
  "columns": ["Leverandør", "Vare", "Pris"],
  "rows": [ { "id": "n_r1", "cells": ["Solberg brenneri", "bønner", "189 kr/kg"] } ] }
```

**Columns are declared once for the whole table.** Rows cannot disagree about what they hold, a
column rename is one edit, and the read view has a header row to draw.

**Cells are positional.** This is a deliberate exception to "named fields, not positional lists"
(SPEC, *Document model*), and the exception holds because the reason for the rule does not apply:
that rule guards against a value drifting from a schema kept somewhere *else*, and here the columns
are on the same node as the cells, so a column change rewrites the table atomically. Keying by name
would also make two columns with the same heading impossible, and tables have those.

`data` is untouched. A table is for a grid; a data line is for a number with a name.

The keyboard is the one that was asked for, moved onto the grid: `Tab` walks the cells and makes a
column off the right-hand edge, `Enter` opens a row and leaves the table on a blank one, `↑`/`↓` move
between rows before leaving. Cells are addressed by position — `col:2`, `cell:1:2` — through the
existing `data-field` mechanism, so `here()`, `restore()` and the undo focus reach them without a
second caret system.

## Consequences

`columns` and `rows` join `page` and `links` as keys the editor must carry through `flatten` → `nest`
*and* the undo snapshots. Dropping them empties the table on the next save. `cells` joins the
reserved names, so no `types.json` can declare a field called that.

A table is made rectangular in `doc.Normalise` and again in the editor's `reflow`, so a hand-written
or ragged table opens rather than being refused — the same bargain as everywhere else here.

Changing a table to another type, or deleting it with `Backspace`, is refused while it holds
anything. Its content is in cells and no other type has room for them, so the alternative is silent
loss on the next save.

Cells carry inline markdown but not `@`-queries, and a `[#tag]` filter does not reach them: links are
recorded per field, and a cell is not a field. A query in a cell would resolve by path, break on a
rename and never appear in a backlink. Half a feature is worse than none, and it is written down as
not built rather than left to be discovered.

## Alternatives considered

**Widen `data` row by row**, as originally proposed. Rejected for the three reasons in *Context*; the
query damage is the one that decided it.

**Key cells by column name** — `{"id": "n_r1", "Leverandør": "Solberg brenneri"}` — to stay with
"named fields, not positional lists". Rejected: renaming a column would have to rewrite a key on
every row, a column with an empty heading would have no key at all, and two columns with the same
heading would collide. The rule it would honour is about a different situation.

**A `row` node type nested under a `table` node**, using the existing children machinery instead of a
new shape. Rejected: only headers nest ([ADR-0003](0003-only-headers-nest.md)), and making a table
nest would either break that rule or need a second exception to it. A table is one thing, and one
node is what it is.

**Wait for formulas and design the two together.** Tempting, and rejected: there is nothing to write
a formula *over* until there is a grid, and the grid's shape — columns declared once, cells
positional — is what a formula would want anyway.
