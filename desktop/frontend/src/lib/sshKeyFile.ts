// Helpers for importing an SSH private key from a local file — used by both
// the "Import from key file" picker and the drag-and-drop zone in the Keys
// panel. Kept free of DOM/Vue so the parsing rules can be tested directly.

// A 4096-bit RSA key in PEM is ~3.2 KiB; 256 KiB leaves generous headroom
// while still rejecting anything that is obviously not a key (images, tarballs).
export const MAX_KEY_FILE_BYTES = 256 * 1024;

const PRIVATE_KEY_HEADER = /-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----/;

// looksLikePrivateKey is a cheap sanity check so we can reject a misdropped
// .pub, ssh_config, or binary file before bothering the backend parser.
export function looksLikePrivateKey(text: string): boolean {
  return PRIVATE_KEY_HEADER.test(text);
}

const OPENSSH_MAGIC = "openssh-key-v1\0";

// detectEncryptedPem reports whether the key needs a passphrase to unlock.
// Three encodings matter in practice:
//   - PKCS#8:      the header itself says ENCRYPTED PRIVATE KEY
//   - legacy PEM:  a "Proc-Type: 4,ENCRYPTED" header line
//   - OpenSSH v1:  a cipher name other than "none" in the binary preamble
// Anything unparsable is reported as not encrypted; the backend's real parse
// is the authority, this only drives a UI hint.
export function detectEncryptedPem(pem: string): boolean {
  if (!pem) return false;
  if (pem.includes("ENCRYPTED PRIVATE KEY")) return true;
  if (/^\s*Proc-Type:\s*4,ENCRYPTED/m.test(pem)) return true;
  if (!pem.includes("BEGIN OPENSSH PRIVATE KEY")) return false;
  const cipher = opensshCipherName(pem);
  return cipher !== null && cipher !== "none";
}

// opensshCipherName decodes the OpenSSH v1 preamble far enough to read the
// cipher name: magic, then a uint32-length-prefixed string. Returns null when
// the body is not decodable or does not look like an OpenSSH v1 blob.
function opensshCipherName(pem: string): string | null {
  const body = pem
    .replace(/-----BEGIN [^-]*-----/g, "")
    .replace(/-----END [^-]*-----/g, "")
    .replace(/\s+/g, "");
  if (body === "") return null;
  let raw: string;
  try {
    raw = atob(body);
  } catch {
    return null;
  }
  if (!raw.startsWith(OPENSSH_MAGIC)) return null;
  const at = OPENSSH_MAGIC.length;
  if (raw.length < at + 4) return null;
  const len =
    (raw.charCodeAt(at) << 24) |
    (raw.charCodeAt(at + 1) << 16) |
    (raw.charCodeAt(at + 2) << 8) |
    raw.charCodeAt(at + 3);
  if (len < 0 || raw.length < at + 4 + len) return null;
  return raw.slice(at + 4, at + 4 + len);
}

// Extensions worth stripping when deriving a key name. Everything else — most
// importantly bare names like id_rsa, and domain-ish names like my.company.io —
// is kept verbatim.
const STRIPPABLE_EXTENSIONS = [".pem", ".key", ".txt"];

// Blob.prototype.text() is missing on older WebKit (and in jsdom), so fall back
// to FileReader rather than assuming it exists.
function readFileText(file: File): Promise<string> {
  if (typeof file.text === "function") return file.text();
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(String(reader.result ?? ""));
    reader.onerror = () => reject(reader.error ?? new Error("could not read file"));
    reader.readAsText(file);
  });
}

export type ImportedKeyFile = {
  pem: string;
  suggestedName: string;
  encrypted: boolean;
};

// readPrivateKeyFile validates a picked/dropped file and extracts what the Key
// drawer needs. Rejects with a message meant for the panel's error banner; the
// backend still does the authoritative parse on save.
export async function readPrivateKeyFile(file: File): Promise<ImportedKeyFile> {
  if (file.size > MAX_KEY_FILE_BYTES) {
    throw new Error(`${file.name} is too large to be a private key.`);
  }
  const pem = await readFileText(file);
  if (!looksLikePrivateKey(pem)) {
    throw new Error(`${file.name} is not a private key file.`);
  }
  return {
    pem,
    suggestedName: keyNameFromFilename(file.name),
    encrypted: detectEncryptedPem(pem),
  };
}

// keyNameFromFilename turns a dropped file's name into a default vault label.
export function keyNameFromFilename(filename: string): string {
  const base = filename.split(/[/\\]/).pop() ?? "";
  const lower = base.toLowerCase();
  for (const ext of STRIPPABLE_EXTENSIONS) {
    if (lower.endsWith(ext) && base.length > ext.length) {
      return base.slice(0, -ext.length);
    }
  }
  return base;
}
