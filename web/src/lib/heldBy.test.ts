import { describe, expect, it } from "vitest";
import { heldDotColor, seenAgo } from "./heldBy";

const NOW = Date.parse("2026-09-28T12:00:00Z");
const ago = (ms: number) => new Date(NOW - ms).toISOString();

describe("seenAgo", () => {
  it("words the last seen in the largest whole unit", () => {
    expect(seenAgo(ago(20_000), NOW)).toBe("seen just now");
    expect(seenAgo(ago(4 * 60_000 + 30_000), NOW)).toBe("seen 4m ago");
    expect(seenAgo(ago(2 * 3_600_000 + 60_000), NOW)).toBe("seen 2h ago");
    expect(seenAgo(ago(3 * 86_400_000), NOW)).toBe("seen 3d ago");
  });

  it("says nothing for a missing or unreadable time, and just now for a clock ahead of ours", () => {
    expect(seenAgo(undefined, NOW)).toBe("");
    expect(seenAgo("not a time", NOW)).toBe("");
    expect(seenAgo(ago(-5_000), NOW)).toBe("seen just now");
  });
});

describe("heldDotColor", () => {
  it("is blue or violet while an agent works, and red while the ticket waits on the person", () => {
    expect(heldDotColor("in_progress")).toBe("bg-blue-500");
    expect(heldDotColor("agent_review")).toBe("bg-violet-500");
    expect(heldDotColor("needs_user_input")).toBe("bg-red-500");
  });
});
