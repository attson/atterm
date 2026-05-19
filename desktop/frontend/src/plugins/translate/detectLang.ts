// hasCJK returns true if the string contains any Chinese / Japanese kanji /
// Korean Hanja character in the CJK Unified Ideographs block (U+4E00..U+9FFF).
// Used to decide auto target lang. Hiragana / katakana / hangul aren't
// included here on purpose: a script of pure hiragana doesn't mean the user
// wants Chinese-out behavior — but their default target lang still applies.
export function hasCJK(s: string): boolean {
  return /[一-鿿]/.test(s);
}

// computeAutoTargetLang picks a target language for an auto-detected source:
//   - If the text contains CJK and the configured default is zh-CN, flip to "en"
//     so the user gets a non-Chinese result for already-Chinese text.
//   - Otherwise use the configured default.
export function computeAutoTargetLang(text: string, defaultTargetLang: string): string {
  if (hasCJK(text) && defaultTargetLang === "zh-CN") {
    return "en";
  }
  return defaultTargetLang;
}
