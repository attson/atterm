package main

import (
	"bufio"
	"context"
	"encoding/json"
	"io/fs"
	"net/http"
	"os"
	"sync"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// Companion window geometry, in logical pixels.
//
// Only the width is fixed. The height is reported by the frontend from the
// rendered card (PetApp.vue's ResizeObserver → Resize) rather than hardcoded
// here: it depends on the row count, the font, and the locale's line
// wrapping, so any constant is wrong for most states. Hardcoding it clipped
// the card's bottom edge — rounded corner included — whenever the guess came
// in under the real height.
//
// petHeightInitial is only the pre-measurement size, chosen to be close
// enough that the first frame does not visibly jump.
const (
	petWidth         = 252
	petHeightInitial = 172
	// petMaxHeight bounds what the frontend can ask for, so a rendering bug
	// cannot grow an always-on-top window over the whole screen.
	petMaxHeight = 900
	// petScreenMargin insets the default bottom-right placement so the window
	// does not butt against the screen edge (or sit under a macOS Dock).
	petScreenMargin = 24
)

// PetBridge is the Wails-bound API for the companion window's frontend.
//
// It is deliberately tiny: the pet renders whatever state arrives on stdin and
// reports user intent back on stdout. It holds no session handles, no relay
// connection, and no credentials.
type PetBridge struct {
	ctx context.Context

	mu        sync.Mutex
	collapsed bool
	// ready flips once the webview has mounted and subscribed to events.
	//
	// Wails events emitted before that are dropped, and the parent writes both
	// the bootstrap line and the first state snapshot immediately after spawn
	// — long before the webview exists. Without parking them, the pet ignored
	// the persisted collapsed preference and sat on "连接中…" until the next
	// session-list change, which can be minutes.
	ready        bool
	pendingBoot  *petBootstrap
	pendingState string

	// outMu serializes writes to stdout; interleaved NDJSON would be
	// undecodable on the parent side.
	outMu sync.Mutex
}

// NewPetBridge builds the bridge for the pet process.
func NewPetBridge() *PetBridge { return &PetBridge{} }

func (p *PetBridge) startup(ctx context.Context) {
	p.ctx = ctx
	// Must run here, not before wails.Run: Wails sets
	// NSApplicationActivationPolicyRegular during its own startup, which
	// undoes anything set earlier — the symptom is the pet owning the menu
	// bar and a Dock tile.
	applyPetPostStartup(ctx)
	go p.readStdin()
}

// readStdin forwards parent→child NDJSON into webview events.
//
// EOF means the parent is gone — including the case where it was SIGKILLed and
// never got to kill us. Exiting here is what prevents an orphaned always-on-top
// window from outliving AT Term.
func (p *PetBridge) readStdin() {
	sc := bufio.NewScanner(os.Stdin)
	// A PetState with PET_MAX_ROWS rows is a few KB; allow generous headroom
	// but stay bounded.
	sc.Buffer(make([]byte, 0, 8192), 256*1024)
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			continue
		}
		var probe struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal([]byte(line), &probe); err != nil {
			continue
		}
		if p.ctx == nil {
			continue
		}
		switch probe.Type {
		case "bootstrap":
			var boot petBootstrap
			if err := json.Unmarshal([]byte(line), &boot); err != nil {
				continue
			}
			p.mu.Lock()
			ready := p.ready
			if !ready {
				p.pendingBoot = &boot
			}
			p.mu.Unlock()
			if ready {
				p.applyBootstrap(boot)
			}
		default:
			// Anything else is a PetState snapshot; hand the raw JSON to the
			// frontend so Go never has to mirror the projection's shape.
			p.mu.Lock()
			ready := p.ready
			if !ready {
				// Keep only the newest: each snapshot is complete, so an
				// older one carries nothing the newer one lacks.
				p.pendingState = line
			}
			p.mu.Unlock()
			if ready {
				wailsruntime.EventsEmit(p.ctx, "pet:state", line)
			}
		}
	}
	// Parent pipe closed.
	if p.ctx != nil {
		wailsruntime.Quit(p.ctx)
		return
	}
	os.Exit(0)
}

// Ready is called by the frontend once PetApp has mounted and subscribed.
// It replays whatever arrived while the webview was still starting.
func (p *PetBridge) Ready() {
	p.mu.Lock()
	p.ready = true
	boot := p.pendingBoot
	state := p.pendingState
	p.pendingBoot, p.pendingState = nil, ""
	p.mu.Unlock()

	// State first, so the window is already showing real content by the time
	// bootstrap makes it visible — no flash of the "连接中…" placeholder.
	if state != "" {
		wailsruntime.EventsEmit(p.ctx, "pet:state", state)
	}
	if boot != nil {
		p.applyBootstrap(*boot)
		return
	}
	// Launched without a parent bootstrap (e.g. `--pet` by hand): still show
	// something rather than leaving an invisible process running.
	p.placeBottomRight()
	wailsruntime.WindowShow(p.ctx)
}

func (p *PetBridge) applyBootstrap(boot petBootstrap) {
	p.mu.Lock()
	p.collapsed = boot.Collapsed
	p.mu.Unlock()

	if boot.X >= 0 || boot.Y >= 0 {
		wailsruntime.WindowSetPosition(p.ctx, boot.X, boot.Y)
	} else {
		p.placeBottomRight()
	}
	// No height applied here — the frontend measures the card and calls
	// Resize once it has rendered the collapsed/expanded state.
	wailsruntime.EventsEmit(p.ctx, "pet:bootstrap", boot)
	wailsruntime.WindowShow(p.ctx)
}

