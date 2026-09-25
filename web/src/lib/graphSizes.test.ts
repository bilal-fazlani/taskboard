import { describe, expect, it } from "vitest";
import { entrySize, mergeSizes, pruneSizes } from "./graphSizes";

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

describe("entrySize", () => {
  // offsetWidth and offsetHeight round; the border box doesn't.
  const target = { offsetWidth: 256, offsetHeight: 98 } as unknown as Element;

  it("reads the exact border box", () => {
    expect(entrySize({ target, borderBoxSize: [{ inlineSize: 256, blockSize: 97.5 }] })).toEqual({ width: 256, height: 97.5 });
  });

  it("falls back to the element's rounded size when the entry has no border box", () => {
    expect(entrySize({ target })).toEqual({ width: 256, height: 98 });
    expect(entrySize({ target, borderBoxSize: [] })).toEqual({ width: 256, height: 98 });
  });
});

describe("pruneSizes", () => {
  const prev = new Map([
    ["a", { width: 256, height: 96 }],
    ["b", { width: 256, height: 120 }],
  ]);

  it("returns the same map while every known card is still on the graph", () => {
    expect(pruneSizes(prev, new Set(["a", "b"]))).toBe(prev);
    // A card not measured yet is no reason to copy.
    expect(pruneSizes(prev, new Set(["a", "b", "c"]))).toBe(prev);
  });

  it("drops the cards that have left, leaving the old map alone", () => {
    const next = pruneSizes(prev, new Set(["b"]));
    expect([...next]).toEqual([["b", { width: 256, height: 120 }]]);
    expect(prev.size).toBe(2);
    expect(pruneSizes(prev, new Set()).size).toBe(0);
  });
});
