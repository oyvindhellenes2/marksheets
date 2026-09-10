package server

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"marksheets/internal/doc"
	"marksheets/internal/pages"
)

// The archive's half of Fjernmøte.
//
// Fjernmøte (`fjernmote.verftet.info`) runs the video meetings, and a meeting
// belongs to the working document of a task: its address *is* that document's
// slug ([ADR-0030](../../adr/0030-a-meeting-belongs-to-a-working-document.md)).
// Two things follow, and they are the whole of this file.
//
//   - The Del-panel offers the meeting address on a working document, so
//     starting a meeting is one press on the document about the job.
//   - When the meeting is over, Fjernmøte writes what came of it — the referat,
//     the suggestions, a link to the recording, and the transcript as an
//     attachment — onto that same document.
//
// The second is a machine calling this app, which is a new kind of caller here.
// It carries a shared token and nothing else: no session, no cookie, no user.
// `FJERNMOTE_TOKEN` unset turns the whole thing off, which is what the private
// archive at arkiv.hellenes.it does.

// fjernmoteToken is the shared secret, read on the request rather than at boot
// so that setting it does not need a restart. It is compared in constant time
// by `subtle` — a token compared with `==` leaks its length and its prefix to
// anybody patient.
func fjernmoteToken() string { return strings.TrimSpace(os.Getenv("FJERNMOTE_TOKEN")) }

// FjernmoteURL is where the meetings live, e.g. https://fjernmote.verftet.info.
// Empty is the ordinary state of an archive that has no meetings attached, and
// the Del-panel then says nothing about them.
func FjernmoteURL() string { return strings.TrimRight(os.Getenv("FJERNMOTE_URL"), "/") }

// fjernmoteRequest reports whether a request is Fjernmøte's, and is the *only*
// place that says so.
//
// It is asked from `publicRequest`, which is the one hook `auth.Middleware`
// consults ([ADR-0024]). That is deliberate and worth defending: the rule this
// archive keeps is that there is a single function saying what may pass without
// a session, so there is a single place to read and a single place to change.
// Adding `/api/` to the middleware's own prefix list instead would put the same
// decision somewhere it cannot be justified, and it would drift.
//
// This is not "public" in the sense the share links are. A share link is a
// credential somebody chose to hand out; this is a service on the same machine
// proving it holds a secret nobody has ever typed.
func fjernmoteRequest(r *http.Request) bool {
	want := fjernmoteToken()
	if want == "" {
		return false
	}
	if !strings.HasPrefix(r.URL.Path, "/api/fjernmote/") {
		return false
	}
	got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok {
		return false
	}
	return constantEqual(strings.TrimSpace(got), want)
}

// ------------------------------------------------------------------ reading

