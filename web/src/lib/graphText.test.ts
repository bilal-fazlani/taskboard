import { describe, expect, it } from "vitest";
import { columnHeading, hiddenBlockersText, satisfiedDependenciesText } from "./graphText";

describe("columnHeading", () => {
  it("calls column 0 Ready and counts steps after it", () => {
    expect(columnHeading(0)).toBe("Ready");
    expect(columnHeading(1)).toBe("Blocked · 1 step");
    expect(columnHeading(2)).toBe("Blocked · 2 steps");
    expect(columnHeading(12)).toBe("Blocked · 12 steps");
  });
});

describe("satisfiedDependenciesText", () => {
  it("is absent without done dependencies and pluralises otherwise", () => {
    expect(satisfiedDependenciesText(0)).toBeNull();
    expect(satisfiedDependenciesText(1)).toBe("1 dependency done");
    expect(satisfiedDependenciesText(3)).toBe("3 dependencies done");
  });
});

describe("hiddenBlockersText", () => {
  it("is absent without hidden blockers and pluralises otherwise", () => {
    expect(hiddenBlockersText(0)).toBeNull();
    expect(hiddenBlockersText(1)).toBe("1 hidden blocker");
    expect(hiddenBlockersText(2)).toBe("2 hidden blockers");
  });
});
