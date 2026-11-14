import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { deflateRawSync } from "node:zlib";
import { DecompressionStream, ReadableStream, WritableStream } from "node:stream/web";
import { decodeTerminalOutput, supportsTerminalCompression } from "./terminalCompression";

function block(codec: number, length: number, body: Uint8Array): Uint8Array {
  const bytes = new Uint8Array(5 + body.length);
  bytes[0] = codec;
  new DataView(bytes.buffer).setUint32(1, length, false);
  bytes.set(body, 5);
  return bytes;
}

beforeEach(() => { vi.stubGlobal("DecompressionStream", DecompressionStream); });
afterEach(() => { vi.unstubAllGlobals(); });

describe("terminal output compression", () => {
  it("decodes independently framed native DEFLATE and raw fallback bytes", async () => {
    const text = new TextEncoder().encode("\x1b[31m你好 terminal\r\n".repeat(100));
    const decoded = await decodeTerminalOutput(block(1, text.length, deflateRawSync(text)));
    expect(decoded && Array.from(decoded)).toEqual(Array.from(text));
    expect(await decodeTerminalOutput(block(0, 3, new Uint8Array([0, 255, 13])))).toEqual(new Uint8Array([0, 255, 13]));
    expect(await decodeTerminalOutput(block(0, 0, new Uint8Array()))).toEqual(new Uint8Array());
    const max = new Uint8Array(65536).fill(65);
    expect(await decodeTerminalOutput(block(1, max.length, deflateRawSync(max)))).toEqual(max);
  });

  it("rejects unknown codecs, bad lengths, corrupt or truncated streams and oversized output", async () => {
    const deflated = deflateRawSync(new Uint8Array(64).fill(65));
    const malformed = [
      new Uint8Array(4), block(2, 0, new Uint8Array()),
      block(0, 2, new Uint8Array([1])), block(0, 1, new Uint8Array([1, 2])),
      block(0, 65537, new Uint8Array()), block(1, 63, deflated), block(1, 65, deflated),
      block(1, 64, deflated.subarray(0, deflated.length - 1)), block(1, 64, new Uint8Array([255, 255])),
      block(1, 65536, deflateRawSync(new Uint8Array(65537))),
    ];
    for (const value of malformed) expect(await decodeTerminalOutput(value)).toBeNull();
  });

  it("stops reading and cancels as soon as decoded bytes exceed the advertised length", async () => {
    let canceled = false;
    let reads = 0;
    class ExpandingStream {
      writable = new WritableStream();
      readable = new ReadableStream<Uint8Array>({
        pull(controller) { reads++; controller.enqueue(new Uint8Array(8)); },
        cancel() { canceled = true; },
      }, { highWaterMark: 0 });
    }
    vi.stubGlobal("DecompressionStream", ExpandingStream);
    expect(await decodeTerminalOutput(block(1, 7, new Uint8Array([1])))).toBeNull();
    expect(canceled).toBe(true);
    expect(reads).toBe(1);
  });

  it("only advertises support when the native implementation accepts raw DEFLATE", () => {
    expect(supportsTerminalCompression()).toBe(true);
    vi.stubGlobal("DecompressionStream", class { constructor() { throw new TypeError("unsupported"); } });
    expect(supportsTerminalCompression()).toBe(false);
    vi.stubGlobal("DecompressionStream", undefined);
    expect(supportsTerminalCompression()).toBe(false);
  });
});
