#!/usr/bin/env bash
set -euo pipefail

# A dated copy of the page folder, kept beside the live one.
#
# Git is the durable record and GitHub is the off-site copy — but only of what
# has been *published*. Everything between the last Publiser and now exists in
# one place: the working tree. That is where a whole afternoon lives, and it is
# also where a page deleted-but-not-yet-published goes when somebody presses the
# button twice.
#
# So this copies the folder whole — working tree, `.git`, attachments, and the
# untracked files git does not know about — rather than cloning the repository,
# which would capture exactly the half that is already safe.
#
# What it is not: an off-site backup. Both copies are on the same disk in the
# same container, so this answers "I deleted the wrong thing" and does nothing
# at all for a dead disk. GitHub covers the published half of that; the
# unpublished half has no answer yet, and that is worth knowing rather than
# assuming.

PAGES="${PAGES_DIR:-/opt/marksheets/pages}"
STORE="${BACKUP_DIR:-/opt/backup/pages}"
KEEP="${KEEP:-14}"

if [[ ! -d "$PAGES" ]]; then
    echo "backup-pages: $PAGES does not exist" >&2
    exit 1
fi

mkdir -p "$STORE"
stamp="$(date +%Y-%m-%d-%H%M%S)"
dest="${STORE}/${stamp}"

# `cp -al src dest` copies *into* dest when dest already exists, which turns a
# rerun within the same second into a snapshot nested inside its predecessor.
# Refusing is the whole fix: there is nothing useful to do twice in one second.
if [[ -e "$dest" ]]; then
    echo "backup-pages: $dest already exists — nothing to do" >&2
    exit 0
fi

# Hard-linked against the previous snapshot, so unchanged files — which is
# nearly all of them, and includes an 80 MB video — cost one directory entry
# rather than another copy. Fourteen snapshots of a 180 MB folder end up a
# little over 180 MB, not 2.5 GB.
#
# `cp -al` links; a file that has *changed* since is then unlinked and replaced
# by the second cp, which copies only what differs. Doing it in that order is
# what keeps the older snapshot intact: without the unlink, writing the new
# version would write through the hard link and quietly rewrite history.
previous="$(ls -1d "${STORE}"/*/ 2>/dev/null | tail -1 || true)"
if [[ -n "$previous" ]]; then
    cp -al "${previous%/}" "$dest"
    # `-u` and `--remove-destination` are a pair, and both halves are needed.
    #
    # `-u` copies only what has actually changed, which is what leaves the hard
    # links to the previous snapshot standing — without it every file is
    # rewritten and the snapshot costs a full copy. `--remove-destination`
    # unlinks the ones it *does* copy before writing, because writing through a
    # hard link would edit the previous snapshot too, which is the one thing a
    # backup must never do.
    cp -au --remove-destination "$PAGES/." "$dest/"
else
    cp -a "$PAGES/." "$dest/"
fi

# Anything deleted from the live folder since the last snapshot is still in the
# copy that was hard-linked from it. That is the point — but it means the new
# snapshot has to be told about the removals, or every snapshot from here on
# carries every file that has ever existed.
( cd "$dest" && find . -type f -o -type l ) | sed 's|^\./||' | while read -r f; do
    [[ -e "$PAGES/$f" ]] || rm -f "$dest/$f"
done
find "$dest" -type d -empty -delete 2>/dev/null || true

# Oldest first out. Snapshot names sort chronologically, so `ls` is the order.
count="$(ls -1d "${STORE}"/*/ 2>/dev/null | wc -l)"
if (( count > KEEP )); then
    ls -1d "${STORE}"/*/ | head -n "$(( count - KEEP ))" | while read -r old; do
        rm -rf "$old"
    done
fi

# A file count rather than a size. `du` immediately after writing on ZFS reports
# whatever has reached disk so far — the first run of this said "281K" for a
# 179 MB snapshot, which is alarming and wrong. The count is exact at once.
echo "backup-pages: $dest ($(find "$dest" -type f | wc -l) files, $(ls -1d "${STORE}"/*/ | wc -l) snapshots)"
