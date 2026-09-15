import { describe, expect, it } from "vitest";
import { mergeSizes } from "./graphSizes";

describe("mergeSizes", () => {
  const prev = new Map([
    ["a", { width: 256, height: 96 }],
    ["b", { width: 256, height: 120 }],
  ]);

  it("returns the same map when every measurement matches, so the update bails out", () => {
    expect(mergeSizes(prev, [["a", { width: 256, height: 96 }]])).toBe(prev);
    expect(mergeSizes(prev, [])).toBe(prev);
  });

  it("returns a new map with changed and new cards, leaving the old one alone", () => {
    const next = mergeSizes(prev, [
      ["b", { width: 256, height: 140 }],
      ["c", { width: 256, height: 80 }],
    ]);
    expect(next).not.toBe(prev);
    expect([...next]).toEqual([
      ["a", { width: 256, height: 96 }],
      ["b", { width: 256, height: 140 }],
      ["c", { width: 256, height: 80 }],
    ]);
    expect(prev.get("b")).toEqual({ width: 256, height: 120 });
    expect(prev.has("c")).toBe(false);
  });

  it("keeps the last of repeated measurements for one card", () => {
    const next = mergeSizes(prev, [
      ["a", { width: 256, height: 100 }],
      ["a", { width: 256, height: 96 }],
    ]);
    expect(next.get("a")).toEqual({ width: 256, height: 96 });
  });
});
