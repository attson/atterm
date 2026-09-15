import { describe, expect, test } from "vitest";
import { en } from "./en";
import { zhCN } from "./zh-CN";

const KEYS = [
  "title",
  "currentToLatest",
  "releaseNotes",
  "statusAvailable",
  "statusDownloading",
  "statusReady",
  "downloadInstall",
  "cancel",
  "cancelling",
  "installRestart",
  "later",
] as const;

describe("startupUpdate i18n", () => {
  test("en has every startupUpdate key", () => {
    for (const k of KEYS) {
      expect((en as any).startupUpdate?.[k], `en.startupUpdate.${k}`).toBeTruthy();
    }
  });
  test("zh-CN mirrors every startupUpdate key", () => {
    for (const k of KEYS) {
      expect((zhCN as any).startupUpdate?.[k], `zh-CN.startupUpdate.${k}`).toBeTruthy();
    }
  });
});
