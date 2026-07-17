package proto

import "github.com/google/uuid"

// Version is the wire protocol version. Bump when incompatible changes ship.
const Version uint8 = 1

// Type is a frame type discriminator.
type Type uint8

const (
	RemotePermissionView    = "view"
	RemotePermissionControl = "control"
	RemotePermissionFull    = "full"
)

const (
	TaskStateIdle         = "idle"
	TaskStateRunning      = "running"
	TaskStateWaitingInput = "waiting_input"
	TaskStateCompleted    = "completed"
	TaskStateFailed       = "failed"
	TaskStateDisconnected = "disconnected"
	TaskStateClosed       = "closed"
)

const (
	TypeOpen     Type = 0x01 // agent -> relay
	TypeIn       Type = 0x02 // client -> relay -> agent
	TypeOut      Type = 0x03 // agent -> relay -> client (8B seq prefix)
	TypeResize   Type = 0x04 // client -> relay -> agent (4B cols|rows)
	TypeMeta     Type = 0x05 // agent -> relay -> client
	TypeClose    Type = 0x06 // agent -> relay -> client
	TypeAttach   Type = 0x10 // client -> relay
	TypeList     Type = 0x11 // client -> relay
	TypeListResp Type = 0x12 // relay -> client
	// TypeReplayProgress brackets the initial ATTACH scrollback replay so
	// clients can show history-loading progress before the session is live.
	TypeReplayProgress Type = 0x13 // relay -> client
	TypePing           Type = 0x20
	TypePong           Type = 0x21

	// Lazy-mirror frames (Phase 1.5). An "uplink" is an agent that wants to
	// advertise sessions cheaply and only stream their bytes when a client
	// asks. ANNOUNCE goes uplink->relay; STREAM_REQUEST/STOP go relay->uplink.
	TypeAnnounce      Type = 0x30
	TypeStreamRequest Type = 0x31
	TypeStreamStop    Type = 0x32
	TypePasteImage    Type = 0x33 // client -> relay -> desktop PTY host
	TypeClaimDriver   Type = 0x34 // client -> relay (viewer claims driver role)
	TypeCommandEvent  Type = 0x35 // uplink -> relay (Web Push trigger)
	TypeViewers       Type = 0x36 // relay -> uplink; mirror's remote subscriber count
	TypePasteFile     Type = 0x37 // client -> relay -> desktop PTY host (generic file attachment)
	TypeFSRequest     Type = 0x38 // client -> relay -> desktop uplink (remote file explorer)
	TypeFSResponse    Type = 0x39 // desktop uplink -> relay -> requester client
	TypeFSEvent       Type = 0x3a // desktop uplink -> relay -> requester client

	// Auth frames (server → client).
	TypeAuthInfo Type = 0x40 // relay -> uplink; UTF-8 JSON {user_id}
)

// Frame is a single protocol message.
type Frame struct {
	Type      Type
	SessionID uuid.UUID
	Payload   []byte
}

// OpenPayload is the JSON body of a TypeOpen frame.
type OpenPayload struct {
	Cols    uint16 `json:"cols"`
	Rows    uint16 `json:"rows"`
	Command string `json:"command"`
	Cwd     string `json:"cwd"`
	Title   string `json:"title"`
	HostID  string `json:"host_id"` // stable machine UUID persisted by atterm
	Host    string `json:"host"`    // hostname of the agent's machine
	User    string `json:"user"`    // OS username (uid resolved if possible)
}

// SessionSummary is the post-D snapshot of a command's tail output and
// (when the captured exit code was non-zero) the extracted error lines.
// Nil before the first D event on a session; overwritten on each
// subsequent D. RecentOutput is ANSI-stripped UTF-8 text.
type SessionSummary struct {
	RecentOutput string   `json:"recent_output,omitempty"`
	ErrorLines   []string `json:"error_lines,omitempty"`
	CapturedAt   int64    `json:"captured_at,omitempty"`
}