// placeBottomRight puts a never-positioned pet near the bottom-right of the
// primary display — the corner least likely to cover what the user is reading.
func (p *PetBridge) placeBottomRight() {
	screens, err := wailsruntime.ScreenGetAll(p.ctx)
	if err != nil || len(screens) == 0 {
		return
	}
	primary := screens[0]
	for _, s := range screens {
		if s.IsPrimary {
			primary = s
			break
		}
	}
	// Size is the logical-pixel screen size, which is the space
	// WindowSetPosition works in. Screen.Width/Height are deprecated and
	// platform-dependent — on a HiDPI display they can be physical pixels,
	// which would place the window far off the bottom-right corner.
	w, h := primary.Size.Width, primary.Size.Height
	if w <= 0 || h <= 0 {
		w, h = primary.Width, primary.Height
	}
	x := w - petWidth - petScreenMargin
	y := h - petHeightInitial - petScreenMargin*3
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	wailsruntime.WindowSetPosition(p.ctx, x, y)
}

// Resize sets the window to the height the frontend measured for the rendered
// card. Called from a ResizeObserver, so it fires for collapse, expand, peek,
// and any row-count change without Go having to model those states.
func (p *PetBridge) Resize(height int) {
	h := clampPetHeight(height)
	if h == 0 {
		return
	}
	// Info rather than Debug: the pet branches out of main() before any
	// --log-level parsing, so Debug is always below threshold here. Window
	// geometry is otherwise unobservable from outside the process (no
	// titlebar to read, and screen-capture APIs need permissions a terminal
	// usually lacks), and this only fires on collapse/expand and row-count
	// changes.
	logInfo("pet", "resize to %dx%d (requested %d)", petWidth, h, height)
	wailsruntime.WindowSetSize(p.ctx, petWidth, h)
}

// clampPetHeight returns the height to apply, or 0 for "ignore this value".
// Split out from Resize so the policy is testable without a live window.
func clampPetHeight(height int) int {
	if height <= 0 {
		return 0
	}
	if height > petMaxHeight {
		return petMaxHeight
	}
	return height
}

// emit writes one child→parent event.
func (p *PetBridge) emit(ev petEvent) {
	p.outMu.Lock()
	defer p.outMu.Unlock()
	blob, err := json.Marshal(ev)
	if err != nil {
		return
	}
	_, _ = os.Stdout.Write(append(blob, '\n'))
}

// Activate asks the main app to focus a session. Bound to the frontend.
func (p *PetBridge) Activate(sessionID string) {
	p.emit(petEvent{Type: "activate", SessionID: sessionID})
}

// SetCollapsed persists the choice via the parent. The window resize follows
// from the DOM change, through the frontend's ResizeObserver → Resize.
func (p *PetBridge) SetCollapsed(collapsed bool) {
	p.mu.Lock()
	p.collapsed = collapsed
	p.mu.Unlock()
	p.emit(petEvent{Type: "collapse", Collapsed: collapsed})
}

// ReportPosition persists the window position after a drag.
func (p *PetBridge) ReportPosition() {
	x, y := wailsruntime.WindowGetPosition(p.ctx)
	p.emit(petEvent{Type: "move", X: x, Y: y})
}

// Mute suppresses attention animations until the given unix second.
func (p *PetBridge) Mute(untilUnix int64) {
	p.emit(petEvent{Type: "mute", MutedUntilUnix: untilUnix})
}

// Hide turns the plugin off from the pet's own context menu.
func (p *PetBridge) Hide() {
	p.emit(petEvent{Type: "hide"})
}

// Dragging itself is handled by Wails' `--wails-draggable: drag` CSS on the
// pet header (same mechanism as TitleBar.vue) — there is no Go-side drag API
// in v2. The frontend calls ReportPosition once the drag ends so the new
// position reaches config.

// petEntryRewrite serves the pet entry document at the webview's root.
//
// Both windows share one embedded asset tree so they also share the hashed
// /assets/* chunks; only the entry HTML differs. Rewriting here beats emitting
// the pet build into its own directory, which would cut it off from those
// shared chunks.
func petEntryRewrite(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" || r.URL.Path == "/index.html" {
			r = r.Clone(r.Context())
			r.URL.Path = "/" + petEntryDocument
		}
		next.ServeHTTP(w, r)
	})
}

// petEntryDocument is the filename vite emits for the pet entry (see
// desktop/frontend/vite.config.ts).
const petEntryDocument = "index.pet.html"

// runPetWindow is the --pet entry point. It never touches config files, the
// keychain, the relay, or the PTY host: everything it renders arrives on
// stdin, and everything it wants goes out on stdout.
func runPetWindow(assets fs.FS) error {
	bridge := NewPetBridge()

	opts := &options.App{
		Title:  "AT Term Pet",
		Width:  petWidth,
		Height: petHeightInitial,
		// StartHidden avoids a flash at the default position before the
		// bootstrap line tells us where the user last left the pet.
		StartHidden:   true,
		Frameless:     true,
		AlwaysOnTop:   true,
		DisableResize: true,
		// Fully transparent so the pet's rounded card floats over whatever is
		// behind it instead of sitting in a grey rectangle.
		BackgroundColour: &options.RGBA{R: 0, G: 0, B: 0, A: 0},
		AssetServer: &assetserver.Options{
			Assets:     assets,
			Middleware: petEntryRewrite,
		},
		OnStartup: bridge.startup,
		Bind:      []interface{}{bridge},
	}
	applyPetPlatformOptions(opts)

	return wails.Run(opts)
}
