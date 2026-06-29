// Vitest global setup: register @testing-library/jest-dom matchers
// (e.g. toBeInTheDocument) and clean up the DOM between tests.
import "@testing-library/jest-dom/vitest";
import { afterEach } from "vitest";
import { cleanup } from "@testing-library/react";

afterEach(() => {
  cleanup();
});