// handleMeetingDoc answers what Fjernmøte needs before it will open a room:
// what this document is called, and whether it is a working document at all.
//
// The second is the gate. A meeting is about a job, and a document that is not
// a task's working document has no job for it to be about — so Fjernmøte
// refuses to open a room for one, and this is where it finds out.
func (s *Server) handleMeetingDoc(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	p, err := s.pages.BySlug(slug)
	if errors.Is(err, pages.ErrNotFound) || errors.Is(err, pages.ErrBadSlug) {
		http.NotFound(w, r)
		return
	}
	if err != nil || !p.OK() {
		http.Error(w, "dokumentet kan ikkje lesast", http.StatusUnprocessableEntity)
		return
	}

	out := map[string]any{
		"dokument":        p.Slug,
		"tittel":          p.Title,
		"arbeidsdokument": p.Parent != "",
	}
	// The task this document hangs off, so the meeting screen can say which
	// job it is about. A parent that has since been deleted is simply left
	// out: it is a label, and a missing label is better than an error.
	if slug, node, ok := strings.Cut(p.Parent, "#"); ok {
		out["forelder"] = slug
		if parent, err := s.pages.BySlug(slug); err == nil && parent.OK() {
			out["forelder-tittel"] = parent.Title
			parent.Doc.Walk(func(n *doc.Node, _ int) {
				if n.ID == node {
					out["oppgåve"] = n.Label()
				}
			})
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}

// ------------------------------------------------------------------ writing

// meetingIn is a finished meeting as Fjernmøte hands it over.
type meetingIn struct {
	ID      string    `json:"id"`
	Started time.Time `json:"start"`
	Ended   time.Time `json:"slutt"`
	Length  string    `json:"lengd"`
	By      string    `json:"av"`
	People  []string  `json:"deltakarar"`
	Audio   string    `json:"lyd"`

	Summary []string `json:"referat"`
	Points  []string `json:"innspel"`

	Transcript string   `json:"utskrift"`
	Voices     []string `json:"stemmer"`
	Unnamed    int      `json:"utan-namn"`
	Words      int      `json:"ord"`

	Note string `json:"merknad"`
}

// maxMeeting is the largest handover accepted. A transcript of a long meeting
// is a few hundred kilobytes of text; this is wide enough for a day of talking
// and narrow enough that a confused caller cannot fill the disk.
const maxMeeting = 8 << 20

// handleMeetingFile writes a finished meeting onto its working document.
//
// The shape of what is written is decided here and not by the caller. Fjernmøte
// sends facts — who was there, how long, what the model made of it — and this
// builds the lines, because what a document looks like is the archive's
// business and an app writing nodes in somebody else's format would break the
// first time that format changed.
//
// Three things about how it writes are worth keeping:
//
//   - **It appends and never rewrites.** The meeting goes on the end of the
//     document under a heading of its own, below whatever anybody has written
//     there. Nothing existing is touched.
//   - **It refuses the same meeting twice.** The id is in the heading's node,
//     and a second delivery of it is answered with where the first one went.
//     Fjernmøte retries on its own, and a retry after a reply that got lost is
//     the ordinary case rather than the strange one.
//   - **It saves with no version**, the way the restore path does — it is not
//     answering for anything it read, and the version it would carry is one it
//     took a moment earlier from the same store ([ADR-0021]).
func (s *Server) handleMeetingFile(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	p, err := s.pages.BySlug(slug)
	if errors.Is(err, pages.ErrNotFound) || errors.Is(err, pages.ErrBadSlug) {
		http.NotFound(w, r)
		return
	}
	if err != nil || !p.OK() {
		http.Error(w, "dokumentet kan ikkje lesast", http.StatusUnprocessableEntity)
		return
	}

	var in meetingIn
	if err := json.NewDecoder(io.LimitReader(r.Body, maxMeeting)).Decode(&in); err != nil {
		http.Error(w, "ugyldig møte: "+err.Error(), http.StatusBadRequest)
		return
	}
	if in.ID == "" {
		http.Error(w, "møtet manglar id", http.StatusBadRequest)
		return
	}

	if node, ok := meetingAlready(p.Doc, in.ID); ok {
		// Already here. Answering with where it went, rather than with an
		// error, is what lets the caller treat a lost reply and a successful
		// one the same way.
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"node":    node,
			"adresse": "/p/" + slug + "#" + node,
		})
		return
	}

	// The transcript becomes an attachment rather than lines on the page. An
	// hour of talk is tens of thousands of words, and a working document that
	// somebody has to scroll past a machine transcript to read is a document
	// nobody reads. It is still in the folder, still published, still found by
	// a search — search here is a scan of the files ([ADR-0018]).
	var file string
	if strings.TrimSpace(in.Transcript) != "" {
		stored, _, err := s.pages.SaveFile(
			"utskrift-"+slug+"-"+in.ID+".txt",
			strings.NewReader(in.Transcript))
		if err != nil {
			// Not fatal. The referat is the part somebody asked for, and a
			// meeting filed without its transcript is better than one not
			// filed at all.
			log.Printf("fjernmøte: could not store the transcript for %s/%s: %v", slug, in.ID, err)
		} else {
			file = stored
		}
	}

	nodes, head := meetingNodes(s.types, in, file)
	next := *p.Doc
	next.Children = append(append([]*doc.Node(nil), p.Doc.Children...), nodes...)

	// `by` is empty: nobody in particular saved this. The same thing a restore
	// says, and it is true — a machine wrote it.
	if _, err := s.pages.Save(slug, &next, "", ""); err != nil {
		log.Printf("fjernmøte: saving %s: %v", slug, err)
		http.Error(w, "kunne ikkje skrive møtet inn i dokumentet", http.StatusInternalServerError)
		return
	}
	log.Printf("fjernmøte: filed meeting %s on %s", in.ID, slug)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"node":    head,
		"adresse": "/p/" + slug + "#" + head,
	})
}

// meetingMark is what a meeting's heading carries so the same one is never
// filed twice. It is in the heading's own text rather than in a field of its
// own, because a field on a header is not something the editor would keep — the
// type has one field and it is the text.
const meetingMark = "fjernmøte:"

// meetingAlready looks for a meeting that has already been written here, and
// answers with its *heading* rather than with the stamp inside it.
//
// The heading is what the first delivery answered with, and the two have to
// agree: a caller that stored an address on the first attempt and got a
// different one on a retry would have two addresses for one meeting, which is
// the confusion this whole check exists to prevent.
func meetingAlready(d *doc.Doc, id string) (string, bool) {
	want := meetingMark + id
	for _, head := range d.Children {
		for _, n := range head.Children {
			if n.Type == "comment" && strings.Contains(n.Str("text"), want) {
				return head.ID, true
			}
		}
	}
	return "", false
}

