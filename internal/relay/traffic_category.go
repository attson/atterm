package relay

import "github.com/attson/atterm/internal/proto"

// Traffic semantic categories. Kept as a single source of truth so the
// admin "group" view and any future consumer classify frames identically.
const (
	catTerminal = "terminal" // PTY byte transport: keystrokes, output, resize, paste
	catState    = "state"    // session lifecycle + metadata sync
	catConfig   = "config"   // preferences / identity sync
	catFS       = "fs"       // remote file explorer
	catPreview  = "preview"  // remote web preview control
	catOther    = "other"    // unclassified / future frame types
)

// frameCategory maps a wire frame type to its semantic traffic category.
// Every proto.Type defined today has an explicit case; new types fall
// through to catOther until classified (guarded by an exhaustive test).
func frameCategory(t proto.Type) string {
	switch t {
	case proto.TypeIn, proto.TypeOut, proto.TypeResize,
		proto.TypePasteImage, proto.TypePasteFile:
		return catTerminal
	case proto.TypeMeta, proto.TypeList, proto.TypeListResp,
		proto.TypeReplayProgress, proto.TypeAnnounce, proto.TypeViewers,
		proto.TypeOpen, proto.TypeClose, proto.TypeStreamRequest,
		proto.TypeStreamStop, proto.TypeCommandEvent, proto.TypeSessionCreate,
		proto.TypeSessionCreated, proto.TypeAttach, proto.TypeClaimDriver,
		proto.TypePing, proto.TypePong:
		return catState
	case proto.TypePrefsChanged, proto.TypeAuthInfo:
		return catConfig
	case proto.TypeFSRequest, proto.TypeFSResponse, proto.TypeFSEvent:
		return catFS
	case proto.TypeServiceOpen, proto.TypeServiceOpened, proto.TypeServiceClose:
		return catPreview
	default:
		return catOther
	}
}
