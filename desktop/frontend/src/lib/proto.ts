// AT Term wire protocol — TypeScript port of internal/proto. Frames are
// binary WebSocket messages: 6-byte header (ver + type + u32BE payload_len),
// 16-byte session id, then payload.

export const VERSION = 1;
export const HEADER_LEN = 6;
export const SID_LEN = 16;

export const TYPE = {
  OPEN: 0x01,
  IN: 0x02,
  OUT: 0x03,
  RESIZE: 0x04,
  META: 0x05,
  CLOSE: 0x06,
  ATTACH: 0x10,
  LIST: 0x11,
  LIST_RESP: 0x12,
  REPLAY_PROGRESS: 0x13,
  PREFS_CHANGED: 0x14,
  PING: 0x20,
  PONG: 0x21,
  PASTE_IMAGE: 0x33,
  CLAIM_DRIVER: 0x34,
  PASTE_FILE: 0x37,
  FS_REQUEST: 0x38,
  FS_RESPONSE: 0x39,
  FS_EVENT: 0x3a,
} as const;

export type FrameType = (typeof TYPE)[keyof typeof TYPE];

export interface Frame {
  type: FrameType;
  sid: Uint8Array;
  payload: Uint8Array;
}

export const NIL_SID = new Uint8Array(SID_LEN);

const enc = new TextEncoder();
const dec = new TextDecoder();

export function uuidParse(s: string): Uint8Array {
  const hex = s.replace(/-/g, "");
  if (hex.length !== 32) throw new Error("bad uuid");
  const out = new Uint8Array(SID_LEN);
  for (let i = 0; i < SID_LEN; i++) out[i] = parseInt(hex.substr(i * 2, 2), 16);
  return out;
}

export function encodeFrame(type: FrameType, sid: Uint8Array, payload?: Uint8Array): Uint8Array {
  const p = payload ?? new Uint8Array(0);
  const buf = new Uint8Array(HEADER_LEN + SID_LEN + p.length);
  const dv = new DataView(buf.buffer);
  buf[0] = VERSION;
  buf[1] = type;
  dv.setUint32(2, p.length, false);
  buf.set(sid, HEADER_LEN);
  buf.set(p, HEADER_LEN + SID_LEN);
  return buf;
}

export function decodeFrame(arr: Uint8Array): Frame {
  if (arr.length < HEADER_LEN + SID_LEN) throw new Error("short frame");
  if (arr[0] !== VERSION) throw new Error("bad version");
  const dv = new DataView(arr.buffer, arr.byteOffset, arr.byteLength);
  const plen = dv.getUint32(2, false);
  if (HEADER_LEN + SID_LEN + plen !== arr.length) throw new Error("bad len");
  return {
    type: arr[1] as FrameType,
    sid: arr.slice(HEADER_LEN, HEADER_LEN + SID_LEN),
    payload: arr.slice(HEADER_LEN + SID_LEN),
  };
}

export interface OutDecoded {
  seq: number;
  data: Uint8Array;
}

export function decodeOutPayload(p: Uint8Array): OutDecoded {
  if (p.length < 8) return { seq: 0, data: new Uint8Array(0) };
  const dv = new DataView(p.buffer, p.byteOffset, p.byteLength);
  const hi = dv.getUint32(0, false);
  const lo = dv.getUint32(4, false);
  return { seq: hi * 0x100000000 + lo, data: p.slice(8) };
}

export function encodeResize(cols: number, rows: number): Uint8Array {
  const b = new Uint8Array(4);
  const dv = new DataView(b.buffer);
  dv.setUint16(0, cols, false);
  dv.setUint16(2, rows, false);
  return b;
}

export function encodeText(s: string): Uint8Array {
  return enc.encode(s);
}
export function decodeText(b: Uint8Array): string {
  return dec.decode(b);
}
