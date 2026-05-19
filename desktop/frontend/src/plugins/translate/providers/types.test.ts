import { describe, expect, it } from "vitest";
import { TranslateError, type TranslateErrorCode, type TranslateResult, type TranslateProvider } from "./types";

describe("TranslateError", () => {
  it("carries code, message, httpStatus, providerBody", () => {
    const e = new TranslateError("auth", "Auth failed", 401, '{"error":"invalid_api_key"}');
    expect(e.name).toBe("TranslateError");
    expect(e.code).toBe<TranslateErrorCode>("auth");
    expect(e.message).toBe("Auth failed");
    expect(e.httpStatus).toBe(401);
    expect(e.providerBody).toBe('{"error":"invalid_api_key"}');
    expect(e instanceof Error).toBe(true);
  });

  it("works without optional fields", () => {
    const e = new TranslateError("network", "fetch failed");
    expect(e.httpStatus).toBeUndefined();
    expect(e.providerBody).toBeUndefined();
  });

  it("TranslateResult shape compiles", () => {
    const r: TranslateResult = { translated: "你好", detectedSrcLang: "en" };
    expect(r.translated).toBe("你好");
  });

  it("TranslateProvider interface compiles", async () => {
    const stub: TranslateProvider = {
      translate: async () => ({ translated: "", detectedSrcLang: "unknown" }),
    };
    const out = await stub.translate("hi", "zh-CN", { signal: new AbortController().signal });
    expect(out.detectedSrcLang).toBe("unknown");
  });
});
