import { describe, expect, it } from "vitest";
import { encodeSegments, decodeSegments } from "./fsSegments";
import {
  sealUnsequenced,
  openUnsequencedFrame,
  FS_REQUEST_AAD_FRAME_TYPE,
  FS_RESPONSE_AAD_FRAME_TYPE,
} from "./opaque";

const SID = "11111111-2222-3333-4444-555555555555";
const KEY = new Uint8Array(32).map((_, i) => i);

describe("fs segments", () => {
  it("round-trips one, two and three segments", () => {
    for (const segs of [
      [new Uint8Array([1, 2, 3])],
      [new Uint8Array([1]), new Uint8Array([2, 2])],
      [new Uint8Array([1]), new Uint8Array([2]), new Uint8Array([3, 3, 3])],
      [new Uint8Array([1]), new Uint8Array()],
    ]) {
      const decoded = decodeSegments(encodeSegments(segs));
      expect(decoded).not.toBeNull();
      expect(decoded!.length).toBe(segs.length);
      decoded!.forEach((d, i) => expect(Array.from(d)).toEqual(Array.from(segs[i])));
    }
  });

  it("rejects malformed payloads", () => {
    const valid = encodeSegments([new Uint8Array([1, 2]), new Uint8Array([3, 4])]);
    expect(decodeSegments(new Uint8Array())).toBeNull();
    expect(decodeSegments(new Uint8Array([0]))).toBeNull();
    expect(decodeSegments(valid.slice(0, 3))).toBeNull();
    expect(decodeSegments(valid.slice(0, valid.length - 1))).toBeNull();
    expect(decodeSegments(new Uint8Array([...valid, 0xff]))).toBeNull();
  });

  it("refuses to encode zero segments", () => {
    expect(() => encodeSegments([])).toThrow();
  });
});

describe("sealUnsequenced", () => {
  it("round-trips with openUnsequencedFrame", () => {
    const plaintext = new TextEncoder().encode("SECRET=1");
    const env = sealUnsequenced(KEY, SID, FS_RESPONSE_AAD_FRAME_TYPE, plaintext);
    expect(env[0]).toBe(0x01);
    // cipher_id(1) + nonce(24) + tag(16)
    expect(env.length).toBe(plaintext.length + 41);
    const opened = openUnsequencedFrame(KEY, SID, FS_RESPONSE_AAD_FRAME_TYPE, env);
    expect(opened).not.toBeNull();
    expect(new TextDecoder().decode(opened!)).toBe("SECRET=1");
  });

  it("uses a fresh nonce per call", () => {
    const a = sealUnsequenced(KEY, SID, FS_RESPONSE_AAD_FRAME_TYPE, new Uint8Array([1]));
    const b = sealUnsequenced(KEY, SID, FS_RESPONSE_AAD_FRAME_TYPE, new Uint8Array([1]));
    expect(Array.from(a)).not.toEqual(Array.from(b));
  });

  it("fails to open under a different frame type", () => {
    const env = sealUnsequenced(KEY, SID, FS_RESPONSE_AAD_FRAME_TYPE, new TextEncoder().encode("x"));
    expect(openUnsequencedFrame(KEY, SID, FS_REQUEST_AAD_FRAME_TYPE, env)).toBeNull();
  });

  it("fails to open under a different session id", () => {
    const env = sealUnsequenced(KEY, SID, FS_RESPONSE_AAD_FRAME_TYPE, new TextEncoder().encode("x"));
    expect(
      openUnsequencedFrame(KEY, "99999999-2222-3333-4444-555555555555", FS_RESPONSE_AAD_FRAME_TYPE, env),
    ).toBeNull();
  });

  it("rejects structurally invalid envelopes", () => {
    expect(openUnsequencedFrame(KEY, SID, FS_RESPONSE_AAD_FRAME_TYPE, new Uint8Array(10))).toBeNull();
    expect(openUnsequencedFrame(KEY, SID, FS_RESPONSE_AAD_FRAME_TYPE, null)).toBeNull();
    const bad = sealUnsequenced(KEY, SID, FS_RESPONSE_AAD_FRAME_TYPE, new TextEncoder().encode("x"));
    bad[0] = 0x02;
    expect(openUnsequencedFrame(KEY, SID, FS_RESPONSE_AAD_FRAME_TYPE, bad)).toBeNull();
  });
});
