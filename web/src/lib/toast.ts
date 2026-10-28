import { readable, type Readable } from "svelte/store";

const TOAST_TTL_MS = 3000;

let nextId = 1;
const live = new Map<number, string>();
const subscribers: Array<(value: string[]) => void> = [];

function notify(): void {
  const snapshot = [...live.values()];
  for (const push of subscribers) push(snapshot);
}

export const toasts: Readable<string[]> = {
  subscribe(subscriber) {
    subscribers.push(subscriber);
    subscriber([...live.values()]);
    return () => {
      const index = subscribers.indexOf(subscriber);
      if (index >= 0) subscribers.splice(index, 1);
    };
  },
};

export function toast(message: string): void {
  const id = nextId;
  nextId += 1;
  live.set(id, message);
  notify();
  setTimeout(() => {
    live.delete(id);
    notify();
  }, TOAST_TTL_MS);
}