// MetaPayload is the JSON body of a TypeMeta frame.
type MetaPayload struct {
	Cwd   string `json:"cwd,omitempty"`
	Title string `json:"title,omitempty"`
	// DriverClientID is the end-to-end client_id of the subscriber currently
	// allowed to send IN/RESIZE/PASTE_IMAGE. Empty = no driver assigned.
	DriverClientID string `json:"driver_client_id,omitempty"`
	// DriverClientName is the human-readable name reported by the driver
	// (typically its hostname). Empty when no driver, or when the driver
	// didn't supply a name.
	DriverClientName string `json:"driver_client_name,omitempty"`
	// Cols/Rows snapshot the PTY's current size so viewers can lock their
	// xterm.cols/rows to the PTY (they don't run FitAddon).
	Cols uint16 `json:"cols,omitempty"`
	Rows uint16 `json:"rows,omitempty"`
	// Task fields describe the latest command lifecycle derived from OSC 133
	// and lightweight output heuristics.
	TaskState         string `json:"task_state,omitempty"`
	CurrentCommand    string `json:"current_command,omitempty"`
	CommandStartedAt  int64  `json:"command_started_at,omitempty"`
	CommandEndedAt    int64  `json:"command_ended_at,omitempty"`
	CommandDurationMS int    `json:"command_duration_ms,omitempty"`
	CommandExitCode   *int   `json:"command_exit_code,omitempty"`
	LastOutputAt      int64  `json:"last_output_at,omitempty"`
	// Type is the session workload tag (carried-over from P2.11 which only
	// added Type to SessionInfo, never to MetaPayload — meant real-time
	// type changes didn't reach subscribers until they refreshed the list).
	Type string `json:"type,omitempty"`
	// Summary carries the most recent SessionSummary for the session.
	Summary *SessionSummary `json:"summary,omitempty"`
	// AttentionAt mirrors SessionInfo.AttentionAt so attached clients learn
	// the latest attention timestamp in real time. Unread is intentionally
	// NOT carried in META: an attached client is watching ⇒ read.
	AttentionAt int64 `json:"attention_at,omitempty"`
	// Sealed is the AEAD envelope (per internal/e2eecrypto) over a JSON
	// document carrying the content-bearing META fields the relay is
	// structurally unable to read when E2EE is on: Cwd, Title,
	// CurrentCommand. Mirrors SessionInfo.Sealed (M3b) but rides on
	// live TypeMeta frames so live updates do not leak between
	// ANNOUNCE snapshots. The agent's uplink forwarder seals
	// these fields and clears their plaintext counterparts when an
	// account_key is unlocked.
	Sealed []byte `json:"sealed,omitempty"`
}

// ClosePayload is the JSON body of a TypeClose frame.
type ClosePayload struct {
	ExitCode int    `json:"exit_code"`
	Reason   string `json:"reason,omitempty"`
}

// AttachPayload is the JSON body of a TypeAttach frame.
type AttachPayload struct {
	SessionID string `json:"session_id"`
	SinceSeq  uint64 `json:"since_seq,omitempty"`
	// ClientID is a UUID generated client-side per SessionConnection. The
	// relay echoes it back in META.driver_client_id when this subscriber is
	// the active driver so the client can recognize itself. Empty is allowed
	// (older clients) — they participate but never UI-render as driver.
	ClientID string `json:"client_id,omitempty"`
	// ClientName is a human-readable identifier for the attacher (typically
	// the device hostname). Echoed back in META.driver_client_name so
	// viewers can show "taken over by <name>" in their overlay.
	ClientName string `json:"client_name,omitempty"`
}

// ClaimDriverPayload is the JSON body of a TypeClaimDriver frame.
type ClaimDriverPayload struct {
	ClientID   string `json:"client_id"`
	ClientName string `json:"client_name,omitempty"`
}

// ViewersPayload is the JSON body of a TypeViewers frame: the count of remote
// /client subscribers currently attached to a session's mirror on the relay.
type ViewersPayload struct {
	SessionID string `json:"session_id"`
	Count     int    `json:"count"`
}

