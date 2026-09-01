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

// MaxUpload is the largest file that may be stored. Big enough for a photo or
// a scanned drawing, small enough that a mistake does not quietly put a
// gigabyte into a git repository.
const MaxUpload = 25 << 20

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
