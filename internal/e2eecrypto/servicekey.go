package e2eecrypto

import (
	"crypto/sha256"
	"fmt"
	"io"

	"github.com/google/uuid"
	"golang.org/x/crypto/hkdf"
)

const serviceKeyInfoPrefix = "atterm-service-v1"

// ServiceKeys are direction-separated keys for one Preview service. A global
// sequence nonce is maintained independently in each direction, so using two
// keys prevents a client packet and a host packet at the same sequence number
// from ever reusing an AES-GCM (key, nonce) pair.
type ServiceKeys struct {
	ClientToHost []byte
	HostToClient []byte
}

// DeriveServiceKeys expands accountKey into two AES-256 keys bound to
// serviceID. The service UUID is random per lease, so closing/reopening the
// same port never reuses keys.
func DeriveServiceKeys(accountKey []byte, serviceID uuid.UUID) (ServiceKeys, error) {
	if len(accountKey) < SessionKeySize {
		return ServiceKeys{}, ErrAccountKeyShort
	}
	info := make([]byte, 0, len(serviceKeyInfoPrefix)+16)
	info = append(info, serviceKeyInfoPrefix...)
	info = append(info, serviceID[:]...)
	r := hkdf.New(sha256.New, accountKey, nil, info)
	out := make([]byte, SessionKeySize*2)
	if _, err := io.ReadFull(r, out); err != nil {
		return ServiceKeys{}, fmt.Errorf("hkdf service keys: %w", err)
	}
	return ServiceKeys{
		ClientToHost: append([]byte(nil), out[:SessionKeySize]...),
		HostToClient: append([]byte(nil), out[SessionKeySize:]...),
	}, nil
}
