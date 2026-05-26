import { TranslateError, type TranslateProvider, type TranslateResult } from "./types";
import { t } from "../../../i18n";

const REQUEST_TIMEOUT_MS = 30_000;

export interface OpenAIProviderConfig {
  baseUrl: string;  // e.g. "https://api.openai.com" — no trailing slash, no /v1
  apiKey: string;
  model: string;
}

// createOpenAIProvider returns a stateful TranslateProvider that remembers
// whether the endpoint supports response_format. After a single failure
// matching response_format / json_object, all subsequent requests use the
// plain-text fallback prompt.
export function createOpenAIProvider(cfg: OpenAIProviderConfig): TranslateProvider {
  let supportsJsonMode = true;

  async function call(text: string, targetLang: string, signal: AbortSignal, useJsonMode: boolean): Promise<TranslateResult> {
    const url = cfg.baseUrl.replace(/\/+$/, "") + "/v1/chat/completions";

    const systemJson = `You are a translation engine. Detect the source language and translate to ${targetLang}. Respond with strict JSON: {"detectedSrcLang":"<ISO 639-1 code>","translated":"<translation>"}. No commentary, no markdown.`;
    const systemPlain = `You are a translation engine. Translate the following text to ${targetLang}. Output ONLY the translation, no explanation, no quotes, no markdown.`;

    const body: Record<string, unknown> = {
      model: cfg.model,
      messages: [
        { role: "system", content: useJsonMode ? systemJson : systemPlain },
        { role: "user", content: text },
      ],
      temperature: 0.2,
    };
    if (useJsonMode) body.response_format = { type: "json_object" };

    // Wrap user-supplied abort signal + 30s timeout into a combined signal.
    const timeoutCtl = new AbortController();
    const timer = setTimeout(() => timeoutCtl.abort(), REQUEST_TIMEOUT_MS);
    const onUserAbort = () => timeoutCtl.abort();
    signal.addEventListener("abort", onUserAbort);

    let res: Response;
    try {
      res = await fetch(url, {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          Authorization: `Bearer ${cfg.apiKey}`,
        },
        body: JSON.stringify(body),
        signal: timeoutCtl.signal,
      });
    } catch (e) {
      const isAbort = e instanceof DOMException && e.name === "AbortError";
      if (isAbort) {
        if (signal.aborted) throw new TranslateError("aborted", t("plugins.translate.userCancelled"));
        throw new TranslateError("timeout", t("plugins.translate.requestTimedOut", { seconds: REQUEST_TIMEOUT_MS / 1000 }));
      }
      throw new TranslateError("network", e instanceof Error ? e.message : String(e));
    } finally {
      clearTimeout(timer);
      signal.removeEventListener("abort", onUserAbort);
    }

    const bodyText = await res.text();

    if (res.status === 401 || res.status === 403) {
      throw new TranslateError("auth", t("plugins.translate.authFailed", { status: res.status }), res.status, bodyText.slice(0, 200));
    }
    if (res.status === 429) {
      throw new TranslateError("rate_limit", t("plugins.translate.rateLimited"), res.status, bodyText.slice(0, 200));
    }
    if (res.status >= 500) {
      throw new TranslateError("server", t("plugins.translate.serverError", { status: res.status }), res.status, bodyText.slice(0, 200));
    }
    if (res.status === 400 && useJsonMode && /response_format|json_object/i.test(bodyText)) {
      // Endpoint doesn't support JSON mode; mark and let caller fallback.
      supportsJsonMode = false;
      throw new ResponseFormatNotSupportedError();
    }
    if (!res.ok) {
      throw new TranslateError("unknown", `HTTP ${res.status}`, res.status, bodyText.slice(0, 200));
    }

    let envelope: { choices?: Array<{ message?: { content?: string } }> };
    try {
      envelope = JSON.parse(bodyText);
    } catch {
      throw new TranslateError("parse", t("plugins.translate.providerNonJson"), res.status, bodyText.slice(0, 200));
    }
    const content = envelope.choices?.[0]?.message?.content ?? "";

    if (!useJsonMode) {
      return { translated: content.trim(), detectedSrcLang: "unknown" };
    }
    try {
      const inner = JSON.parse(content) as { translated?: unknown; detectedSrcLang?: unknown };
      if (typeof inner.translated !== "string") throw new Error(t("plugins.translate.missingTranslated"));
      const detected = typeof inner.detectedSrcLang === "string" ? inner.detectedSrcLang : "unknown";
      return { translated: inner.translated, detectedSrcLang: detected };
    } catch {
      // Provider sent JSON-mode response but the content isn't valid JSON.
      // Per spec: degrade gracefully — show raw content as translated.
      return { translated: content, detectedSrcLang: "unknown" };
    }
  }

  return {
    async translate(text, targetLang, opts) {
      try {
        return await call(text, targetLang, opts.signal, supportsJsonMode);
      } catch (e) {
        if (e instanceof ResponseFormatNotSupportedError) {
          // Retry once with plain mode. supportsJsonMode is already false.
          return await call(text, targetLang, opts.signal, false);
        }
        throw e;
      }
    },
  };
}

class ResponseFormatNotSupportedError extends Error {
  constructor() { super(t("plugins.translate.responseFormatUnsupported")); }
}
