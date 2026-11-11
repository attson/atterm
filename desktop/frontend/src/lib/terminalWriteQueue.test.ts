import { describe, expect, test, vi } from "vitest";
import { createTerminalWriteQueue } from "./terminalWriteQueue";

describe("terminal write queue", () => {
  test("bounds default xterm writes to one KiB for escape-heavy replay", () => {
    const writes: Uint8Array[] = [];
    const writer = {
      write: vi.fn((data: string | Uint8Array) => {
        if (data instanceof Uint8Array) writes.push(data);
      }),
    };
    const queue = createTerminalWriteQueue(writer);

    queue.write(new Uint8Array(1025));

    expect(writes.map((chunk) => chunk.byteLength)).toEqual([1024, 1]);
  });

  test("keeps output byte-exact while bounding each xterm write", () => {
    const writes: Uint8Array[] = [];
    const writer = {
      write: vi.fn((data: string | Uint8Array) => {
        if (data instanceof Uint8Array) writes.push(data);
      }),
    };
    const queue = createTerminalWriteQueue(writer, { maxChunkBytes: 4 });
    const output = new Uint8Array([0xf0, 0x9f, 0x98, 0x80, 0x1b, 0x5b, 0x32, 0x4a, 0x24, 0x20]);

    queue.write(output);

    expect(writes.map((chunk) => Array.from(chunk))).toEqual([
      [0xf0, 0x9f, 0x98, 0x80],
      [0x1b, 0x5b, 0x32, 0x4a],
      [0x24, 0x20],
    ]);
    expect(Array.from(new Uint8Array(writes.flatMap((chunk) => Array.from(chunk))))).toEqual(
      Array.from(output),
    );
  });

  test("reports drained only after every previously queued output chunk", () => {
    const writes: Array<{ data: string | Uint8Array; callback?: () => void }> = [];
    const writer = {
      write: vi.fn((data: string | Uint8Array, callback?: () => void) => {
        writes.push({ data, callback });
      }),
    };
    const queue = createTerminalWriteQueue(writer, { maxChunkBytes: 2 });
    const drained = vi.fn();

    queue.write(new Uint8Array([1, 2, 3, 4, 5]));
    queue.drain(drained);

    expect(writes.map(({ data }) => (typeof data === "string" ? data : Array.from(data)))).toEqual([
      [1, 2],
      [3, 4],
      [5],
      "",
    ]);
    expect(drained).not.toHaveBeenCalled();

    writes.at(-1)?.callback?.();
    expect(drained).toHaveBeenCalledOnce();
  });
});
