import { describe, expect, it } from "vitest";
import {
  sealUnsequenced,
  openUnsequencedFrame,
  FS_REQUEST_AAD_FRAME_TYPE,
  FS_RESPONSE_AAD_FRAME_TYPE,
} from "./opaque";

// The Go and TS envelope implementations are written independently. Only
// a fixture produced by one and opened by the other proves they agree on
// key derivation, AAD layout, and envelope framing. Keep these two
// constants in sync with internal/proto/fs_vectors_test.go.
const SID = "11111111-2222-3333-4444-555555555555";
const KEY = new Uint8Array(32).map((_, i) => i);

// Produced by Go: SealUnsequenced(DeriveSessionKey(KEY, SID), SID, 0x39, "SECRET=1")
const GO_ENVELOPE_B64 = "AS21SQ7pMfGoUYiqS2rhVf0KtaxOeyWqhWyby0AoU/MC7KgkxJg8+h6Ko363Xyutwg==";

function b64ToBytes(s: string): Uint8Array {
  const bin = atob(s);
  return Uint8Array.from(bin, (c) => c.charCodeAt(0));
}

function bytesToB64(b: Uint8Array): string {
  let bin = "";
  for (const x of b) bin += String.fromCharCode(x);
  return btoa(bin);
}

describe("cross-language envelope vectors", () => {
  it("opens a Go-sealed envelope", () => {
    const opened = openUnsequencedFrame(KEY, SID, FS_RESPONSE_AAD_FRAME_TYPE, b64ToBytes(GO_ENVELOPE_B64));
    expect(opened).not.toBeNull();
    expect(new TextDecoder().decode(opened!)).toBe("SECRET=1");
  });

  it("rejects the Go envelope under the wrong frame type", () => {
    expect(
      openUnsequencedFrame(KEY, SID, FS_REQUEST_AAD_FRAME_TYPE, b64ToBytes(GO_ENVELOPE_B64)),
    ).toBeNull();
  });

  it("emits an envelope for the Go fixture", () => {
    // Prints the vector consumed by internal/proto/fs_vectors_test.go.
    // Nonces are random, so any run's output is a valid fixture; the Go
    // test pins one of them.
    const env = sealUnsequenced(KEY, SID, FS_REQUEST_AAD_FRAME_TYPE, new TextEncoder().encode("/home/u/.env"));
    console.log("TS_ENVELOPE_B64:", bytesToB64(env));
    expect(env[0]).toBe(0x01);
    expect(env.length).toBe("/home/u/.env".length + 41);
  });
});
