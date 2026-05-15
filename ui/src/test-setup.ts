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

export {};
