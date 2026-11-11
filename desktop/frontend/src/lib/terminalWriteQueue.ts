// xterm checks its 12 ms parser budget only between write calls. Relay replay
// chunks are about 16 KiB, so split them without adding another timer/queue.
export const DEFAULT_TERMINAL_WRITE_CHUNK_BYTES = 1024;

export type TerminalWriter = {
  write(data: string | Uint8Array, callback?: () => void): void;
};

export type TerminalWriteQueue = {
  write(data: string | Uint8Array): void;
  drain(callback: () => void): void;
};

export function createTerminalWriteQueue(
  writer: TerminalWriter,
  options: { maxChunkBytes?: number } = {},
): TerminalWriteQueue {
  const maxChunkBytes = options.maxChunkBytes ?? DEFAULT_TERMINAL_WRITE_CHUNK_BYTES;
  if (!Number.isInteger(maxChunkBytes) || maxChunkBytes <= 0) {
    throw new RangeError("maxChunkBytes must be a positive integer");
  }

  return {
    write(data) {
      if (typeof data === "string" || data.byteLength <= maxChunkBytes) {
        writer.write(data);
        return;
      }
      for (let offset = 0; offset < data.byteLength; offset += maxChunkBytes) {
        writer.write(data.subarray(offset, offset + maxChunkBytes));
      }
    },
    drain(callback) {
      writer.write("", callback);
    },
  };
}
