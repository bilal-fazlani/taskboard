import { describe, expect, it } from "vitest";
import { columnHeading, doneCountText, hiddenBlockersText, satisfiedDependenciesText } from "./graphText";

describe("columnHeading", () => {
  it("calls column 0 Ready and counts steps after it", () => {
    expect(columnHeading(0)).toBe("Ready");
    expect(columnHeading(1)).toBe("Blocked · 1 step");
    expect(columnHeading(2)).toBe("Blocked · 2 steps");
    expect(columnHeading(12)).toBe("Blocked · 12 steps");
  });
});

describe("doneCountText", () => {
  it("shows shown of total while the block holds fewer than there are", () => {
    expect(doneCountText(50, 96)).toBe("50 of 96");
    expect(doneCountText(7, 20)).toBe("7 of 20");
  });

  it("shows just the total once the block holds them all", () => {
    expect(doneCountText(50, 50)).toBe("50");
    expect(doneCountText(42, 42)).toBe("42");
    expect(doneCountText(0, 0)).toBe("0");
  });
});

describe("satisfiedDependenciesText", () => {
  it("is absent without done dependencies", () => {
    expect(satisfiedDependenciesText(0, 0)).toBeNull();
    expect(satisfiedDependenciesText(0, 2)).toBeNull();
  });

  it("shows done against the total, agreeing the noun with the total", () => {
    expect(satisfiedDependenciesText(1, 2)).toBe("1 of 2 dependencies done");
    expect(satisfiedDependenciesText(3, 3)).toBe("3 of 3 dependencies done");
    expect(satisfiedDependenciesText(1, 1)).toBe("1 of 1 dependency done");
  });
});

describe("hiddenBlockersText", () => {
  it("is absent without hidden blockers and pluralises otherwise", () => {
    expect(hiddenBlockersText(0)).toBeNull();
    expect(hiddenBlockersText(1)).toBe("1 hidden blocker");
    expect(hiddenBlockersText(2)).toBe("2 hidden blockers");
  });
});
