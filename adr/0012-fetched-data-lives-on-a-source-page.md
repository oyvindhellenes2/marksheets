# ADR-0012: Data fetched from an API lives on a source page

- **Status:** Proposed
- **Date:** 2026-09-01
- **Supersedes:** —
- **Superseded by:** —

**Nothing here is built.** This records a design settled in discussion so it does not have to be
argued again when somebody sits down to write it. It becomes *Accepted* when the experiment in
*Try it from outside first* has been run and found wanting, and the thing exists.

## Context

The case that prompted this: a page for a building, showing its power consumption over the last
month, up to date whenever you open it. The numbers come from a meter API. Nothing in the app can
reach outside itself today.

Four things already true constrain every answer:

- **The renderer is synchronous.** `Renderer.expand` builds a string inside the HTTP handler. There
  is no point in it where a request can be awaited without blocking the response.
- **Pages are files and the folder is the database** ([ADR-0001](0001-pages-are-files.md)). The app
  works offline because everything it needs is on disk.
- **Git is the historian** ([ADR-0004](0004-git-is-the-historian.md)). What a page said at a commit
  is what the file said at that commit.
- **Reads do not write.** ADR-0001 rejected having a render POST backlink registers into the pages
  it read: it leaves dangling entries, and hand-editing bypasses it.

`go.mod` has no requirements ([ADR-0002](0002-no-dependencies.md)) and does not need any: `net/http`
does this.

## Decision

**A source page.** A third kind of page, beside the ordinary one and the working file: its content
is written by the app, not typed. It carries what is needed to fetch — URL, an auth reference, a
refresh interval, a mapping — and its `children` are the result.

```json
{
  "title": "Straum – Bygg A",
  "tags": ["bygg-a", "straum"],
  "source": {
    "url": "https://api.example.com/consumption?meter=1234&period=month",
    "auth": { "header": "Authorization", "env": "STRAUM_KEY", "prefix": "Bearer " },
    "interval": "1h",
    "fetched": "2026-09-01T20:14:00+02:00",
    "error": ""
  },
  "children": [ "…rewritten by every refresh…" ]
}
```

Four decisions inside that.

**The fetched content is ordinary nodes.** A series becomes a `table`
([ADR-0011](0011-a-table-is-its-own-type.md)) — which is what an array of `{tidspunkt, kWh}` is —
and a scalar becomes a `data` line. This is the point of the whole design: `@` needs **no new
mechanism at all**. `@straum-bygg-a/siste-månad` walks nodes exactly as it does anywhere else, and
filters, transclusion, backlinks and the eventual `=sum()` all work on the result unchanged.

The mapping from JSON to nodes is the one genuinely new mechanism and is deliberately left open
here. It needs a dotted path to a value, and a way to say "this array becomes these columns". The
syntax is an implementation choice, not a decision worth pre-arguing.

**A source page's `children` are the app's, whole.** Not a marked region inside a page you also
write in — the entire body is rewritten by each refresh, and anything typed there is lost. Prose
about the building belongs on the building page, which reads the source with `@`. That is also why
this is a page rather than a line: one place for the plumbing, and the page you actually read stays
prose and queries.

**Refresh is a ticker, not a visit.** A background loop refreshes each source page on its own
interval, regardless of whether anyone is looking, plus a button for "now". The data is therefore
already fresh when you arrive, which is what "updates each time I visit" actually wants.

**Keys are named, never written.** The page names an environment variable; the value lives in the
environment and never touches the file, the git history or the read view.

**Fetched data is content, not cache.** Source pages live in `pages/`, are published like anything
else, and are versioned. See the alternative below for what this costs and why it is still right.

## Consequences

Every refresh makes the source page unpublished, so the front page will show it amber more or less
permanently. Either that is accepted as honest, or source pages are left out of `unpublishedSlugs`.
This should be decided when it is built, not guessed at now.

Because the file is only committed when `Publiser` is pressed, git records the values as they stood
at each publish rather than at each refresh. That is a free time series of your power use, sampled
at the points you chose to publish, and it is a decent argument for the *content* decision on its
own.

Outbound requests take a URL that came from a file, so they need a guard: an allowlist, or at
minimum a block on loopback, private ranges and link-local. `safeURL` protects rendered links and is
not this.

"Consumption over the last month" is a sum, and formulas are not built. Until they are, this feature
is only as good as the endpoints available — pick ones that pre-aggregate, or accept a table you
read rather than a number you glance at.

This is the largest thing in the app after the editor: a page kind, secrets handling, outbound HTTP
with timeouts and single-flight locking, a staleness policy, error surfacing in the UI, and the
publish question above.

## Alternatives considered

**Fetch at render time, from an `@`-function** — `@hent(https://…)`. The first idea and the worst.
The renderer is synchronous, so it blocks the response; it would fire on every toggle to `Les`, and
on `handleHistoryVersion`, which means looking at what a page said in June makes live calls today;
the value would never be in the file, so git would stop being the historian of what the page said
and a published page would say different things on different days; and the app would stop working
offline. Rejected on any one of those.

**Store the response as a JSON object on a node, and address into it with `@`.** Rejected because it
needs a *second* path language: `@kurs/usd` would resolve `kurs` through the node walker and `usd`
through something JSON-shaped that does not exist. Two path syntaxes behind one `@` is the kind of
thing that is tolerable for a month and permanent thereafter. It is also unreadable in a file that
is meant to be edited by hand.

**A source *line* type rather than a page.** Rejected: it mixes machine-owned nodes into a
hand-written page, so "which lines will be overwritten" becomes a live question on every page, and a
line carrying a URL, a key reference and an interval is a fat line on a page you have to look at.

**Refresh when a page that reads the source is visited.** Considered seriously and rejected, and the
reasoning is worth keeping because it is the obvious thing to reach for. It would mean rendering
page A triggers a write to page B — which is the shape ADR-0001 rejected, even done after the
response. The distinctions are real (B exists only to hold fetched data, nothing dangles, hand-edits
there are meant to be overwritten) but they are distinctions, not an exemption, and a ticker avoids
the argument entirely at the cost of fetching for pages nobody opened. On a personal machine with a
handful of sources that cost is nothing.

**Fetched data as cache, outside `pages/`.** It would never dirty anything, never be published, and
never bloat the notes repo. Rejected because `@` reaches pages through `Store.DocBySlug`, so a
second read-only store would have to exist and be taught to every path that resolves a query — and
because the accidental time series described above is worth more than the tidiness.

**Do it outside the app entirely**, with a script that fetches and writes `pages/straum-bygg-a.json`
from cron. **Not rejected — this is what to try first.** `Store.load` re-reads any file whose mtime
or size changed, so a building page with real `@`-queries into real data works today with no code at
all. The only thing it lacks is the refresh interval being the app's business rather than crontab's.
If a fortnight of that turns out to be enough, this record stays *Proposed* for good, and that is a
fine outcome.
