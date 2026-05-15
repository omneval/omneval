import "@testing-library/jest-dom";

// Polyfill ResizeObserver for recharts in jsdom environment
global.ResizeObserver = class ResizeObserver {
  observe() {}
  unobserve() {}
  disconnect() {}
};
