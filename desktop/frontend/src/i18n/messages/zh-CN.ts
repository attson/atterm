import type { Messages } from "./en";

export const zhCN = {
  common: {
    language: "语言",
    system: "跟随系统",
    english: "英语",
    simplifiedChinese: "简体中文",
    save: "保存",
    cancel: "取消",
    close: "关闭",
    refresh: "刷新",
    copy: "复制",
    paste: "粘贴",
    send: "发送",
    disconnect: "断开连接",
    connect: "连接",
    settings: "设置",
  },
  settings: {
    general: {
      languageLabel: "语言",
      languageHint: "选择跟随系统时，会使用设备语言。",
    },
  },
  test: {
    interpolated: "{count} 个会话",
    englishOnly: "仅英语",
  },
} as const satisfies Messages;
