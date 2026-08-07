// Mirror of internal/proto/fs_segments.go.
//
// FS payloads are segmented rather than a single JSON document so file
// bytes can ride raw inside an AEAD envelope instead of being base64'd
// into JSON — encoding/json already base64s []byte, so nesting an
// envelope in a JSON string would encode twice for 1.78x expansion.
//
//   payload := segment_count(1B) || segment*
//   segment := length(4B BE) || bytes
//
// Segment 0 is always plaintext JSON with the fields the relay routes
// on. Segment count doubles as the key-state signal: one segment means
// the peer is keyless, more means sealed.

const MAX_SEGMENT = 16 * 1024 * 1024;

export function encodeSegments(segments: Uint8Array[]): Uint8Array {
  if (segments.length === 0 || segments.length > 255) {
    throw new Error(`fs segments: bad segment count ${segments.length}`);
  }
  let total = 1;
  for (const s of segments) {
    if (s.length > MAX_SEGMENT) throw new Error("fs segments: segment too large");
    total += 4 + s.length;
  }
  const out = new Uint8Array(total);
  const view = new DataView(out.buffer);
  out[0] = segments.length;
  let offset = 1;
  for (const s of segments) {
    view.setUint32(offset, s.length, false);
    offset += 4;
    out.set(s, offset);
    offset += s.length;
  }
  return out;
}

/** decodeSegments returns null rather than throwing on malformed input:
 *  every caller sits on a frame-handling path where the right response to
 *  a bad payload is to drop it. Declared lengths must sum to exactly the
 *  remaining bytes, so truncation and padding both fail. */
export function decodeSegments(payload: Uint8Array): Uint8Array[] | null {
  if (payload.length < 1) return null;
  const count = payload[0];
  if (count === 0) return null;
  const view = new DataView(payload.buffer, payload.byteOffset, payload.byteLength);
  const segments: Uint8Array[] = [];
  let offset = 1;
  for (let i = 0; i < count; i++) {
    if (offset + 4 > payload.length) return null;
    const n = view.getUint32(offset, false);
    if (n > MAX_SEGMENT) return null;
    offset += 4;
    if (offset + n > payload.length) return null;
    segments.push(payload.slice(offset, offset + n));
    offset += n;
  }
  if (offset !== payload.length) return null;
  return segments;
}
