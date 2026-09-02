// Package vcs commits page files to git and pushes them, so publishing is a
// point you can read and return to.
//
// Git is the historian here, never the database, and the layering says so: the
// file is written and safe before a commit is attempted, and the commit is made
// and safe before a push is attempted. A failed commit never turns into a failed
// save, and a failed push never turns into a failed commit.
package vcs

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ErrNoRemote means there is nowhere to publish to. The commit still stands;
// this is a state to report, not a failure to undo.
var ErrNoRemote = errors.New("ingen fjernlager er sett opp")

// Repo is a git repository holding the page folder.
type Repo struct {
	root string // repository root
	dir  string // the page folder
	mu   sync.Mutex
}

// Open finds the repository containing dir. ok is false when there is none,
// which is not an error — the app runs fine without history.
func Open(dir string) (repo *Repo, ok bool) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, false
	}
	out, err := run(abs, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, false
	}
	return &Repo{root: strings.TrimSpace(out), dir: abs}, true
}

// Init creates a repository in dir and commits whatever is already there.
func Init(dir string) (*Repo, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	if _, err := run(abs, "init", "-q", "-b", "main"); err != nil {
		return nil, fmt.Errorf("git init: %w", err)
	}
	r := &Repo{root: abs, dir: abs}
	if err := r.Commit(nil, "Fyrste versjon", Author{}); err != nil {
		return nil, err
	}
	return r, nil
}

// Root is the repository root.
func (r *Repo) Root() string { return r.root }

// Author is who a commit is by. Empty when there is nobody in particular —
// which is what a single-user instance looks like, and what git's own
// configuration is then left to answer.
type Author struct{ Name, Email string }

// Commit stages the named files under the page folder and commits them as
// author. Passing no files stages the whole page folder, which is only used for
// the first commit after init.
//
// Only paths inside the page folder are ever staged — the app must not be able
// to commit anything else, least of all its own source.
func (r *Repo) Commit(files []string, message string, author Author) error {
	r.mu.Lock() // git's index takes one writer at a time
	defer r.mu.Unlock()

	args := []string{"add", "--"}
	if len(files) == 0 {
		args = append(args, r.dir)
	} else {
		for _, f := range files {
			p, ok := r.staged(f)
			if !ok {
				return fmt.Errorf("nektar å stage %q: utanfor sidemappa", f)
			}
			args = append(args, p)
		}
	}
	if _, err := run(r.root, args...); err != nil {
		return fmt.Errorf("git add: %w", err)
	}

	// Nothing staged means nothing changed; that is a success, not a failure.
	if _, err := run(r.root, "diff", "--cached", "--quiet"); err == nil {
		return nil
	}

	var commit []string
	if author.Name != "" {
		// Whoever pressed Publiser wrote these pages, and the history should
		// say so rather than crediting the machine the app runs on. The email
		// is theirs when the provider knows one, and otherwise a local address
		// that is at least stable per person.
		mail := author.Email
		if mail == "" {
			mail = mailish(author.Name) + "@marksheets.local"
		}
		commit = append(commit,
			"-c", "user.name="+author.Name,
			"-c", "user.email="+mail)
	} else if !r.hasIdentity() {
		// Only when git has no identity of its own — passing these
		// unconditionally would credit every commit to the app instead of
		// to whoever configured git.
		commit = append(commit,
			"-c", "user.name=Marksheets",
			"-c", "user.email=marksheets@localhost")
	}
	commit = append(commit, "commit", "-q", "-m", message, "--")
	if len(files) == 0 {
		commit = append(commit, r.dir)
	} else {
		for _, f := range files {
			p, _ := r.staged(f) // already checked above
			commit = append(commit, p)
		}
	}
	if _, err := run(r.root, commit...); err != nil {
		return fmt.Errorf("git commit: %w", err)
	}
	return nil
}

// hasIdentity reports whether git can work out who is committing, from any of
// its own sources — repo config, global config or environment.
// staged turns a path given relative to the page folder into an absolute one,
// and reports whether it really is inside that folder.
//
// This used to be filepath.Base, which kept the app from staging its own source
// by throwing away the directory part entirely. That was too blunt once
// attachments arrived: they live in a folder of their own inside the page
// folder, and Base flattened "filer/skisse.png" to a file beside the pages.
// Checking containment is the same guarantee, made properly — the app still
// cannot commit anything outside PAGES_DIR.
func (r *Repo) staged(f string) (string, bool) {
	p := filepath.Clean(filepath.Join(r.dir, f))
	dir := filepath.Clean(r.dir)
	if p != dir && !strings.HasPrefix(p, dir+string(filepath.Separator)) {
		return "", false
	}
	return p, true
}

// mailish makes a local-part out of a name, for the case where the identity
// provider does not hand over an email. Not doc.Slug: this package shells out
// to git and knows nothing about documents, and one address is not a reason for
// it to start.
func mailish(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case b.Len() > 0 && !strings.HasSuffix(b.String(), "."):
			b.WriteByte('.')
		}
	}
	out := strings.Trim(b.String(), ".")
	if out == "" {
		return "brukar"
	}
	return out
}

