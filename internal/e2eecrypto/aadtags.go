package e2eecrypto

// AAD discriminator bytes for sealed envelopes.
//
// Every sealed namespace gets a unique byte. It is mixed into the AEAD's
// additional data alongside the session UUID, which is what stops an envelope
// of one type being replayed in place of another (AGENTS.md redline #22).
// A missing or colliding registration here is a cryptographic gap, not a
// documentation nit: it means an attacker who can capture one sealed
// envelope can splice it into a different frame type and have it decrypt.
//
// Most values are the protocol frame type that carries the envelope. Two are
// synthetic: they never appear on the relay wire and exist only to give a
// preference-sync payload its own discriminator.
//
// Adding a namespace means adding it here AND adding a row to
// docs/spec/protocol.md's sealed-envelope table — aadtags_test.go fails if the
// two disagree.
//
// AGENTS.md redline #22 carries its own prose copy of this same allocation
// list, for the reader who never opens this file. That copy is NOT checked
// against this registry by any test (it's a sentence, not a table — see
// aadtags_test.go's doc comment for why that wasn't automated). Editing one
// without the other silently reintroduces the exact drift this file exists
// to prevent — update both by hand.
const (
	AADTagOut          byte = 0x03
	AADTagMeta         byte = 0x05
	AADTagListResp     byte = 0x12
	AADTagCommandEvent byte = 0x35
	AADTagPasteFile    byte = 0x37
	AADTagFSRequest    byte = 0x38
	AADTagFSResponse   byte = 0x39
	AADTagFSEvent      byte = 0x3a

	// Synthetic, AAD-only — no wire frame carries these.
	AADTagSSHHosts byte = 0xF0
	AADTagProfiles byte = 0xF1
)

var AADTags = map[byte]string{
	AADTagOut:          "OUT",
	AADTagMeta:         "META",
	AADTagListResp:     "LIST_RESP",
	AADTagCommandEvent: "COMMAND_EVENT",
	AADTagPasteFile:    "PASTE_FILE",
	AADTagFSRequest:    "FS_REQUEST",
	AADTagFSResponse:   "FS_RESPONSE",
	AADTagFSEvent:      "FS_EVENT",
	AADTagSSHHosts:     "ssh_hosts_encrypted sync",
	AADTagProfiles:     "profiles_encrypted sync",
}
