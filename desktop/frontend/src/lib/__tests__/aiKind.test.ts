import { describe, it, expect } from "vitest";
import { classifyAIKind } from "../aiKind";

describe("classifyAIKind", () => {
  it("identifies claude by bare name", () => {
    expect(classifyAIKind("claude")).toBe("claude");
  });
  it("identifies codex", () => {
    expect(classifyAIKind("codex resume some-id")).toBe("codex");
  });
  it("identifies aider", () => {
    expect(classifyAIKind("aider --model gpt-4")).toBe("aider");
  });
  it("strips absolute path", () => {
    expect(classifyAIKind("/opt/homebrew/bin/claude --foo")).toBe("claude");
  });
  it("strips env assigns + wrappers", () => {
    expect(classifyAIKind("ANTHROPIC_API_KEY=x sudo claude")).toBe("claude");
    expect(classifyAIKind("time codex")).toBe("codex");
  });
  it("returns empty for non-AI", () => {
    expect(classifyAIKind("/bin/zsh")).toBe("");
    expect(classifyAIKind("ls -la")).toBe("");
    expect(classifyAIKind("")).toBe("");
  });
  it("gemini is treated as non-AI (out of scope v1)", () => {
    expect(classifyAIKind("gemini")).toBe("");
  });
});
