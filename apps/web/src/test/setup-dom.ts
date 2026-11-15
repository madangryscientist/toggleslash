import { cleanup } from "@testing-library/react";
import { afterEach } from "vitest";

// Vitest `globals` is off, so Testing Library cannot register its own cleanup: unmount here.
afterEach(() => {
  cleanup();
});
