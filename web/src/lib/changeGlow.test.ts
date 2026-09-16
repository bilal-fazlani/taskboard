import { describe, expect, it } from "vitest";
import { changedSince, fetchStamps } from "./changeGlow";

const at = (id: string, updatedAt: string) => ({ id, updatedAt });

describe("fetchStamps", () => {
  it("maps each ticket id to its updatedAt", () => {
    expect(fetchStamps([at("a", "t1"), at("b", "t2")])).toEqual(
      new Map([
        ["a", "t1"],
        ["b", "t2"],
      ]),
    );
  });

  it("takes an empty list", () => {
    expect(fetchStamps([])).toEqual(new Map());
  });
});

describe("changedSince", () => {
  it("glows nothing on the first fetch, so a page load does not light the whole graph", () => {
    expect(changedSince(null, fetchStamps([at("a", "t1"), at("b", "t1")]))).toEqual([]);
  });

  it("glows a ticket whose updatedAt moved", () => {
    const before = fetchStamps([at("a", "t1"), at("b", "t1")]);
    const after = fetchStamps([at("a", "t2"), at("b", "t1")]);
    expect(changedSince(before, after)).toEqual(["a"]);
  });

  it("glows a ticket that was not there before", () => {
    const before = fetchStamps([at("a", "t1")]);
    const after = fetchStamps([at("a", "t1"), at("b", "t1")]);
    expect(changedSince(before, after)).toEqual(["b"]);
  });

  it("glows everything when the graph was empty and tickets arrived", () => {
    expect(changedSince(fetchStamps([]), fetchStamps([at("a", "t1"), at("b", "t1")]))).toEqual([
      "a",
      "b",
    ]);
  });

  it("glows nothing when a refetch brought the same tickets back unchanged", () => {
    const before = fetchStamps([at("a", "t1"), at("b", "t2")]);
    const after = fetchStamps([at("a", "t1"), at("b", "t2")]);
    expect(changedSince(before, after)).toEqual([]);
  });

  it("says nothing about a ticket that left, so moving to done flashes nobody", () => {
    const before = fetchStamps([at("a", "t1"), at("b", "t1")]);
    // `b` was marked done: the graph drops it, and its bumped updatedAt never
    // reaches this function, because the page only stamps the cards it draws.
    const after = fetchStamps([at("a", "t1")]);
    expect(changedSince(before, after)).toEqual([]);
  });

  it("does not glow the tickets that stayed when another one left", () => {
    const before = fetchStamps([at("a", "t1"), at("b", "t1"), at("c", "t1")]);
    const after = fetchStamps([at("a", "t1"), at("c", "t2")]);
    expect(changedSince(before, after)).toEqual(["c"]);
  });

  it("keeps the order of the new fetch, so the result is stable", () => {
    const before = fetchStamps([at("a", "t1")]);
    const after = fetchStamps([at("c", "t1"), at("a", "t2"), at("b", "t1")]);
    expect(changedSince(before, after)).toEqual(["c", "a", "b"]);
  });
});
