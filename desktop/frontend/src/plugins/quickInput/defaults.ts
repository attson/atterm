import type { QuickInputButton } from "../configStore";

let counter = 0;
function clientID(): string {
  return `qib-${Date.now()}-${counter++}`;
}

export function defaultButtons(): QuickInputButton[] {
  return [
    { id: clientID(), label: "ok", send: "ok", appendNewline: true },
    { id: clientID(), label: "continue", send: "continue", appendNewline: true },
    { id: clientID(), label: "发布", send: "发布", appendNewline: true },
  ];
}
