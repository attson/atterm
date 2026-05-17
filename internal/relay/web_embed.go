package relay

import (
	"embed"
	"io/fs"
)

//go:embed all:web-dist
var embeddedWeb embed.FS

// EmbeddedWebFS returns the relay-bundled static web assets rooted at the
// repository's web/ output (built by scripts/build-web.sh).
//
// During PR-A the contents mirror web/legacy/ byte-for-byte; later PRs will
// replace individual entries with Vite output.
func EmbeddedWebFS() fs.FS {
	sub, err := fs.Sub(embeddedWeb, "web-dist")
	if err != nil {
		// fs.Sub only errors on a malformed name; "web-dist" is a constant.
		panic(err)
	}
	return sub
}
