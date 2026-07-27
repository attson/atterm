// Boots the desktop-layout app for the web target.
//
// This indirection exists because Vite cannot resolve a `<script
// type="module" src="...">` in index.html that escapes the project root via
// relative ".." segments — both the browser's URL resolution and Vite's own
// devHtmlHook clamp the path at the server root ("/"), so
// `src="../desktop/frontend/src/main.web.ts"` collapses to
// "/desktop/frontend/src/main.web.ts" and 404s. A same-origin *module
// import*, by contrast, is resolved by Vite's plugin container against the
// real filesystem (see `resolve.alias['@']` / `server.fs.allow` in
// vite.config.ts) and correctly crosses outside the project root via its
// `/@fs/` mechanism.
import '../../desktop/frontend/src/main.web.ts'
