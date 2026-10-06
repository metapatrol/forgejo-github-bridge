// Package ui serves the embedded console (docs/console.md).
//
// The Angular application in ui/ builds into dist/console (make ui-build).
// Only dist/.gitkeep is committed, so the package always compiles; a binary
// built without the UI serves the API and says that no UI is built. The image
// and the release build the UI first.
package ui

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"regexp"
	"strings"
)

//go:embed all:dist
var dist embed.FS

// Built returns the embedded UI, or nil when the binary was built without
// one.
func Built() fs.FS {
	sub, err := fs.Sub(dist, "dist/console")
	if err != nil {
		return nil
	}
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		return nil
	}
	return sub
}

// fingerprinted matches what the Angular build names after its content.
var fingerprinted = regexp.MustCompile(`-[A-Za-z0-9_-]{8}\.(js|css)$`)

// Handler serves files, and index.html for every path that is not one, so
// that the application's own routes survive a reload. The build fingerprints
// its assets, which may therefore be cached; index.html may not.
func Handler(files fs.FS) http.Handler {
	server := http.FileServerFS(files)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if name == "" {
			name = "index.html"
		}
		if st, err := fs.Stat(files, name); err != nil || st.IsDir() {
			name = "index.html"
		}
		if name == "index.html" {
			w.Header().Set("Cache-Control", "no-cache")
			http.ServeFileFS(w, r, files, "index.html")
			return
		}
		if fingerprinted.MatchString(name) {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		server.ServeHTTP(w, r)
	})
}
