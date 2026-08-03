import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createOpenAIProvider } from "./openai";
import { TranslateError } from "./types";

function mkResponse(body: unknown, init: { status?: number; ok?: boolean } = {}): Response {
  const status = init.status ?? 200;
  return new Response(typeof body === "string" ? body : JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

function mkChoice(content: string) {
  return { choices: [{ message: { content } }] };
}

const baseConfig = {
  baseUrl: "https://api.openai.com",
  apiKey: "sk-test",
  model: "gpt-4o-mini",
};

describe("openai provider — happy path", () => {
  beforeEach(() => { vi.useFakeTimers(); });
  afterEach(() => { vi.useRealTimers(); vi.restoreAllMocks(); });

  it("parses JSON response and returns TranslateResult", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      mkResponse(mkChoice(JSON.stringify({ detectedSrcLang: "en", translated: "你好" }))),
    );
    vi.stubGlobal("fetch", fetchMock);
    const p = createOpenAIProvider(baseConfig);
    const r = await p.translate("hello", "zh-CN", { signal: new AbortController().signal });
    expect(r).toEqual({ translated: "你好", detectedSrcLang: "en" });
    expect(fetchMock).toHaveBeenCalledOnce();
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe("https://api.openai.com/v1/chat/completions");
    expect((init as RequestInit).method).toBe("POST");
    const body = JSON.parse((init as RequestInit).body as string);
    expect(body.model).toBe("gpt-4o-mini");
    expect(body.response_format).toEqual({ type: "json_object" });
  });
});

describe("openai provider — injected transport", () => {
  afterEach(() => { vi.restoreAllMocks(); });

  it("uses opts.transport instead of global fetch when provided", async () => {
    const fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);
    const transport = vi.fn().mockResolvedValue(
      mkResponse(mkChoice(JSON.stringify({ detectedSrcLang: "en", translated: "你好" }))),
    );
    const p = createOpenAIProvider(baseConfig, { transport });
    const r = await p.translate("hello", "zh-CN", { signal: new AbortController().signal });
    expect(r).toEqual({ translated: "你好", detectedSrcLang: "en" });
    expect(transport).toHaveBeenCalledOnce();
    expect(fetchMock).not.toHaveBeenCalled();
    const [url, init] = transport.mock.calls[0];
    expect(url).toBe("https://api.openai.com/v1/chat/completions");
    expect((init as RequestInit).method).toBe("POST");
  });
});

describe("openai provider — extraParams merge", () => {
  afterEach(() => { vi.restoreAllMocks(); });

  it("merges extraParams into body and overrides defaults", async () => {
    const transport = vi.fn().mockResolvedValue(
      mkResponse(mkChoice(JSON.stringify({ detectedSrcLang: "en", translated: "ok" }))),
    );
    const p = createOpenAIProvider(
      { ...baseConfig, extraParams: '{"stream":true,"top_p":0.9,"temperature":0.9}' },
      { transport },
    );
    await p.translate("x", "en", { signal: new AbortController().signal });
    const [, init] = transport.mock.calls[0];
    const body = JSON.parse((init as RequestInit).body as string);
    expect(body.stream).toBe(true);
    expect(body.top_p).toBe(0.9);
    // extraParams wins over the default temperature.
    expect(body.temperature).toBe(0.9);
    // Original messages still present.
    expect(body.messages).toHaveLength(2);
  });

  it("empty / whitespace extraParams is a no-op", async () => {
    const transport = vi.fn().mockResolvedValue(
      mkResponse(mkChoice(JSON.stringify({ detectedSrcLang: "en", translated: "ok" }))),
    );
    const p = createOpenAIProvider({ ...baseConfig, extraParams: "   " }, { transport });
    await p.translate("x", "en", { signal: new AbortController().signal });
    const [, init] = transport.mock.calls[0];
    const body = JSON.parse((init as RequestInit).body as string);
    expect(body.stream).toBeUndefined();
    expect(body.temperature).toBe(0.2);
  });

  it("malformed extraParams JSON → TranslateError", async () => {
    const transport = vi.fn();
    const p = createOpenAIProvider({ ...baseConfig, extraParams: "not json" }, { transport });
    await expect(p.translate("x", "en", { signal: new AbortController().signal }))
      .rejects.toBeInstanceOf(TranslateError);
    expect(transport).not.toHaveBeenCalled();
  });

  it("extraParams that isn't a JSON object → TranslateError", async () => {
    const transport = vi.fn();
    const p = createOpenAIProvider({ ...baseConfig, extraParams: "[1,2,3]" }, { transport });
    await expect(p.translate("x", "en", { signal: new AbortController().signal }))
      .rejects.toBeInstanceOf(TranslateError);
    expect(transport).not.toHaveBeenCalled();
  });
});

