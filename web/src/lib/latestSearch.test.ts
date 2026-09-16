import { describe, expect, it } from "vitest";
import { latestSearchParams } from "./latestSearch";

// The browser path is covered under a real BrowserRouter in
// FilterPanel.dom.test.tsx; Node has no window.
describe("latestSearchParams without a window", () => {
  it("falls back to a copy of the router's params", () => {
    const params = new URLSearchParams("ticket=ACP-7&q=x");
    const latest = latestSearchParams(params);
    expect(latest.toString()).toBe("ticket=ACP-7&q=x");
    latest.delete("q");
    expect(params.toString()).toBe("ticket=ACP-7&q=x");
  });
});
