// Mirrors internal/session/ClassifyCommand for the {claude, codex, aider}
// subset. v1 deliberately omits gemini — its session-id story isn't stable
// enough to sniff. See spec §2 and §10 (graceful degrade for alias miss).
const WRAPPERS = new Set(["sudo", "time", "nice", "env"]);
const ENV_ASSIGN = /^[A-Z_][A-Z0-9_]*=/;

export type AIKind = "claude" | "codex" | "aider" | "";

export function classifyAIKind(command: string): AIKind {
  let tokens = command.trim().split(/\s+/).filter(Boolean);
  while (tokens.length > 0) {
    const t = tokens[0];
    if (WRAPPERS.has(t) || ENV_ASSIGN.test(t)) {
      tokens = tokens.slice(1);
      continue;
    }
    break;
  }
  if (tokens.length === 0) return "";
  const first = tokens[0].split("/").pop() ?? "";
  switch (first) {
    case "claude":
      return "claude";
    case "codex":
      return "codex";
    case "aider":
      return "aider";
    default:
      return "";
  }
}
