import "@testing-library/jest-dom/vitest";

// Mock window.scrollTo since jsdom doesn't implement it
if (typeof window !== "undefined") {
  window.scrollTo = () => {};
}