func (r *Repo) hasIdentity() bool {
	name, err := run(r.root, "config", "--get", "user.name")
	if err != nil || strings.TrimSpace(name) == "" {
		return false
	}
	email, err := run(r.root, "config", "--get", "user.email")
	return err == nil && strings.TrimSpace(email) != ""
}

// HasRemote reports whether there is anywhere to publish to.
func (r *Repo) HasRemote() bool {
	out, err := run(r.root, "remote")
	return err == nil && strings.TrimSpace(out) != ""
}

// Push sends committed work to the remote. It is given a longer leash than the
// other commands because it is the only one that touches the network.
func (r *Repo) Push() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.HasRemote() {
		return ErrNoRemote
	}
	// Pushing HEAD rather than a named branch keeps this working whatever the
	// branch is called, and still updates the origin/<branch> ref that
	// publishedRef reads back.
	if _, err := runFor(r.root, 30*time.Second, "push", "origin", "HEAD"); err != nil {
		return fmt.Errorf("git push: %w", err)
	}
	return nil
}

// publishedRef is what "already published" means: the remote-tracking branch
// when there is one, HEAD otherwise. Reading the local ref rather than asking
// the remote keeps this off the network — it is accurate because this app is
// the only thing that pushes.
func (r *Repo) publishedRef() string {
	branch, err := run(r.root, "rev-parse", "--abbrev-ref", "HEAD")
	if err == nil {
		if b := strings.TrimSpace(branch); b != "" && b != "HEAD" {
			ref := "refs/remotes/origin/" + b
			if _, err := run(r.root, "rev-parse", "--verify", "--quiet", ref); err == nil {
				return ref
			}
		}
	}
	return "HEAD"
}

// Unpublished lists the page files that differ from what has been published,
// by file name. It covers both kinds of unpublished work — edited but not
// committed, and committed but not pushed — because both are invisible to
// everyone else.
//
// Errors from git are swallowed rather than surfaced: a repository with no
// commits yet is an ordinary state, not a fault, and the worst case is that a
// marker is missing. History is optional here and so is this.
func (r *Repo) Unpublished() map[string]bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	out := map[string]bool{}
	// Paths come back relative to the repository root and go out relative to
	// the page folder. This used to be filepath.Base, which was fine while
	// everything sat directly in that folder and wrong the moment attachments
	// got a subfolder: "filer/skisse.png" arrived looking like a page.
	base, relErr := filepath.Rel(r.root, r.dir)
	collect := func(s string) {
		if relErr != nil {
			return
		}
		for _, line := range strings.Split(strings.TrimSpace(s), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			rel, err := filepath.Rel(base, line)
			if err != nil || strings.HasPrefix(rel, "..") {
				continue
			}
			out[filepath.ToSlash(rel)] = true
		}
	}
	if diff, err := run(r.root, "diff", "--name-only", r.publishedRef(), "--", r.dir); err == nil {
		collect(diff)
	}
	// A file git has never seen has certainly never been published.
	if others, err := run(r.root, "ls-files", "--others", "--exclude-standard", "--", r.dir); err == nil {
		collect(others)
	}
	return out
}

// Show returns one page file as it stood at a commit.
//
// It reads out of git rather than checking out, so the working tree and the
// index are left alone: restoring is then an ordinary write, and going back to
// an old version moves the page forward like any other edit instead of
// rewriting what is already published.
func (r *Repo) Show(file, ref string) ([]byte, error) {
	rel, err := filepath.Rel(r.root, filepath.Join(r.dir, filepath.Base(file)))
	if err != nil {
		return nil, err
	}
	out, err := run(r.root, "show", ref+":"+filepath.ToSlash(rel))
	if err != nil {
		return nil, err
	}
	return []byte(out), nil
}

// Entry is one commit touching a page.
type Entry struct {
	Hash    string
	Short   string
	When    time.Time
	Message string
}

// History returns the commits that touched one page's file, newest first.
func (r *Repo) History(file string, limit int) ([]Entry, error) {
	out, err := run(r.root,
		"log", fmt.Sprintf("-%d", limit), "--follow",
		"--format=%H%x00%h%x00%cI%x00%s", "--", filepath.Join(r.dir, filepath.Base(file)))
	if err != nil {
		return nil, err
	}
	var entries []Entry
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		parts := strings.Split(line, "\x00")
		if len(parts) != 4 {
			continue
		}
		when, _ := time.Parse(time.RFC3339, parts[2])
		entries = append(entries, Entry{Hash: parts[0], Short: parts[1], When: when, Message: parts[3]})
	}
	return entries, nil
}

func run(dir string, args ...string) (string, error) {
	return runFor(dir, 10*time.Second, args...)
}

func runFor(dir string, timeout time.Duration, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}
