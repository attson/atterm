import { t } from "../i18n";

function isLoopbackHost(host: string): boolean {
  const h = host.toLowerCase();
  if (h === "localhost" || h === "127.0.0.1" || h === "::1") return true;
  if (h.startsWith("127.")) return true;
  if (h.startsWith("[") && h.endsWith("]") && h.slice(1, -1) === "::1") return true;
  return false;
}

export function validateRelayBase(base: string, allowInsecure: boolean): string | null {
  const trimmed = base.trim();
  if (!trimmed) return t("mobile.relayUrlRequired");
  let u: URL;
  try {
    u = new URL(trimmed);
  } catch {
    return t("mobile.relayUrlMalformed");
  }
  if (u.protocol !== "http:" && u.protocol !== "https:") {
    return t("mobile.relayUrlProtocol");
  }
  if (u.pathname !== "/" || u.search !== "" || u.hash !== "") {
    return t("mobile.relayUrlNoPath");
  }
  if (u.protocol === "http:" && !isLoopbackHost(u.hostname) && !allowInsecure) {
    return t("mobile.relayUrlInsecure");
  }
  return null;
}

