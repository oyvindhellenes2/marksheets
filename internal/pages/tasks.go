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

// syncTasks keeps task pages in step with the tasks that own them: a task with
// text but no page gets one, and a task that has gone takes its page with it —
// but only while that page is still empty. A page with work in it is left
// alone rather than deleted behind your back.
func (s *Store) syncTasks(slug string, prev *Page, next *doc.Doc) (created, kept []string, err error) {
	for _, t := range next.TasksOf() {
		if t.Page != "" || strings.TrimSpace(t.Str("text")) == "" {
			continue
		}
		// A working file is part of the job the page it hangs off is about, so
		// it starts with that page's tags rather than one made from its own
		// name. Nobody would think to tag scratch space by hand.
		p, err := s.create(t.Str("text"), next.Tags, slug+"#"+t.ID)
		if err != nil {
			return created, kept, err
		}
		t.Page = p.Slug
		created = append(created, p.Slug)
	}

	if prev == nil || !prev.OK() {
		return created, kept, nil
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
				return created, kept, err
			}
			kept = append(kept, t.Page)
			continue
		}
		if err := s.remove(t.Page); err != nil && err != ErrNotFound {
			return created, kept, err
		}
	}
	return created, kept, nil
}