describe("openai provider — error mapping", () => {
  afterEach(() => { vi.restoreAllMocks(); });

  it("401 → TranslateError code 'auth'", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(
      mkResponse({ error: "invalid_api_key" }, { status: 401 }),
    ));
    const p = createOpenAIProvider(baseConfig);
    await expect(p.translate("x", "en", { signal: new AbortController().signal }))
      .rejects.toMatchObject({ code: "auth", httpStatus: 401 });
  });

  it("429 → TranslateError code 'rate_limit'", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(
      mkResponse({ error: "rate_limited" }, { status: 429 }),
    ));
    const p = createOpenAIProvider(baseConfig);
    await expect(p.translate("x", "en", { signal: new AbortController().signal }))
      .rejects.toMatchObject({ code: "rate_limit", httpStatus: 429 });
  });

  it("5xx → TranslateError code 'server'", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(
      mkResponse("upstream gone", { status: 502 }),
    ));
    const p = createOpenAIProvider(baseConfig);
    await expect(p.translate("x", "en", { signal: new AbortController().signal }))
      .rejects.toMatchObject({ code: "server", httpStatus: 502 });
  });

  it("fetch rejects (network) → TranslateError code 'network'", async () => {
    vi.stubGlobal("fetch", vi.fn().mockRejectedValue(new TypeError("Failed to fetch")));
    const p = createOpenAIProvider(baseConfig);
    await expect(p.translate("x", "en", { signal: new AbortController().signal }))
      .rejects.toMatchObject({ code: "network" });
  });

  it("user-triggered abort → TranslateError code 'aborted'", async () => {
    const ctl = new AbortController();
    vi.stubGlobal("fetch", vi.fn().mockImplementation(
      (_url: string, init: RequestInit) =>
        new Promise((_resolve, reject) => {
          (init.signal as AbortSignal).addEventListener("abort", () => {
            const err = new DOMException("aborted", "AbortError");
            reject(err);
          });
        }),
    ));
    const p = createOpenAIProvider(baseConfig);
    const promise = p.translate("x", "en", { signal: ctl.signal });
    ctl.abort();
    await expect(promise).rejects.toMatchObject({ code: "aborted" });
  });
});

describe("openai provider — response_format fallback", () => {
  afterEach(() => { vi.restoreAllMocks(); });

  it("retries without response_format when endpoint rejects it", async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(
        mkResponse({ error: "response_format is not supported" }, { status: 400 }),
      )
      .mockResolvedValueOnce(
        mkResponse(mkChoice("你好")),  // raw text in fallback mode
      );
    vi.stubGlobal("fetch", fetchMock);
    const p = createOpenAIProvider(baseConfig);
    const r = await p.translate("hello", "zh-CN", { signal: new AbortController().signal });
    expect(fetchMock).toHaveBeenCalledTimes(2);
    expect(r).toEqual({ translated: "你好", detectedSrcLang: "unknown" });
    const secondBody = JSON.parse((fetchMock.mock.calls[1][1] as RequestInit).body as string);
    expect(secondBody.response_format).toBeUndefined();
  });

  it("subsequent call after fallback skips response_format from the start", async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(
        mkResponse({ error: "response_format is not supported" }, { status: 400 }),
      )
      .mockResolvedValueOnce(mkResponse(mkChoice("你好")))
      .mockResolvedValueOnce(mkResponse(mkChoice("再见")));
    vi.stubGlobal("fetch", fetchMock);
    const p = createOpenAIProvider(baseConfig);
    await p.translate("hello", "zh-CN", { signal: new AbortController().signal });
    await p.translate("bye", "zh-CN", { signal: new AbortController().signal });
    expect(fetchMock).toHaveBeenCalledTimes(3);
    const thirdBody = JSON.parse((fetchMock.mock.calls[2][1] as RequestInit).body as string);
    expect(thirdBody.response_format).toBeUndefined();
  });

  it("malformed JSON in JSON mode → returns raw content, no throw", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(
      mkResponse(mkChoice("not valid json")),
    ));
    const p = createOpenAIProvider(baseConfig);
    const r = await p.translate("hello", "zh-CN", { signal: new AbortController().signal });
    expect(r).toEqual({ translated: "not valid json", detectedSrcLang: "unknown" });
  });
});
