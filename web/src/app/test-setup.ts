import "@testing-library/jest-dom/vitest";

import { cleanup, configure } from "@testing-library/react";
import { afterEach } from "vitest";

// findBy and waitFor give up after 1000 ms by default. Route tests render
// through the real router, query cache and fake transport, and with make check
// sharing the host with the Go tiers a first render can take longer than that.
// The wait ends as soon as the element appears, so a passing test is no slower.
configure({ asyncUtilTimeout: 5000 });

afterEach(() => {
  cleanup();
});
