import "@testing-library/jest-dom";

// Polyfill ResizeObserver for recharts in jsdom environment
declare global {
  interface Window {
    ResizeObserver: typeof ResizeObserverPolyfill;
  }
}

const ResizeObserverPolyfill = class {
  observe() {}
  unobserve() {}
  disconnect() {}
};

window.ResizeObserver = ResizeObserverPolyfill;

// Some Node versions ship an experimental (and non-functional without a
// flag) global localStorage that shadows jsdom's. Stub a real in-memory
// Storage so persistence behavior is testable everywhere.
const localStorageStore = new Map<string, string>();
Object.defineProperty(window, "localStorage", {
  value: {
    getItem: (k: string) => localStorageStore.get(k) ?? null,
    setItem: (k: string, v: string) => void localStorageStore.set(k, String(v)),
    removeItem: (k: string) => void localStorageStore.delete(k),
    clear: () => void localStorageStore.clear(),
    key: (i: number) => [...localStorageStore.keys()][i] ?? null,
    get length() {
      return localStorageStore.size;
    },
  },
  configurable: true,
});

export {};