const (
	ReplayProgressStart = "start"
	ReplayProgressChunk = "chunk"
	ReplayProgressEnd   = "end"
)

// ReplayProgressPayload reports ATTACH replay progress in terminal bytes.
type ReplayProgressPayload struct {
	Phase      string `json:"phase"`
	Bytes      uint64 `json:"bytes"`
	TotalBytes uint64 `json:"total_bytes"`
	Seq        uint64 `json:"seq,omitempty"`
}

// AnnouncePayload is the JSON body of a TypeAnnounce frame. It is a full
// snapshot — relay replaces its manifest of this host's sessions on each
// receive (no diff format).
type AnnouncePayload struct {
	HostID   string        `json:"host_id"`
	Host     string        `json:"host"`
	User     string        `json:"user"`
	Sessions []SessionInfo `json:"sessions"`
}

// StreamRequestPayload is the JSON body of TypeStreamRequest.
type StreamRequestPayload struct {
	SessionID string `json:"session_id"`
	SinceSeq  uint64 `json:"since_seq,omitempty"`
}

// StreamStopPayload is the JSON body of TypeStreamStop.
type StreamStopPayload struct {
	SessionID string `json:"session_id"`
}

// PasteImagePayload carries an image clipboard item from a remote web client
// to the desktop host that owns the PTY. Data is JSON/base64 encoded on the
// wire via Go's []byte JSON encoding.
type PasteImagePayload struct {
	Filename    string `json:"filename,omitempty"`
	ContentType string `json:"content_type"`
	Data        []byte `json:"data"`
}

// PasteFilePayload carries a generic file attachment from a remote client
// to the desktop that owns the PTY. Structurally identical to
// PasteImagePayload but semantically distinct: PASTE_IMAGE is clipboard
// image data (silent, filename synthesized); PASTE_FILE is an explicit
// user-picked attachment (filename is user-visible). The desktop
// sanitizes/dedups Filename before writing, and injects the resulting
// absolute path into the PTY master (no CR, no quoting).
type PasteFilePayload struct {
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	Data        []byte `json:"data"`
}

// DirEntry is a protocol-level directory listing item.
type DirEntry struct {
	Name    string `json:"name"`
	IsDir   bool   `json:"isDir"`
	Size    int64  `json:"size,omitempty"`
	ModTime int64  `json:"modTime,omitempty"`
}

// FileContent carries a bounded file preview body.
type FileContent struct {
	Path        string `json:"path"`
	Data        []byte `json:"data"`
	IsBinary    bool   `json:"isBinary"`
	TruncatedAt int64  `json:"truncatedAt,omitempty"`
}

// FileMetaInfo carries file metadata used before reading content.
type FileMetaInfo struct {
	Path     string `json:"path"`
	Size     int64  `json:"size"`
	ModTime  int64  `json:"modTime"`
	IsBinary bool   `json:"isBinary"`
}

// FSRequestPayload is the JSON body of TypeFSRequest.
type FSRequestPayload struct {
	RequestID       string `json:"request_id"`
	ClientID        string `json:"client_id,omitempty"`
	Op              string `json:"op"`
	Path            string `json:"path,omitempty"`
	MaxBytes        int64  `json:"max_bytes,omitempty"`
	Offset          int64  `json:"offset,omitempty"`
	Length          int64  `json:"length,omitempty"`
	WatchID         string `json:"watch_id,omitempty"`
	Data            []byte `json:"data,omitempty"`
	ExpectedModTime int64  `json:"expected_modtime,omitempty"`
	NewPath         string `json:"new_path,omitempty"`
	Recursive       bool   `json:"recursive,omitempty"`
	CreateIfMissing bool   `json:"create_if_missing,omitempty"`
}

