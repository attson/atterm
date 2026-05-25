export const en = {
  common: {
    language: "Language",
    system: "System",
    english: "English",
    simplifiedChinese: "Simplified Chinese",
    save: "Save",
    cancel: "Cancel",
    close: "Close",
    refresh: "Refresh",
    copy: "Copy",
    paste: "Paste",
    send: "Send",
    disconnect: "Disconnect",
    connect: "Connect",
    settings: "Settings",
  },
  settings: {
    general: {
      languageLabel: "Language",
      languageHint: "Use System to follow your device language.",
    },
  },
  test: {
    interpolated: "{count} sessions",
    englishOnly: "English only",
  },
} as const;

type WidenMessageValues<T> = {
  readonly [Key in keyof T]: T[Key] extends string ? string : WidenMessageValues<T[Key]>;
};

export type Messages = WidenMessageValues<typeof en>;
