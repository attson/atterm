// Each frame owns an independent stream so replay and reconnect never depend on
// a previous frame's inflater state. The advertised length bounds allocation.
export const MAX_OUTPUT_BYTES = 64 * 1024;

export function supportsTerminalCompression(): boolean {
  try {
    new DecompressionStream("deflate-raw");
    return true;
  } catch {
    return false;
  }
}

/** Decode authenticated codec bytes, bounding output while it is produced. */
export async function decodeTerminalOutput(block: Uint8Array): Promise<Uint8Array | null> {
  if (block.length < 5) return null;
  const codec = block[0];
  const length = new DataView(block.buffer, block.byteOffset, block.byteLength).getUint32(1, false);
  if (length > MAX_OUTPUT_BYTES) return null;
  const body = block.subarray(5);
  if (codec === 0) return body.length === length ? body : null;
  if (codec !== 1) return null;

  // Native inflaters do not expose consumed input (some accept trailing bytes).
  // AEAD authenticates the whole block; decoded length remains exact and bounded.
  let stream: DecompressionStream;
  try {
    stream = new DecompressionStream("deflate-raw");
  } catch {
    return null;
  }
  const writer = stream.writable.getWriter();
  const reader = stream.readable.getReader();
  // Writing and reading must overlap: the native transform applies backpressure.
  const writing = writer.write(new Uint8Array(body)).then(() => writer.close());
  void writing.catch(() => {});
  const result = new Uint8Array(length);
  let offset = 0;
  let complete = false;
  try {
    for (;;) {
      const { value, done } = await reader.read();
      if (done) break;
      if (value.length > length - offset) return null;
      result.set(value, offset);
      offset += value.length;
    }
    await writing;
    complete = true;
    return offset === length ? result : null;
  } catch {
    return null;
  } finally {
    if (!complete) {
      await reader.cancel().catch(() => {});
      await writer.abort().catch(() => {});
    }
    reader.releaseLock();
    writer.releaseLock();
  }
}
