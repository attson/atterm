package terminalcodec

import (
	"bytes"
	"compress/flate"
	"encoding/binary"
	"math/rand"
	"sync"
	"testing"
)

func TestCodecRoundTrip(t *testing.T) {
	random := make([]byte, 4096)
	_, _ = rand.New(rand.NewSource(1)).Read(random)
	for _, input := range [][]byte{nil, []byte("\x1b[Hworking"), random, bytes.Repeat([]byte("\x1b[2Kreading source.go\r\n"), 200), bytes.Repeat([]byte("a"), MaxDecodedBytes)} {
		encoded, err := Encode(input)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := Decode(encoded)
		if err != nil || !bytes.Equal(decoded, input) {
			t.Fatalf("round trip size=%d: %v", len(input), err)
		}
		if len(input) < 128 || bytes.Equal(input, random) {
			if encoded[0] != 0 {
				t.Fatal("small/incompressible output must use raw codec")
			}
		} else if encoded[0] != 1 || len(encoded) >= len(input) {
			t.Fatal("repeated output should be compressed")
		}
	}
}

func TestDecodeRejectsInvalidOrOversizedOutput(t *testing.T) {
	valid, err := Encode(bytes.Repeat([]byte("r"), 2048))
	if err != nil {
		t.Fatal(err)
	}
	wrongLength := append([]byte(nil), valid...)
	binary.BigEndian.PutUint32(wrongLength[1:5], 1)
	overLimit := append([]byte(nil), valid...)
	binary.BigEndian.PutUint32(overLimit[1:5], MaxDecodedBytes+1)
	for _, data := range [][]byte{nil, {0, 0}, {9, 0, 0, 0, 0}, {0, 0, 0, 0, 2, 'a'}, {1, 0, 0, 0, 1, 0xff}, valid[:len(valid)-2], wrongLength, overLimit} {
		if _, err := Decode(data); err == nil {
			t.Fatalf("accepted invalid frame %x", data[:min(6, len(data))])
		}
	}
	if _, err := Encode(make([]byte, MaxDecodedBytes+1)); err == nil {
		t.Fatal("accepted oversized input")
	}
}

func TestCodecConcurrentFramesRemainIndependent(t *testing.T) {
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			input := bytes.Repeat([]byte{byte(i)}, 4096)
			for j := 0; j < 20; j++ {
				encoded, err := Encode(input)
				if err != nil {
					t.Error(err)
					return
				}
				got, err := Decode(encoded)
				if err != nil || !bytes.Equal(got, input) {
					t.Errorf("independent frame %d: %v", i, err)
					return
				}
			}
		}(i)
	}
	wg.Wait()
}

func TestDecodeRejectsTrailingStreamAndExcessiveExpansion(t *testing.T) {
	valid, err := Encode(bytes.Repeat([]byte("r"), 2048))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Decode(append(valid, 0)); err == nil {
		t.Fatal("accepted bytes after complete DEFLATE stream")
	}
	var compressed bytes.Buffer
	writer, err := flate.NewWriter(&compressed, flate.BestSpeed)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write(bytes.Repeat([]byte("r"), MaxDecodedBytes*8)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	encoded := []byte{1, 0, 0, 0, 1}
	if _, err := Decode(append(encoded, compressed.Bytes()...)); err == nil {
		t.Fatal("accepted compressed expansion beyond the decoded limit")
	}
}

func TestCodecFramesOwnTheirStorage(t *testing.T) {
	for _, input := range [][]byte{[]byte("small"), bytes.Repeat([]byte("r"), 2048)} {
		encoded, err := Encode(input)
		if err != nil {
			t.Fatal(err)
		}
		saved := bytes.Clone(encoded)
		input[0] = 'x'
		for i := 0; i < 8; i++ {
			if _, err := Encode(bytes.Repeat([]byte{byte(i)}, 4096)); err != nil {
				t.Fatal(err)
			}
		}
		if !bytes.Equal(encoded, saved) {
			t.Fatal("encoded frame aliases caller or compressor storage")
		}
		decoded, err := Decode(encoded)
		if err != nil {
			t.Fatal(err)
		}
		before := bytes.Clone(decoded)
		for i := range encoded {
			encoded[i] = 0
		}
		if !bytes.Equal(decoded, before) {
			t.Fatal("decoded frame aliases encoded storage")
		}
	}
}
