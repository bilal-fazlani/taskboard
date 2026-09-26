import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { formatDueDate } from "./dueDate";

describe("formatDueDate", () => {
  it("formats the stored calendar date", () => {
    expect(formatDueDate("2026-10-01T00:00:00Z")).toBe("10/1/2026");
    expect(formatDueDate("2026-10-01")).toBe("10/1/2026");
  });

  it("returns an empty string with no due date", () => {
    expect(formatDueDate(undefined)).toBe("");
    expect(formatDueDate("")).toBe("");
  });
});

// ACP-53: anyone west of UTC saw the previous day, because
// `new Date(ticket.dueDate).toLocaleDateString()` converts through the
// viewer's local timezone before formatting. This runs under a negative UTC
// offset (America/Los_Angeles, UTC-7 in September) to prove the day no
// longer shifts. Reverting dueDate.ts's body to
// `new Date(dueDate).toLocaleDateString()` makes this fail: it prints
// "9/30/2026" instead.
describe("formatDueDate under a negative-offset timezone", () => {
  const originalTz = process.env.TZ;

  beforeEach(() => {
    process.env.TZ = "America/Los_Angeles";
  });

  afterEach(() => {
    if (originalTz === undefined) {
      delete process.env.TZ;
    } else {
      process.env.TZ = originalTz;
    }
  });

  it("keeps the stored calendar date instead of shifting a day earlier", () => {
    expect(formatDueDate("2026-10-01T00:00:00Z")).toBe("10/1/2026");
  });
});
