// Package files says how an attachment is named and how it may be served.
//
// It is its own package because both the store that keeps files and the
// renderer that draws them need these answers, and the store already imports
// the renderer — so anything they share has to sit below them both.
package files

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"marksheets/internal/doc"
)

// Dir is the folder attachments live in, inside the page folder, so that they
// travel with the pages: the same backup, the same git repository, the same
// publish. A page whose picture lived somewhere else would be a page that
// arrives broken wherever it is read.
const Dir = "filer"

// MaxUpload is the largest file that may be stored.
//
// It is deliberately generous — a scanned drawing set or a long PDF is a
// legitimate thing to put on a page, and 25 MB turned that into a fight. What
// it is *not* is a promise that a file this size will arrive: the wiki is
// reached through a Cloudflare tunnel, and Cloudflare refuses a request body
// over 100 MB before it reaches this process at all. Anything past that is
// stopped by the edge with an error page this app never sees and cannot
// improve on.
//
// The other half of the cost is git. The pages are a repository that is cloned
// whole — by everybody who works on the wiki, and by the booking site, which
// reads room text out of it — and a blob is kept for ever, so a large file
// added and deleted the same afternoon is carried by every clone from then on.
// Video is refused outright for that reason (see notStored); this limit is what
// stands between the folder and everything else large.
const MaxUpload = 500 << 20

// EdgeLimit is the largest body that can actually reach this process, and it is
// not ours to set: Cloudflare's tunnel refuses anything bigger with a 413 of
// its own, before the request arrives. Confirmed by sending one — a 120 MB
// upload is turned away after about three megabytes, on the Content-Length
// alone.
//
// It matters because of what the browser checks against. Refusing a file that
// will not fit is only useful if the number it uses is the one that will
// actually stop it; checking against MaxUpload and letting the edge do the
// rest would put back exactly the silent failure that the early check exists to
// prevent — a long upload ending in somebody else's error page.
//
// Zero turns this off, for a deployment that is not behind the tunnel.
const EdgeLimit = 100 << 20

// UploadLimit is the largest file worth offering to send: the smaller of what
// this app will store and what the network in front of it will carry.
func UploadLimit() int64 {
	if EdgeLimit > 0 && EdgeLimit < MaxUpload {
		return EdgeLimit
	}
	return MaxUpload
}

// extRe is what may pass as an extension. Anything else is dropped rather than
// kept, because the extension decides how the file is served.
var extRe = regexp.MustCompile(`^[a-z0-9]{1,8}$`)

// StoredName is the name an upload is kept under: the name it arrived with,
// slugged the same way a page title is, keeping its extension.
//
// Readable rather than hashed, because the folder is meant to be opened in a
// file manager and read in a git diff like everything else here. The cost is
// that two files called the same thing need distinguishing, which is the same
// cost pages have and is solved the same way.
func StoredName(original string) string {
	base := filepath.Base(original)
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(base), "."))
	if !extRe.MatchString(ext) {
		ext = ""
	}
	stem := doc.Slug(strings.TrimSuffix(base, filepath.Ext(base)))
	if stem == "" {
		stem = "fil"
	}
	if ext == "" {
		return stem
	}
	return stem + "." + ext
}

// Ref is how a stored file is written into a commit: relative to the page
// folder, so it means the same thing to the store, to git and to the URL that
// serves it.
func Ref(name string) string { return Dir + "/" + name }

// serveTypes are the types a file may be shown as, in the page, by content
// type. Everything not here is sent as a download instead.
//
// SVG is deliberately absent. It is an image to look at and a document that
// can run script, and it would run that script on this origin — so it is
// stored and linked like any other file, and simply not drawn inline.
var serveTypes = map[string]string{
	"png":  "image/png",
	"jpg":  "image/jpeg",
	"jpeg": "image/jpeg",
	"gif":  "image/gif",
	"webp": "image/webp",
	"avif": "image/avif",
	"pdf":  "application/pdf",
	// Video the browsers agree on. MP4 (H.264) and WebM are what a phone or a
	// camera produces and what every browser plays; MOV is here because an
	// iPhone writes one and it is usually an MP4 wearing a different name,
	// which Safari plays and others may not. Nothing is transcoded — the file
	// is served as it arrived, and a codec a browser will not take shows the
	// player's own failure rather than something this app can fix.
	"mp4":  "video/mp4",
	"m4v":  "video/mp4",
	"mov":  "video/quicktime",
	"webm": "video/webm",
	"ogv":  "video/ogg",
}

// IsVideo reports whether a stored file is one the read view gives a player.
func IsVideo(name string) bool {
	t, inline := ServeType(name)
	return inline && strings.HasPrefix(t, "video/")
}

// notStored is what this wiki deliberately does not keep, and what to say
// instead of keeping it.
//
// Nothing is on the list. Video was, briefly, and the reasoning still stands as
// a cost rather than as a rule: the pages are a git repository cloned whole —
// by everybody who works on the wiki, and by the booking site, which reads room
// text out of it — and git keeps a blob for ever, so a video added and deleted
// the same afternoon is carried by every clone from then on. That was weighed
// and the wiki keeps video anyway; MaxUpload is what stands between the folder
// and a runaway.
//
// The map stays because the question will come back for some other kind of
// file, and answering it is one line here rather than a new mechanism.
var notStored = map[string]string{}

// Refused says why a file may not be stored here, or "" when it may. The
// answer is a whole sentence, because it is shown to whoever tried.
func Refused(name string) string {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(name), "."))
	return notStored[ext]
}

// ServeType is the content type a stored file is served as, and whether it may
// be shown in the page rather than downloaded.
func ServeType(name string) (string, bool) {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(name), "."))
	if t, ok := serveTypes[ext]; ok {
		return t, true
	}
	return "application/octet-stream", false
}

// IsImage reports whether a stored file is one the read view draws with <img>.
func IsImage(name string) bool {
	t, inline := ServeType(name)
	return inline && strings.HasPrefix(t, "image/")
}

// HumanSize is a file size as a person would say it.
func HumanSize(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f kB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}
