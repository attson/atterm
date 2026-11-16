// Package terminalcodec encodes independently decodable terminal output blocks
// before encryption, keeping decompression memory bounded for untrusted frames.
package terminalcodec

import (
	"bytes"
	"compress/flate"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// MaxDecodedBytes bounds both publisher blocks and client decompression work.
const MaxDecodedBytes = 64 * 1024

const headerBytes = 5

type compressor struct {
	writer *flate.Writer
	buffer bytes.Buffer
}

// A channel caps retained compressors even after a burst of concurrent sessions.
var compressors = make(chan *compressor, 4)

// Encode returns an owned frame containing its codec and original byte length.
// Each compressed block is a complete raw DEFLATE stream, so replay needs no
// compression state from an earlier frame.
func Encode(input []byte) ([]byte, error) {
	if len(input) > MaxDecodedBytes {
		return nil, errors.New("terminal output exceeds decoded limit")
	}
	output := make([]byte, headerBytes, headerBytes+len(input))
	binary.BigEndian.PutUint32(output[1:], uint32(len(input)))
	if len(input) < 128 {
		return append(output, input...), nil
	}

	var c *compressor
	select {
	case c = <-compressors:
	default:
		writer, err := flate.NewWriter(io.Discard, flate.BestSpeed)
		if err != nil {
			return nil, fmt.Errorf("create terminal compressor: %w", err)
		}
		c = &compressor{writer: writer}
	}
	c.buffer.Reset()
	c.writer.Reset(&c.buffer)
	defer func() {
		c.writer.Reset(io.Discard)
		select {
		case compressors <- c:
		default:
		}
	}()
	if _, err := c.writer.Write(input); err != nil {
		return nil, fmt.Errorf("compress terminal output: %w", err)
	}
	if err := c.writer.Close(); err != nil {
		return nil, fmt.Errorf("finish terminal compression: %w", err)
	}
	if c.buffer.Len() < len(input) {
		output[0] = 1
		return append(output, c.buffer.Bytes()...), nil
	}
	return append(output, input...), nil
}

// Decode validates the declared size and returns owned plaintext bytes. The
// bounded reader prevents a small authenticated block from expanding without
// limit, and the exact-size check rejects incomplete or misleading envelopes.
func Decode(encoded []byte) ([]byte, error) {
	if len(encoded) < headerBytes {
		return nil, errors.New("truncated terminal output header")
	}
	originalLength := binary.BigEndian.Uint32(encoded[1:headerBytes])
	if originalLength > MaxDecodedBytes {
		return nil, errors.New("terminal output exceeds decoded limit")
	}
	body := encoded[headerBytes:]
	switch encoded[0] {
	case 0:
		if len(body) != int(originalLength) {
			return nil, errors.New("raw terminal output length mismatch")
		}
		return bytes.Clone(body), nil
	case 1:
		// bytes.Reader supplies ReadByte, preventing the inflater from reading
		// past the end of its stream and hiding trailing garbage.
		source := bytes.NewReader(body)
		reader := flate.NewReader(source)
		defer reader.Close()
		decoded, err := io.ReadAll(io.LimitReader(reader, MaxDecodedBytes+1))
		if err != nil {
			return nil, fmt.Errorf("decompress terminal output: %w", err)
		}
		if len(decoded) != int(originalLength) {
			return nil, errors.New("compressed terminal output length mismatch")
		}
		if source.Len() != 0 {
			return nil, errors.New("trailing compressed terminal output bytes")
		}
		return decoded, nil
	default:
		return nil, errors.New("unknown terminal output codec")
	}
}