// FSChunkPayload carries a chunk of file data for remote previews/assets.
type FSChunkPayload struct {
	Path        string `json:"path"`
	Data        []byte `json:"data"`
	Offset      int64  `json:"offset"`
	Length      int64  `json:"length"`
	EOF         bool   `json:"eof"`
	ContentType string `json:"contentType,omitempty"`
}

// FSResponsePayload is the JSON body of TypeFSResponse.
type FSResponsePayload struct {
	RequestID string          `json:"request_id"`
	OK        bool            `json:"ok"`
	Error     string          `json:"error,omitempty"`
	Entries   []DirEntry      `json:"entries,omitempty"`
	Meta      *FileMetaInfo   `json:"meta,omitempty"`
	Content   *FileContent    `json:"content,omitempty"`
	Chunk     *FSChunkPayload `json:"chunk,omitempty"`
	WatchID   string          `json:"watch_id,omitempty"`
}

// FSEventPayload is the JSON body of TypeFSEvent.
type FSEventPayload struct {
	WatchID string `json:"watch_id"`
	Path    string `json:"path"`
	Event   string `json:"event"`
}

// SessionInfo is one entry of TypeListResp.
type SessionInfo struct {
	ID        string `json:"id"`
	Command   string `json:"command"`
	Cwd       string `json:"cwd"`
	Title     string `json:"title"`
	Cols      uint16 `json:"cols"`
	Rows      uint16 `json:"rows"`
	StartedAt int64  `json:"started_at"`
	HostID    string `json:"host_id"`
	Host      string `json:"host"`
	User      string `json:"user"`
	// RemotePermission is owner-published permission for remote attachers.
	// Empty means full for backwards compatibility with older ANNOUNCEs.
	RemotePermission string `json:"remote_permission,omitempty"`
	// Task fields are additive metadata for task-first clients. Empty
	// TaskState means idle for older publishers that do not send it.
	TaskState         string `json:"task_state,omitempty"`
	CurrentCommand    string `json:"current_command,omitempty"`
	CommandStartedAt  int64  `json:"command_started_at,omitempty"`
	CommandEndedAt    int64  `json:"command_ended_at,omitempty"`
	CommandDurationMS int    `json:"command_duration_ms,omitempty"`
	CommandExitCode   *int   `json:"command_exit_code,omitempty"`
	LastOutputAt      int64  `json:"last_output_at,omitempty"`
	// Type is the workload classification ("shell" | "ai" | "test" |
	// "build" | "deploy"), derived by session.ClassifyCommand from the
	// current command. Older publishers omit it; clients treat empty as
	// equivalent to "shell". See spec §3.
	Type string `json:"type,omitempty"`
	// Summary is the most recent OSC 133 'D' summary for the session.
	// Nil before the first command finishes; overwritten on each D event.
	Summary *SessionSummary `json:"summary,omitempty"`
	// AttentionAt is the unix time (seconds) the session last entered an
	// attention-worthy state (waiting_input, or a non-shell completed/failed).
	// Zero means nothing is pending. See spec §4.
	AttentionAt int64 `json:"attention_at,omitempty"`
	// Unread is computed per authenticated user by the relay when building the
	// session list: attention_at > seen_at AND no client is attached. Always
	// zero in session-local copies; the relay sets it. See spec §2.
	Unread bool `json:"unread,omitempty"`
	// Sealed is the AEAD envelope (cipher_id + nonce + ciphertext + tag,
	// per internal/e2eecrypto) over a JSON document carrying the
	// content-bearing fields the relay is structurally unable to read
	// when E2EE is on: title, cwd, command, current_command. The agent
	// populates it when it has the user's unlocked account_key;
	// clients that share the account_key (same user, same relay)
	// decrypt and prefer these values over the plaintext fields.
	//
	// During the M3b-additive rollout the plaintext fields (Title,
	// Cwd, Command, CurrentCommand) stay populated alongside Sealed
	// so older clients without decrypt support keep rendering. A
	// later slice (M3b-strip) will drop the plaintext fields once
	// every shipping client reliably decrypts.
	Sealed []byte `json:"sealed,omitempty"`
}
