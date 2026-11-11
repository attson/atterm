export type DeferredInputSender = {
  send(value: string): void;
  setTarget(target: (value: string) => void): void;
};

export function createDeferredInputSender(): DeferredInputSender {
  let target: ((value: string) => void) | null = null;
  const pending: string[] = [];
  return {
    send(value) {
      if (target) target(value);
      else pending.push(value);
    },
    setTarget(next) {
      target = next;
      const queued = pending.splice(0);
      for (const value of queued) next(value);
    },
  };
}
