import type { Messages } from './en';

export const zhCN = {
  common: {
    appName: 'AT Term',
    cancel: '取消',
    confirm: '确认',
    loading: '正在加载...',
    save: '保存',
  },
  topbar: {
    admin: '管理',
    main: '会话',
    settings: '设置',
    signOut: '退出登录',
  },
  auth: {
    email: '邮箱',
    password: '密码',
    signIn: '登录',
    signUp: '注册',
  },
  setup: {
    title: '设置 relay',
    relayUrl: 'Relay 地址',
    token: 'API token',
  },
  main: {
    empty: '还没有会话',
    reconnect: '重新连接',
    title: '远程会话',
  },
  terminal: {
    connecting: '正在连接...',
    disconnected: '连接已断开',
    paste: '粘贴',
  },
  sessions: {
    attach: '接管',
    created: '创建时间',
    host: '主机',
  },
  settings: {
    title: '设置',
    language: '语言',
    notifications: '通知',
  },
  admin: {
    title: '管理',
    users: '用户',
    invitations: '邀请',
  },
  test: {
    interpolation: '{count} 个会话',
  },
} satisfies Messages;
