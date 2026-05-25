export const en = {
  common: {
    appName: 'AT Term',
    cancel: 'Cancel',
    confirm: 'Confirm',
    loading: 'Loading...',
    save: 'Save',
  },
  topbar: {
    admin: 'Admin',
    main: 'Sessions',
    settings: 'Settings',
    signOut: 'Sign out',
  },
  auth: {
    email: 'Email',
    password: 'Password',
    signIn: 'Sign in',
    signUp: 'Sign up',
  },
  setup: {
    title: 'Set up relay',
    relayUrl: 'Relay URL',
    token: 'API token',
  },
  main: {
    empty: 'No sessions yet',
    reconnect: 'Reconnect',
    title: 'Remote sessions',
  },
  terminal: {
    connecting: 'Connecting...',
    disconnected: 'Disconnected',
    paste: 'Paste',
  },
  sessions: {
    attach: 'Attach',
    created: 'Created',
    host: 'Host',
  },
  settings: {
    title: 'Settings',
    language: 'Language',
    notifications: 'Notifications',
  },
  admin: {
    title: 'Admin',
    users: 'Users',
    invitations: 'Invitations',
  },
  test: {
    interpolation: '{count} sessions',
  },
} as const;

type DeepStringRecord<T> = {
  readonly [K in keyof T]: T[K] extends string ? string : DeepStringRecord<T[K]>;
};

export type Messages = DeepStringRecord<typeof en>;
