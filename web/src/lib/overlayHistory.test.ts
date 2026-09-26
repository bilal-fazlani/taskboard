import { describe, expect, it } from "vitest";
import { closedState, hasOverlay, overlayState, pushState, withoutOverlays } from "./overlayHistory";

describe("overlayState", () => {
  it("reads depth and fromLink from location state", () => {
    expect(overlayState({ overlayDepth: 2, overlayFromLink: true })).toEqual({ depth: 2, fromLink: true });
  });

  it("treats missing or malformed state as the plain view", () => {
    expect(overlayState(null)).toEqual({ depth: 0, fromLink: false });
    expect(overlayState(undefined)).toEqual({ depth: 0, fromLink: false });
    expect(overlayState({ overlayDepth: "2" })).toEqual({ depth: 0, fromLink: false });
    expect(overlayState({ overlayDepth: -1 })).toEqual({ depth: 0, fromLink: false });
  });
});

describe("pushState", () => {
  it("starts at depth 1 and records whether the base entry already had an overlay", () => {
    expect(pushState(null, false)).toEqual({ overlayDepth: 1, overlayFromLink: false });
    expect(pushState(null, true)).toEqual({ overlayDepth: 1, overlayFromLink: true });
  });

  it("adds one and carries fromLink from the entry below", () => {
    expect(pushState({ overlayDepth: 1, overlayFromLink: true }, false)).toEqual({
      overlayDepth: 2,
      overlayFromLink: true,
    });
  });

  it("keeps other state fields", () => {
    expect(pushState({ other: 1 }, false)).toEqual({ other: 1, overlayDepth: 1, overlayFromLink: false });
  });
});

describe("closedState", () => {
  it("drops the depth and fromLink and keeps other state fields", () => {
    expect(closedState({ other: 1, overlayDepth: 2, overlayFromLink: true })).toEqual({ other: 1 });
    expect(overlayState(closedState({ overlayDepth: 2 }))).toEqual({ depth: 0, fromLink: false });
    expect(closedState(null)).toEqual({});
  });
});

describe("overlay params", () => {
  it("knows which parameters are overlays", () => {
    expect(hasOverlay("/table", new URLSearchParams("status=todo"))).toBe(false);
    expect(hasOverlay("/table", new URLSearchParams("ticket=ACP-7"))).toBe(true);
    expect(hasOverlay("/epics", new URLSearchParams("epic=Launch"))).toBe(true);
  });

  it("counts epic as an overlay only in the Epics view, where it is the modal", () => {
    for (const path of ["/", "/kanban", "/table"]) {
      expect(hasOverlay(path, new URLSearchParams("epic=Launch"))).toBe(false);
    }
  });

  it("drops every overlay and keeps the rest in order", () => {
    const next = withoutOverlays("/table", new URLSearchParams("project=ACP&ticket=ACP-7&doc=Plan.md&status=todo"));
    expect(next.toString()).toBe("project=ACP&status=todo");
  });

  it("keeps a ticket view's epic filter and drops the Epics view's modal", () => {
    expect(withoutOverlays("/table", new URLSearchParams("epic=M1&ticket=ACP-7")).toString()).toBe("epic=M1");
    expect(withoutOverlays("/epics", new URLSearchParams("project=ACP&epic=M1&doc=a.md")).toString()).toBe("project=ACP");
  });
});
