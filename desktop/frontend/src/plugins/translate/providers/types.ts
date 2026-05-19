export interface TranslateResult {
  translated: string;
  // ISO 639-1 ("en", "zh", "ja") if the provider reported one;
  // "unknown" if the provider did not (e.g. fallback non-JSON mode).
  detectedSrcLang: string;
}

export type TranslateErrorCode =
  | "auth"        // 401/403
  | "rate_limit"  // 429
  | "server"      // 5xx
  | "network"     // fetch threw, no response
  | "timeout"     // AbortController fired due to 30s timeout
  | "aborted"     // user-triggered cancel (e.g. switched targetLang) — silent
  | "parse"       // provider returned non-JSON in JSON mode
  | "unknown";

export class TranslateError extends Error {
  constructor(
    public readonly code: TranslateErrorCode,
    message: string,
    public readonly httpStatus?: number,
    public readonly providerBody?: string,
  ) {
    super(message);
    this.name = "TranslateError";
  }
}

export interface TranslateProvider {
  translate(
    text: string,
    targetLang: string,
    opts: { signal: AbortSignal },
  ): Promise<TranslateResult>;
}