// meetingNodes builds what goes on the page.
//
// The order is what somebody reading the document a month later wants: what it
// was, then the referat, then the suggestions, then where to hear it. The
// machine's own note about what it could not do goes at the top of the section
// rather than the bottom, because a missing referat is the thing a reader will
// otherwise spend a minute wondering about.
func meetingNodes(reg *doc.Registry, in meetingIn, file string) ([]*doc.Node, string) {
	head := &doc.Node{
		ID:     doc.NewID(),
		Type:   "header",
		Fields: map[string]any{"text": "Fjernmøte " + norskDato(in.Started)},
	}

	line := func(t, text string, extra map[string]any) *doc.Node {
		f := map[string]any{"text": text}
		for k, v := range extra {
			f[k] = v
		}
		return &doc.Node{ID: doc.NewID(), Type: t, Fields: f}
	}

	var kids []*doc.Node

	// The stamp. A comment rather than ordinary text: it is the app talking
	// about the document rather than part of it, which is what a comment is
	// for — and it is what stops the same meeting being filed twice.
	var facts []string
	if in.Length != "" {
		facts = append(facts, in.Length)
	}
	if len(in.People) > 0 {
		facts = append(facts, strings.Join(in.People, ", "))
	}
	if in.By != "" {
		facts = append(facts, "teke opp av "+in.By)
	}
	kids = append(kids, line("comment",
		strings.Join(facts, " · ")+" · "+meetingMark+in.ID, nil))

	if in.Note != "" {
		kids = append(kids, line("callout", in.Note, map[string]any{"slag": "info"}))
	}

	for _, para := range in.Summary {
		kids = append(kids, line("text", para, nil))
	}
	if len(in.Summary) == 0 && in.Note == "" {
		kids = append(kids, line("text", "Ingen referat vart skrive av dette møtet.", nil))
	}

	if len(in.Points) > 0 {
		// Suggestions, written as list lines and not as tasks. A machine
		// reading of a conversation is a good prompt and a bad instruction, and
		// a task appears with somebody's name on it — which nobody agreed to
		// ([ADR-0031](../../adr/0031-what-a-meeting-suggests-is-not-a-task.md)).
		kids = append(kids, line("callout",
			"Innspel frå maskina, til vurdering:", map[string]any{"slag": "tips"}))
		for _, p := range in.Points {
			kids = append(kids, line("list", p, nil))
		}
	}

	if in.Audio != "" {
		kids = append(kids, line("text", "Lyd: "+in.Audio, nil))
	}
	if file != "" {
		kids = append(kids, &doc.Node{
			ID:   doc.NewID(),
			Type: "file",
			Fields: map[string]any{
				"file": file,
				"name": transcriptLabel(in),
			},
		})
	}

	head.Children = kids
	return []*doc.Node{head}, head.ID
}

// norskDato is the date as it is written on the page. Go's month names are
// English, and this is the one place a machine writes into somebody's document.
func norskDato(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	months := [...]string{"januar", "februar", "mars", "april", "mai", "juni",
		"juli", "august", "september", "oktober", "november", "desember"}
	return fmt.Sprintf("%d. %s %d", t.Day(), months[int(t.Month())-1], t.Year())
}

// constantEqual compares two secrets without leaking their length or their
// prefix through how long the comparison took. `==` on a token is the kind of
// thing that is fine until somebody is patient.
func constantEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// meetingURL is where a meeting about this document happens, or "" — which is
// what an archive with no Fjernmøte attached says, and what every document that
// is not a working document says.
//
// It is derived rather than stored: the address of a meeting *is* the slug of
// the document, so there is nothing to keep, nothing to mint and nothing that
// can be minted twice ([ADR-0030]).
func meetingURL(p *pages.Page) string {
	base := FjernmoteURL()
	if base == "" || p == nil || p.Parent == "" {
		return ""
	}
	return base + "/m/" + url.PathEscape(p.Slug)
}

// transcriptLabel is what the attachment is called on the page.
//
// It says whether the transcript names who is speaking, because that changes
// how it should be read. The names come from Fjernmøte's record of who was
// making a sound when, matched to the transcript on time — so **the names
// themselves are certain**, taken from a session rather than from a voice, and
// it is the boundaries between turns that are approximate.
//
// When most of it could not be attributed the label says that instead of
// implying an attribution that is mostly missing.
func transcriptLabel(in meetingIn) string {
	base := fmt.Sprintf("Utskrift av møtet (%d ord", in.Words)
	switch {
	case len(in.Voices) == 0:
		return base + ", utan namn på kven som snakkar)"
	case in.Unnamed > 2*len(in.Voices) && in.Unnamed > 10:
		return base + fmt.Sprintf(", %s namngjevne — %d linjer utan namn)",
			strings.Join(in.Voices, " og "), in.Unnamed)
	default:
		return base + ", med " + strings.Join(in.Voices, " og ") + ")"
	}
}
