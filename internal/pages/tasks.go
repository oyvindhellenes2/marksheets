package pages

import (
	"strings"

	"marksheets/internal/doc"
)

// TaskState is what the editor needs to know about one task-todo: where its
// working file is, and whether that file holds anything. Deleting a task is
// only allowed while its page is still empty, and the editor enforces that
// before the deletion ever reaches a save.
type TaskState struct {
	Page  string `json:"page"`
	Title string `json:"title"`
	Empty bool   `json:"empty"`
	Lines int    `json:"lines"`
}

// TaskStates maps task-todo node ids to the state of their pages.
func (s *Store) TaskStates(d *doc.Doc) map[string]TaskState {
	out := map[string]TaskState{}
	for _, t := range d.TasksOf() {
		if t.Page == "" {
			continue
		}
		p, err := s.load(t.Page)
		if err != nil || !p.OK() {
			continue
		}
		out[t.ID] = TaskState{
			Page:  t.Page,
			Title: p.Title,
			Empty: p.Doc.IsEmpty(s.reg),
			Lines: p.Doc.Count(),
		}
	}
	return out
}

// syncTasks keeps task pages in step with the tasks that own them: a task that
// has gone takes its page with it — but only while that page is still empty. A
// page with work in it is left alone rather than deleted behind your back.
//
// It does not *create* anything. Saving used to open a working file for every
// task that had text, which meant writing a task created a page whether or not
// anybody ever went to it — twenty empty files, one per passing thought
// ([ADR-0025]). A page is made when somebody follows the arrow to it, by
// OpenTask below.
func (s *Store) syncTasks(prev *Page, next *doc.Doc) (kept []string, err error) {
	if prev == nil || !prev.OK() {
		return kept, nil
	}
	alive := map[string]bool{}
	for _, t := range next.TasksOf() {
		if t.Page != "" {
			alive[t.Page] = true
		}
	}
	for _, t := range prev.Doc.TasksOf() {
		if t.Page == "" || alive[t.Page] {
			continue
		}
		p, err := s.load(t.Page)
		if err != nil || !p.OK() {
			continue
		}
		if !p.Doc.IsEmpty(s.reg) {
			// The task is gone but its working file holds real work. Deleting
			// it would destroy that; keeping it as a task page would strand it,
			// since a task page is reached through its task and nowhere else.
			// So it graduates: it loses its parent and becomes an ordinary
			// page, listed on the front page like any other.
			p.Doc.Parent = ""
			if err := s.write(t.Page, p.Doc); err != nil {
				return kept, err
			}
			kept = append(kept, t.Page)
			continue
		}
		if err := s.remove(t.Page); err != nil && err != ErrNotFound {
			return kept, err
		}
	}
	return kept, nil
}

// OpenTask is the working file for one task, made now if it does not exist.
//
// This is the moment a task page comes into being: somebody followed the arrow
// to it. Creating one on save instead — which is what this replaces — meant a
// task you wrote and never opened still left a file behind, and twenty of them
// had piled up before anybody counted ([ADR-0025]).
//
// It is idempotent. A task that already has a page gets that page back, so a
// double click, a retry, or two people pressing at once cannot make a second
// working file for one task.
//
// The author is whoever followed the arrow, and it goes to the first todo on
// the new working file — the same rule an ordinary page gets, so the first line
// of a page is never the one line nobody is down for. Not the task's owner,
// tempting as that is: the owner of a job and the person breaking it into
// steps are only usually the same person, and the second is the one who is
// here. It is a starting point either way, changed by clicking the name.
func (s *Store) OpenTask(slug, nodeID, author string) (*Page, error) {
	s.writeMu.Lock() // the read, the decision and the write are one act
	defer s.writeMu.Unlock()

	parent, err := s.load(slug)
	if err != nil {
		return nil, err
	}
	if !parent.OK() {
		return nil, ErrNotFound
	}

	var task *doc.Node
	for _, t := range parent.Doc.TasksOf() {
		if t.ID == nodeID {
			task = t
			break
		}
	}
	if task == nil {
		return nil, ErrNotFound
	}
	if task.Page != "" {
		// Already opened. Return what is there rather than making another —
		// and rather than failing, because the caller's question was "where do
		// I go", and there is an answer.
		if p, err := s.load(task.Page); err == nil && p.OK() {
			return p, nil
		}
		// The page is named but gone — deleted by hand, or lost in a restore.
		// Fall through and make a new one under the same task.
	}

	// A task with nothing written in it has no name to give its page, and no
	// job for the page to be about. The editor does not offer the arrow in
	// that state; this is the same rule, held where it cannot be skipped.
	title := strings.TrimSpace(task.Str("text"))
	if title == "" {
		return nil, ErrEmptyTask
	}

	// A working file is part of the job the page it hangs off is about, so it
	// starts with that page's tags rather than one made from its own name.
	// Nobody would think to tag scratch space by hand.
	p, err := s.create(title, parent.Doc.Tags, slug+"#"+nodeID, author)
	if err != nil {
		return nil, err
	}

	// The link back, on the task itself. Written straight to disk rather than
	// through Save: this is the store's own bookkeeping, not an edit somebody
	// made, and it must not be refused as a stale save by an editor that is
	// still holding the version it loaded.
	task.Page = p.Slug
	if err := s.write(slug, parent.Doc); err != nil {
		return nil, err
	}
	return p, nil
}

// numberTasks gives every task on a page a number that will not move.
//
// The numbers exist to be referred to out loud — "look at task 4" — so what
// matters is not that they are tidy but that they stay put. A number is given
// out once, written onto the node, and never given out again on that page:
// reordering the list leaves each number where it was, and deleting task 2
// leaves a gap rather than pulling 3 down into it. Counting positions instead
// would be less code and would make every reference wrong the moment somebody
// tidied the list, which is the one thing it must not do.
//
// The counter is Doc.TaskSeq: the highest number ever used here, which is not
// the same as the highest still present. Deriving it from the tasks on the page
// instead only remembers as far back as the highest surviving number — delete
// the top task, save twice, and its number is quietly free again. Store.Save
// carries the counter across from disk the way it carries Parent, so the editor
// never sends it and cannot lose it.
//
// Tasks written before numbering existed keep their zero. Backfilling them
// would be inventing an order nobody chose, and they are exactly the tasks
// somebody may already have referred to by position.
func numberTasks(next *doc.Doc) {
	high := next.TaskSeq
	// A file edited by hand may carry numbers higher than the counter — or
	// numbers with no counter at all, on a page from before this existed. The
	// tasks on the page are the other half of the high-water mark, never the
	// whole of it.
	forEachTask(next.Children, func(n *doc.Node) {
		if n.TaskNo > high {
			high = n.TaskNo
		}
	})
	forEachTask(next.Children, func(n *doc.Node) {
		// An empty line is a task nobody has written yet — every new page comes
		// with one from the template. Numbering it would spend a number on a
		// placeholder, and the number is meant to name something you can point
		// at. It gets one the moment it says anything.
		if n.TaskNo == 0 && strings.TrimSpace(n.Str("text")) != "" {
			high++
			n.TaskNo = high
		}
	})
	next.TaskSeq = high
}

// forEachTask visits the task lines of a document in the order they are written.
//
// Children only, never Items: a sub-line of a task is part of that task, not a
// task of its own, and numbering them would put a second sequence inside the
// first.
func forEachTask(nodes []*doc.Node, fn func(*doc.Node)) {
	for _, n := range nodes {
		if n.Type == "task" || n.Type == "todo" {
			fn(n)
		}
		forEachTask(n.Children, fn)
	}
}
