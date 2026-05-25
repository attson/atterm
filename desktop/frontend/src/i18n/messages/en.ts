export type MessageSchema = {
  common: {
    language: string;
    system: string;
    english: string;
    simplifiedChinese: string;
    save: string;
    cancel: string;
    close: string;
    refresh: string;
    copy: string;
    paste: string;
    send: string;
    disconnect: string;
    connect: string;
    settings: string;
  };
  settings: {
    general: {
      languageLabel: string;
      languageHint: string;
    };
  };
  test: {
    interpolated: string;
    englishOnly: string;
  };
};

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
} satisfies MessageSchema;

export type Messages = typeof en;
