// @vitest-environment jsdom
import { describe, expect, it } from "vitest";
import { latestLocationState } from "./latestSearch";

describe("latestLocationState in a browser", () => {
  it("reads the state BrowserRouter keeps under usr, not the fallback", () => {
    window.history.pushState({ usr: { overlayDepth: 2 }, key: "k", idx: 1 }, "", "/kanban");
    expect(latestLocationState({ overlayDepth: 9 })).toEqual({ overlayDepth: 2 });
  });

  it("is undefined for an entry the router gave no state", () => {
    window.history.replaceState(null, "", "/");
    expect(latestLocationState({ overlayDepth: 9 })).toBeUndefined();
  });
});
