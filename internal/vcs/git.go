// Package vcs commits page files to git, so every manual save is a point you
// can read and return to.
//
// Git is the historian here, never the database: the file is already written
// and safe before a commit is attempted, and a commit that fails is reported
// but never turns into a failed save.
package vcs

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

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
	if err := r.Commit(nil, "Fyrste versjon"); err != nil {
		return nil, err
	}
	return r, nil
}

// Root is the repository root.
func (r *Repo) Root() string { return r.root }

// Commit stages the named files under the page folder and commits them.
// Passing no files stages the whole page folder, which is only used for the
// first commit after init.
//
// Only paths inside the page folder are ever staged — the app must not be able
// to commit anything else, least of all its own source.
func (r *Repo) Commit(files []string, message string) error {
	r.mu.Lock() // git's index takes one writer at a time
	defer r.mu.Unlock()

	args := []string{"add", "--"}
	if len(files) == 0 {
		args = append(args, r.dir)
	} else {
		for _, f := range files {
			args = append(args, filepath.Join(r.dir, filepath.Base(f)))
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
	if !r.hasIdentity() {
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
			commit = append(commit, filepath.Join(r.dir, filepath.Base(f)))
		}
	}
	if _, err := run(r.root, commit...); err != nil {
		return fmt.Errorf("git commit: %w", err)
	}
	return nil
}

// hasIdentity reports whether git can work out who is committing, from any of
// its own sources — repo config, global config or environment.
func (r *Repo) hasIdentity() bool {
	name, err := run(r.root, "config", "--get", "user.name")
	if err != nil || strings.TrimSpace(name) == "" {
		return false
	}
	email, err := run(r.root, "config", "--get", "user.email")
	return err == nil && strings.TrimSpace(email) != ""
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
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}
