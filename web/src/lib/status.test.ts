import { describe, expect, it } from "vitest";
import {
  STATUSES,
  STATUS_COLORS,
  STATUS_LABELS,
  STATUS_STYLES,
  isActive,
  isDone,
  isInProgress,
  isStatus,
} from "./status";

describe("the status set", () => {
  it("is in board column order, with agent_review between in_progress and done", () => {
    expect(STATUSES).toEqual(["todo", "in_progress", "agent_review", "done"]);
  });

  it("labels, colours and styles every status", () => {
    for (const status of STATUSES) {
      expect(STATUS_LABELS[status]).toBeTruthy();
      expect(STATUS_COLORS[status]).toBeTruthy();
      expect(STATUS_STYLES[status]).toBeTruthy();
    }
    expect(STATUS_LABELS.agent_review).toBe("Agent Review");
  });

  it("gives agent_review violet where in_progress is blue", () => {
    expect(STATUS_COLORS.agent_review).toBe("bg-violet-500");
    expect(STATUS_STYLES.agent_review).toBe("bg-violet-500/20 text-violet-400");
  });

  it("recognises agent_review as a known status", () => {
    expect(isStatus("agent_review")).toBe(true);
    expect(isStatus("needs_input")).toBe(false);
  });
});

describe("isActive", () => {
  it("covers both statuses an agent holds", () => {
    expect(isActive("in_progress")).toBe(true);
    expect(isActive("agent_review")).toBe(true);
  });

  it("covers nothing else", () => {
    expect(isActive("todo")).toBe(false);
    expect(isActive("done")).toBe(false);
    expect(isActive("needs_input")).toBe(false);
    expect(isActive("")).toBe(false);
  });

  it("does not blur in_progress and done with agent_review", () => {
    expect(isInProgress("agent_review")).toBe(false);
    expect(isDone("agent_review")).toBe(false);
  });
});
