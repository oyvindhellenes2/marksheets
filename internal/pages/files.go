package pages

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"marksheets/internal/doc"
	"marksheets/internal/files"
)

var ErrTooBig = errors.New("fila er for stor")

// FilesPath is the folder attachments are kept in.
func (s *Store) FilesPath() string { return filepath.Join(s.dir, files.Dir) }

// filePath maps a stored name to a path on disk, refusing anything that is not
// a plain stored name. The name has to round-trip through files.StoredName,
// which strips path separators, so a request can never address a file outside
// the folder — the same guard, and the same reasoning, as Store.path.
func (s *Store) filePath(name string) (string, error) {
	if name == "" || files.StoredName(name) != name {
		return "", ErrBadSlug
	}
	return filepath.Join(s.FilesPath(), name), nil
}

// OpenFile opens a stored file for reading.
func (s *Store) OpenFile(name string) (*os.File, os.FileInfo, error) {
	path, err := s.filePath(name)
	if err != nil {
		return nil, nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, ErrNotFound
		}
		return nil, nil, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, nil, err
	}
	return f, info, nil
}

// FileSize is the size of a stored attachment, and whether it is there at all.
// The read view asks so it can say how big a download is — and so it can say
// plainly when a page points at a file that is gone.
func (s *Store) FileSize(name string) (int64, bool) {
	path, err := s.filePath(name)
	if err != nil {
		return 0, false
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return 0, false
	}
	return info.Size(), true
}

// SaveFile stores an upload and returns the name it was kept under, which is
// not necessarily the one it arrived with.
//
// O_EXCL makes the uniqueness check and the claim one atomic step, exactly as
// it does for a new page, and a name already taken gets a number appended
// rather than overwriting what is there.
func (s *Store) SaveFile(original string, r io.Reader) (string, int64, error) {
	if err := os.MkdirAll(s.FilesPath(), 0o755); err != nil {
		return "", 0, err
	}

	want := files.StoredName(original)
	stem, ext := want, ""
	if i := strings.LastIndex(want, "."); i > 0 {
		stem, ext = want[:i], want[i:]
	}

	name := want
	for attempt := 2; ; attempt++ {
		path := filepath.Join(s.FilesPath(), name)
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err == nil {
			// One byte past the limit is read on purpose: it is how a file
			// that is exactly too big is told from one that only just fits.
			n, copyErr := io.Copy(f, io.LimitReader(r, files.MaxUpload+1))
			closeErr := f.Close()
			switch {
			case copyErr != nil:
				os.Remove(path)
				return "", 0, copyErr
			case closeErr != nil:
				os.Remove(path)
				return "", 0, closeErr
			case n > files.MaxUpload:
				os.Remove(path)
				return "", 0, ErrTooBig
			}
			return name, n, nil
		}
		if !os.IsExist(err) || attempt > 50 {
			return "", 0, err
		}
		name = stem + "-" + strconv.Itoa(attempt) + ext
	}
}

// FilesOn lists the attachments a page refers to, as refs relative to the page
// folder. Publishing a page has to take them along, or everyone else reads a
// page with holes in it.
func (s *Store) FilesOn(d *doc.Doc) []string {
	var out []string
	seen := map[string]bool{}
	d.Walk(func(n *doc.Node, _ int) {
		if n.Type != "file" {
			return
		}
		name := n.Str("file")
		if name == "" || seen[name] {
			return
		}
		if _, err := s.filePath(name); err != nil {
			return
		}
		seen[name] = true
		out = append(out, files.Ref(name))
	})
	return out
}
