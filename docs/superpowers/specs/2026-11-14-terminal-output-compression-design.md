# Terminal output compression

Approved scope: add DEFLATE level 1 before end-to-end encryption and assess related traffic optimizations. The preceding CPU experiment found approximately 54% fewer wire bytes for representative synthetic small TUI frames, with negligible CPU at 40 MiB/hour. This is not a promise about actual sessions.

## Compatibility and ownership

Keep protocol version 1 and every existing frame payload unchanged. Add OUT_COMPRESSED (0x43), STREAM_REQUEST_COMPRESSED (0x44), and STREAM_STOP_COMPRESSED (0x45). The latter two reuse their original request/stop JSON schemas. ANNOUNCE gains optional `output_compression: "deflate-raw-v1"`; ATTACH gains the same optional capability. Older relays ignore these additive JSON fields and continue sending ordinary OUT. New relays only select compression when both publisher and subscriber support it; keyless publishers do not advertise it.

A mirror keeps independent, lazily allocated ordinary and compressed output buffers and subscription lifecycles inside the same Session. Session identity, driver, permissions, metadata, viewer count, and inbound routing remain shared. Mixed old/new viewers request only their respective stream; with only new viewers, no ordinary stream is uploaded. A publisher may send both formats when old and new viewers coexist. No relay ever decrypts terminal content.

## Encoding and limits

OUT_COMPRESSED is `seq(u64 BE) || AEAD envelope`. AAD is session UUID || 0x43 || seq. Encrypted plaintext is `codec(u8) || original_length(u32 BE) || body`. Codec 0 carries raw bytes; codec 1 carries a complete independent raw DEFLATE stream at level 1. Small or incompressible blocks use codec 0. Limit original data to 64 KiB per frame and validate exact decoded length. Browser decoding uses native DecompressionStream only when available; all decoding stays in the main thread. Reject unknown codecs, corrupt ciphertext, truncated or oversized output. No keys or plaintext enter logs.

Reuse a bounded pool of Go compressors. Preserve canonical source frame boundaries across Relay and direct transports, including plaintext replay and resync of remote proxy subscriptions. Independent coalescing is deliberately excluded: if one route delivers seq 1 and another later delivers seq 2 containing the bytes of seq 1 and 2, a final-seq-only receiver cannot trim the overlapping prefix. Compression operates on each canonical source chunk and adds no batching delay.

## Replay and sequencing

Preserve REPLAY_PROGRESS and pacing. Opaque replay preserves individual envelope boundaries and their original seq; it must never concatenate ciphertext chunks. If opaque backlog overflows, reconnect rather than slicing ciphertext into an invalid envelope. Resume publisher streams from the mirror's last stored seq to avoid unnecessary repeated history uploads. Synthetic reset output is not inserted into the monotonic scrollback sequence.

Frontend decompresses in an ordered queue that includes metadata, replay progress, and close. Recheck socket identity and route generation after asynchronous decode so late work cannot affect a detached/reconnected connection. Commit seq only after successful authentication and decoding.

## Related optimization decisions

Include compressor reuse, canonical remote output boundaries, opaque replay preservation, and cached replay cursors. Existing direct routes remain compatible ordinary OUT, with the common receive path accepting both formats. Do not change background subscription policy or direct preference defaults: those change interaction/recovery behavior and are not necessary to gain compression savings. Inspect session-list snapshot duplication, but only change it if demonstrated and independently testable.

## Validation

Use real codec and AEAD round trips, malformed/oversized decode tests, mixed subscribers with independent first/last transitions, ciphertext replay boundaries, legacy peers, reconnect cursors, no-viewer lazy upload, ordered frontend processing and stale generation tests. Run focused Go and frontend tests, then repository-required Go vet/tests and frontend/web builds plus contract checks. Rebuild committed web assets with the pinned build script. Preserve unrelated workspace changes.
