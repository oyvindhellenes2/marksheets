package main

import (
	"embed"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"marksheets/internal/auth"
	"marksheets/internal/doc"
	"marksheets/internal/pages"
	"marksheets/internal/preview"
	"marksheets/internal/server"
	"marksheets/internal/share"
	"marksheets/internal/users"
	"marksheets/internal/vcs"
)

//go:embed templates
var templates embed.FS

//go:embed static
var static embed.FS

// beside puts a file next to the page folder, never inside it. The page folder
// is a git repository with a remote, and neither an email address nor a session
// is something to publish by accident — and a `.json` file in there would be
// counted as a page besides.
func beside(pagesDir, name string) string {
	abs, err := filepath.Abs(pagesDir)
	if err != nil {
		return name
	}
	return filepath.Join(filepath.Dir(abs), name)
}

// usersPath is where the list of people is kept.
func usersPath(pagesDir string) string {
	if p := os.Getenv("USERS_PATH"); p != "" {
		return p
	}
	return beside(pagesDir, "brukarar.json")
}

// sharesPath is where the public share links are kept.
func sharesPath(pagesDir string) string {
	if p := os.Getenv("SHARES_PATH"); p != "" {
		return p
	}
	return beside(pagesDir, "deling.json")
}

// previewsPath is where what has been read off other people's websites is kept
// — titles and pictures for the links written on a page. Beside the pages and
// never inside: the page folder is pushed, and a list of every address anybody
// has looked at is not something to publish by accident.
func previewsPath(pagesDir string) string {
	if p := os.Getenv("PREVIEWS_PATH"); p != "" {
		return p
	}
	return beside(pagesDir, "lenkjer.json")
}

func main() {
	// PAGES_DIR is the folder holding one JSON file per page. The files are
	// the only copy of your documents — back that folder up, or keep it in git.
	pagesDir := os.Getenv("PAGES_DIR")
	if pagesDir == "" {
		pagesDir = "pages"
	}

	// TYPES_PATH points at a types.json to override the built-in templates.
	// Editing that file is how line types are customised.
	types, err := doc.LoadTypes(os.Getenv("TYPES_PATH"))
	if err != nil {
		log.Fatalf("types: %v", err)
	}
	log.Printf("line types loaded from: %s", types.Source)

	store, err := pages.NewStore(pagesDir, types)
	if err != nil {
		log.Fatalf("pages: %v", err)
	}
	log.Printf("pages stored in: %s", store.Dir())

	// History is optional: if the page folder is not in a repository, the app
	// runs without it and offers to start one.
	repo, ok := vcs.Open(store.Dir())
	if ok {
		log.Printf("git history in: %s", repo.Root())
	} else {
		repo = nil
		log.Printf("no git repository around %s — history is off", store.Dir())
	}

	// Who may use this, and who each of them is. With no AUTH_ISSUER set the
	// app runs as one local user, exactly as it did before there was any of
	// this — see internal/auth.
	cfg := auth.FromEnv()
	// Sessions outlive the process, so a deploy is no longer a login for
	// everybody ([ADR-0023]). Beside the pages for the same reason the user
	// list is: this one holds who is signed in until when.
	if cfg.Sessions == "" {
		cfg.Sessions = beside(pagesDir, "sesjonar.json")
	}
	people, err := users.Open(usersPath(pagesDir))
	if err != nil {
		log.Fatalf("users: %v", err)
	}
	log.Printf("users kept in: %s", people.Path())
	log.Printf("sessions kept in: %s", cfg.Sessions)

	// The public share links. Beside the pages like the other two, and outside
	// them for a sharper reason than either: a token in the page folder would be
	// pushed to the remote, and a token *is* the way in to the page it names.
	shares, err := share.Open(sharesPath(pagesDir))
	if err != nil {
		log.Fatalf("share links: %v", err)
	}
	log.Printf("share links kept in: %s", shares.Path())

	// Titles and pictures read off the sites people link to. The only part of
	// this app that reaches outside the folder; see internal/preview.
	cards := preview.New(previewsPath(pagesDir))
	log.Printf("link previews kept in: %s", previewsPath(pagesDir))

	// What this wiki calls itself. One binary serves more than one of them now,
	// so the name is configuration rather than a constant in the templates.
	srv := server.New(templates, static, store, types, repo, auth.New(cfg, people), people, shares,
		cards, os.Getenv("SITE_NAME"))

	port := os.Getenv("PORT")
	if port == "" {
		port = "3003"
	}

	log.Printf("Starting server on http://localhost:%s", port)
	if err := http.ListenAndServe(":"+port, canonical(srv.Routes())); err != nil {
		log.Fatal(err)
	}
}

// canonical sends every request that arrives on an old address to the address
// this archive is called by now.
//
// It exists because `wiki.verftet.info` became `arkiv.verftet.info` and the old
// name had been handed out — in share links, which are credentials with a
// fortnight on them, and in browser bookmarks. Turning the old hostname off
// would have broken both silently; a redirect keeps every one of them working.
//
// `CANONICAL_HOST` is empty by default and the wrapper then does nothing at
// all. That is deliberate: switching the hostname is a three-step move — add
// the address, teach the identity provider its new callback, and only then send
// people to it — and this is the third step, waiting to be turned on. Turning it
// on before the provider knows the new callback would leave everybody unable to
// sign in.
//
// 308 rather than 301: it is the one that promises the method and the body
// survive, so a save that lands on the old name is still a save.
func canonical(next http.Handler) http.Handler {
	want := strings.TrimSpace(os.Getenv("CANONICAL_HOST"))
	if want == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		// Behind the tunnel every request arrives over plain HTTP, so the
		// scheme is not something to read off the request. The address people
		// were sent is https, and that is what they are sent on to.
		if host == "" || strings.EqualFold(host, want) {
			next.ServeHTTP(w, r)
			return
		}
		to := "https://" + want + r.URL.RequestURI()
		http.Redirect(w, r, to, http.StatusPermanentRedirect)
	})
}
