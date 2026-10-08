package peertransport

import (
	"encoding/binary"
	"errors"

	"github.com/google/uuid"
)

const (
	serviceMessageHeaderSize = 16 + 1 + 4 + 2
	// MaxServiceData keeps the complete service envelope inside one encrypted
	// Peer record. TCP readers split larger byte streams into multiple messages.
	MaxServiceData = MaxRecordPlaintext - serviceMessageHeaderSize
)

var ErrInvalidServiceMessage = errors.New("peertransport: invalid service message")

// ServiceKind identifies one operation in the Peer Preview byte channel.
type ServiceKind byte

const (
	ServiceOpen  ServiceKind = 1
	ServiceData  ServiceKind = 2
	ServiceClose ServiceKind = 3
)

// ServiceMessage multiplexes one Preview service and its local TCP
// connections without putting service bytes in terminal protocol frames.
type ServiceMessage struct {
	ServiceID  uuid.UUID
	Kind       ServiceKind
	Connection uint32
	Data       []byte
}

// EncodeServiceMessage validates and encodes one bounded service message.
func EncodeServiceMessage(message ServiceMessage) ([]byte, error) {
	if message.ServiceID == uuid.Nil || message.Connection == 0 ||
		message.Kind < ServiceOpen || message.Kind > ServiceClose ||
		len(message.Data) > MaxServiceData ||
		(message.Kind != ServiceData && len(message.Data) != 0) ||
		(message.Kind == ServiceData && len(message.Data) == 0) {
		return nil, ErrInvalidServiceMessage
	}
	payload := make([]byte, serviceMessageHeaderSize+len(message.Data))
	copy(payload[:16], message.ServiceID[:])
	payload[16] = byte(message.Kind)
	binary.BigEndian.PutUint32(payload[17:21], message.Connection)
	binary.BigEndian.PutUint16(payload[21:23], uint16(len(message.Data)))
	copy(payload[23:], message.Data)
	return payload, nil
}

// DecodeServiceMessage rejects malformed, oversized and ambiguous messages.
func DecodeServiceMessage(payload []byte) (ServiceMessage, error) {
	if len(payload) < serviceMessageHeaderSize || len(payload) > MaxRecordPlaintext {
		return ServiceMessage{}, ErrInvalidServiceMessage
	}
	serviceID, err := uuid.FromBytes(payload[:16])
	if err != nil || serviceID == uuid.Nil {
		return ServiceMessage{}, ErrInvalidServiceMessage
	}
	kind := ServiceKind(payload[16])
	connection := binary.BigEndian.Uint32(payload[17:21])
	dataLen := int(binary.BigEndian.Uint16(payload[21:23]))
	if connection == 0 || kind < ServiceOpen || kind > ServiceClose ||
		dataLen > MaxServiceData || len(payload) != serviceMessageHeaderSize+dataLen ||
		(kind != ServiceData && dataLen != 0) || (kind == ServiceData && dataLen == 0) {
		return ServiceMessage{}, ErrInvalidServiceMessage
	}
	return ServiceMessage{
		ServiceID: serviceID, Kind: kind, Connection: connection,
		Data: append([]byte(nil), payload[serviceMessageHeaderSize:]...),
	}, nil
}
