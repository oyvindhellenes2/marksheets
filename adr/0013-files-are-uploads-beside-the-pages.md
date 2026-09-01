# ADR-0013: A file is uploaded and kept beside the pages

- **Status:** Accepted
- **Date:** 2026-09-01
- **Supersedes:** the `image` line type
- **Superseded by:** —

## Context

The `image` type held a URL — `src` and `alt` — and drew whatever was at the other end. It was the
only type that pointed outside the folder, and it was the wrong shape twice over.

A URL is somebody else's file. It rots, it needs the internet to render, and the page is a claim
about a picture rather than a page with a picture in it. That sits badly beside everything else here:
pages are files, the folder is the database ([ADR-0001](0001-pages-are-files.md)), and the app works
offline because everything it needs is on disk.

And it was only ever images. A drawing, a scanned invoice, a PDF from the council — none of them had
anywhere to go.

In practice the type was barely used: three nodes across the notes, two of them inline `data:` SVG
placeholders and one empty. Nothing pointed at a real external URL, so nothing was lost by changing
what the type means.

## Decision

`image` becomes `file`: an **upload**, with two fields — the stored name, and a name for the file.

**Attachments live in `filer/`, inside the page folder.** Same backup, same git repository, same
publish as the pages that show them. `publishSet` stages the files a page shows along with the page,
because a published page whose picture only exists on the machine that wrote it is worse than a
page with no picture.

**Kept under a readable name**: the one it arrived with, slugged the way a page title is, keeping its
extension. `Skisse av Disken.PNG` becomes `skisse-av-disken.png`, and a name already taken gets a
number appended — the same rule, and the same `O_EXCL`, as a page slug.

**Drawn only from a short allowlist** — PNG, JPEG, GIF, WebP, AVIF, PDF — served with that exact
content type and `X-Content-Type-Options: nosniff`. Everything else is sent with
`Content-Disposition: attachment`.

**SVG is on the download side of that line, deliberately.** It is an image to look at and a document
that can run script, and an attachment is served from the same origin as the app — so an inline SVG
would be running script here. It uploads, stores and links like anything else; it just does not draw.

**A stored name has to round-trip** through `files.StoredName` before anything opens it, so a request
cannot address a file outside the folder. `FilesOn` applies the same test before a publish stages
anything, because a page file is hand-editable and a `file` node saying `../../.git/config` is a
thing somebody can write.

Old `image` nodes are migrated on load: the type becomes `file`, `alt` becomes `name`, and the old
`src` is kept on the node and still drawn. A page written before uploads existed keeps its picture.

## Consequences

`Repo.Commit` used to keep staging inside the page folder with `filepath.Base`, which threw the
directory part away entirely. That was too blunt once attachments existed in a subfolder — it
flattened `filer/skisse.png` to a file beside the pages. It is now a containment check: join, clean,
and refuse anything that is not under the page folder. The invariant is unchanged and better
enforced, but it is a change to the code that protects "the app cannot commit its own source", and
worth knowing about.

Binaries now go into the notes repository. At the size of a personal notebook that is fine; at the
size of a photo library it would not be, and the answer then would be an ignore rule plus a separate
sync, not a redesign of this.

**Nothing sweeps up orphans.** Delete a file line, or a page, and the file stays in `filer/`. Losing
a file to a deleted line is the worse mistake of the two, so the orphan stays and a tidy-up command
is a job for later.

The `examples/` page set is served with `PAGES_DIR=examples`, which is inside the code repository —
so uploading there puts a binary in the *public* repo rather than the private notes one. Worth
remembering when using that instance for anything but looking.

## Alternatives considered

**Keep the URL type and add uploads beside it.** Rejected: two ways to put a picture on a page, and
the URL one is the one that breaks. If an external image is ever genuinely wanted, inline markdown
already has `[text](url)` and can carry a link to it.

**Content-addressed names**, `sha256-….png`. Tidier — deduplication for free, no collisions to
resolve — and rejected because the folder is meant to be opened in a file manager and read in a git
diff like everything else here. A page that says `skisse-av-disken.png` can be understood without the
app; one that says `a3f1c9…png` cannot.

**Files outside the page folder**, in their own directory beside it. Rejected: they would fall out of
the pages repository, so publishing a page would not publish its picture, and the notes would need a
second thing backed up. The whole point of ADR-0006 is that the notes are one repository.

**Serve everything inline and let the browser decide.** Rejected: sniffing plus same-origin is how an
uploaded SVG or HTML file becomes script running on this app. The allowlist is short on purpose, and
`nosniff` is there so the browser does not helpfully widen it.

**Drag and drop onto the editor.** Not rejected — just not built. It is the natural gesture and the
right next step; the button exists first because the interesting decisions were about storage, not
about the gesture.
