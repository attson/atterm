package peertransport

import (
	"bytes"
	"testing"
	"time"
)

func FuzzHandshakeMessages(f *testing.F) {
	for stage := uint8(0); stage < 4; stage++ {
		f.Add(stage, true, []byte(nil))
	}
	f.Add(uint8(0), false, []byte{})
	f.Add(uint8(2), false, []byte{HandshakeVersion, 0xff})

	f.Fuzz(func(t *testing.T, stage uint8, useValid bool, candidate []byte) {
		auth, err := NewAccountKeyAuthenticator(bytes.Repeat([]byte{0x42}, accountKeySize))
		if err != nil {
			t.Fatal(err)
		}
		now := time.Unix(1_800_000_000, 0)
		authorization := testAuthorization(now.Add(time.Hour))
		fixedNow := func() time.Time { return now }

		client, err := NewClientHandshake(auth, authorization)
		if err != nil {
			t.Fatal(err)
		}
		client.now = fixedNow
		host, err := NewHostHandshake(auth, authorization)
		if err != nil {
			t.Fatal(err)
		}
		host.now = fixedNow
		clientHello, err := client.ClientHello()
		if err != nil {
			t.Fatal(err)
		}

		switch stage % 4 {
		case 0:
			if useValid {
				candidate = clientHello
			}
			_, err = host.Handle(candidate)
			if useValid && err != nil {
				t.Fatalf("valid client hello rejected: %v", err)
			}
		case 1:
			hostHello, err := host.Handle(clientHello)
			if err != nil {
				t.Fatal(err)
			}
			clientFinish, err := client.Handle(hostHello.Response)
			if err != nil {
				t.Fatal(err)
			}
			if useValid {
				candidate = clientFinish.Response
			}
			result, err := host.Handle(candidate)
			if useValid && (err != nil || !result.Authenticated) {
				t.Fatalf("valid client finish rejected: authenticated=%v err=%v", result.Authenticated, err)
			}
		case 2:
			hostHello, err := host.Handle(clientHello)
			if err != nil {
				t.Fatal(err)
			}
			if useValid {
				candidate = hostHello.Response
			}
			_, _, decodeErr := DecodeHostHello(candidate)
			_, handleErr := client.Handle(candidate)
			if useValid && (decodeErr != nil || handleErr != nil) {
				t.Fatalf("valid host hello rejected: decode=%v handle=%v", decodeErr, handleErr)
			}
		case 3:
			hostHello, err := host.Handle(clientHello)
			if err != nil {
				t.Fatal(err)
			}
			clientFinish, err := client.Handle(hostHello.Response)
			if err != nil {
				t.Fatal(err)
			}
			hostResult, err := host.Handle(clientFinish.Response)
			if err != nil {
				t.Fatal(err)
			}
			if useValid {
				candidate = hostResult.Response
			}
			isAuthOK := IsAuthOK(candidate)
			result, err := client.Handle(candidate)
			if useValid && (!isAuthOK || err != nil || !result.Authenticated) {
				t.Fatalf("valid auth ok rejected: marker=%v authenticated=%v err=%v", isAuthOK, result.Authenticated, err)
			}
		}
	})
}

func FuzzRecordAndServiceDecoders(f *testing.F) {
	sealer, _ := fuzzRecordPair(f)
	validService := mustEncodeFuzzService(f)
	validRecord, err := sealer.Seal(RecordService, validService)
	if err != nil {
		f.Fatal(err)
	}
	f.Add(validRecord)
	f.Add(validService)
	f.Add([]byte{})
	f.Add(bytes.Repeat([]byte{0xff}, recordHeaderSize+recordTagSize))

	f.Fuzz(func(t *testing.T, data []byte) {
		_, opener := fuzzRecordPair(t)
		kind, plaintext, err := opener.Open(data)
		if err == nil {
			if !kind.valid() || len(plaintext) > MaxRecordPlaintext {
				t.Fatalf("accepted record kind=%d plaintext=%d", kind, len(plaintext))
			}
		}
		message, err := DecodeServiceMessage(data)
		if err == nil && len(message.Data) > MaxServiceData {
			t.Fatalf("accepted service payload=%d", len(message.Data))
		}
	})
}

func FuzzFragmentReassemblers(f *testing.F) {
	frameFragments, err := FragmentFrame(1, bytes.Repeat([]byte("f"), MaxRecordPlaintext+128))
	if err != nil {
		f.Fatal(err)
	}
	configFragments, err := FragmentConfigMessage(RecordConfigBatch, 2, bytes.Repeat([]byte("c"), MaxRecordPlaintext+128))
	if err != nil {
		f.Fatal(err)
	}
	signalFragments, err := FragmentSignalMessage(3, bytes.Repeat([]byte("s"), MaxRecordPlaintext+128))
	if err != nil {
		f.Fatal(err)
	}
	f.Add(uint8(0), frameFragments[0], frameFragments[1], false)
	f.Add(uint8(1), configFragments[0], configFragments[1], false)
	f.Add(uint8(2), signalFragments[0], signalFragments[1], false)
	f.Add(uint8(0), []byte{}, []byte{}, true)

	f.Fuzz(func(t *testing.T, namespace uint8, first, second []byte, expire bool) {
		now := time.Unix(1_800_000_000, 0)
		secondAt := now
		if expire {
			secondAt = now.Add(ReassemblyTimeout + time.Millisecond)
		}
		switch namespace % 3 {
		case 0:
			var reassembler Reassembler
			frame, complete, err := reassembler.Add(first, now)
			if err == nil && !complete {
				frame, complete, err = reassembler.Add(second, secondAt)
			}
			if err == nil && complete && (len(frame) <= MaxRecordPlaintext || len(frame) > MaxFrameSize) {
				t.Fatalf("accepted terminal frame size=%d", len(frame))
			}
		case 1:
			var reassembler ConfigReassembler
			kind, message, complete, err := reassembler.Add(first, now)
			if err == nil && !complete {
				kind, message, complete, err = reassembler.Add(second, secondAt)
			}
			if err == nil && complete && (!kind.configMessage() || len(message) <= MaxRecordPlaintext || len(message) > MaxConfigMessageSize) {
				t.Fatalf("accepted config kind=%d size=%d", kind, len(message))
			}
		case 2:
			var reassembler SignalReassembler
			message, complete, err := reassembler.Add(first, now)
			if err == nil && !complete {
				message, complete, err = reassembler.Add(second, secondAt)
			}
			if err == nil && complete && (len(message) <= MaxRecordPlaintext || len(message) > MaxSignalMessageSize) {
				t.Fatalf("accepted signal size=%d", len(message))
			}
		}
	})
}

type fuzzFataler interface {
	Helper()
	Fatal(args ...any)
}

func fuzzRecordPair(t fuzzFataler) (*RecordSealer, *RecordOpener) {
	t.Helper()
	key := bytes.Repeat([]byte{0x71}, 32)
	prefix := bytes.Repeat([]byte{0x19}, 16)
	hash := TranscriptHash([]byte("fuzz transcript"))
	sealer, err := NewRecordSealer(key, prefix, hash)
	if err != nil {
		t.Fatal(err)
	}
	opener, err := NewRecordOpener(key, prefix, hash)
	if err != nil {
		t.Fatal(err)
	}
	return sealer, opener
}

func mustEncodeFuzzService(f *testing.F) []byte {
	f.Helper()
	payload, err := EncodeServiceMessage(ServiceMessage{
		ServiceID:  [16]byte{15: 1},
		Kind:       ServiceData,
		Connection: 1,
		Data:       []byte("preview"),
	})
	if err != nil {
		f.Fatal(err)
	}
	return payload
}
