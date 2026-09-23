import { describe, expect, it } from "vitest";
import { nextEpicSort, sortByEpic } from "./epicSort";

const row = (key: string, epic?: string) => ({ key, epic: epic === undefined ? undefined : { name: epic } });
const ROWS = [row("A1", "views"), row("A2"), row("A3", "Agents"), row("A4", "Views"), row("A5"), row("A6", "realtime")];
const keys = (rows: { key: string }[]) => rows.map((r) => r.key);

describe("nextEpicSort", () => {
  it("steps through ascending, descending and back to none", () => {
    expect(nextEpicSort(null)).toBe("asc");
    expect(nextEpicSort("asc")).toBe("desc");
    expect(nextEpicSort("desc")).toBeNull();
  });
});

describe("sortByEpic", () => {
  it("keeps the table's order without a sort, as a new array", () => {
    const sorted = sortByEpic(ROWS, null);
    expect(keys(sorted)).toEqual(keys(ROWS));
    expect(sorted).not.toBe(ROWS);
  });

  it("sorts by name ignoring case, keeping each epic's tickets in order, with no epic last", () => {
    expect(keys(sortByEpic(ROWS, "asc"))).toEqual(["A3", "A6", "A1", "A4", "A2", "A5"]);
  });

  it("reverses the epics descending, still with no epic last", () => {
    expect(keys(sortByEpic(ROWS, "desc"))).toEqual(["A1", "A4", "A6", "A3", "A2", "A5"]);
  });

  it("leaves the input alone", () => {
    const before = keys(ROWS);
    sortByEpic(ROWS, "asc");
    expect(keys(ROWS)).toEqual(before);
  });
});
