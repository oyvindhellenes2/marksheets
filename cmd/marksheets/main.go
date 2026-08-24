package main

import (
	"embed"
	"log"
	"net/http"
	"os"

	"marksheets/internal/doc"
	"marksheets/internal/pages"
	"marksheets/internal/server"
	"marksheets/internal/vcs"
)

//go:embed templates
var templates embed.FS

//go:embed static
var static embed.FS

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

	srv := server.New(templates, static, store, types, repo)

	port := os.Getenv("PORT")
	if port == "" {
		port = "3003"
	}

	log.Printf("Starting server on http://localhost:%s", port)
	if err := http.ListenAndServe(":"+port, srv.Routes()); err != nil {
		log.Fatal(err)
	}
}
