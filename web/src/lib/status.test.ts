import { describe, expect, it } from "vitest";
import {
  KNOWN_STATUSES,
  STATUSES,
  STATUS_COLORS,
  STATUS_LABELS,
  STATUS_STYLES,
  isActive,
  isDone,
  isHeld,
  isInProgress,
  isStatus,
  isWaiting,
  isWritableStatus,
} from "./status";

describe("the status set", () => {
  it("offers the writable statuses in board column order, with agent_review between in_progress and done", () => {
    expect(STATUSES).toEqual(["todo", "in_progress", "agent_review", "done"]);
  });

  it("knows needs_user_input too, between in_progress and agent_review, as the server's board orders it", () => {
    expect(KNOWN_STATUSES).toEqual(["todo", "in_progress", "needs_user_input", "agent_review", "done"]);
  });

  it("labels, colours and styles every known status", () => {
    for (const status of KNOWN_STATUSES) {
      expect(STATUS_LABELS[status]).toBeTruthy();
      expect(STATUS_COLORS[status]).toBeTruthy();
      expect(STATUS_STYLES[status]).toBeTruthy();
    }
    expect(STATUS_LABELS.agent_review).toBe("Agent Review");
    expect(STATUS_LABELS.needs_user_input).toBe("Waiting on You");
  });

  it("gives agent_review violet where in_progress is blue", () => {
    expect(STATUS_COLORS.agent_review).toBe("bg-violet-500");
    expect(STATUS_STYLES.agent_review).toBe("bg-violet-500/20 text-violet-400");
  });

  it("gives needs_user_input red, and no status yellow", () => {
    expect(STATUS_COLORS.needs_user_input).toBe("bg-red-500");
    expect(STATUS_STYLES.needs_user_input).toBe("bg-red-500/20 text-red-400");
    for (const status of KNOWN_STATUSES) {
      expect(STATUS_COLORS[status]).not.toMatch(/yellow|amber/);
      expect(STATUS_STYLES[status]).not.toMatch(/yellow|amber/);
    }
  });

  it("recognises agent_review and needs_user_input as known statuses", () => {
    expect(isStatus("agent_review")).toBe(true);
    expect(isStatus("needs_user_input")).toBe(true);
    expect(isStatus("needs_input")).toBe(false);
  });

  it("offers needs_user_input to no write: only a request puts a ticket there", () => {
    expect(isWritableStatus("needs_user_input")).toBe(false);
    for (const status of STATUSES) expect(isWritableStatus(status)).toBe(true);
    expect(isWritableStatus("needs_input")).toBe(false);
  });
});

describe("isActive", () => {
  it("covers both statuses an agent is at work in", () => {
    expect(isActive("in_progress")).toBe(true);
    expect(isActive("agent_review")).toBe(true);
  });

  it("covers nothing else, waiting on the person included", () => {
    expect(isActive("todo")).toBe(false);
    expect(isActive("done")).toBe(false);
    expect(isActive("needs_user_input")).toBe(false);
    expect(isActive("needs_input")).toBe(false);
    expect(isActive("")).toBe(false);
  });

  it("does not blur in_progress and done with agent_review", () => {
    expect(isInProgress("agent_review")).toBe(false);
    expect(isDone("agent_review")).toBe(false);
  });
});

describe("isWaiting and isHeld", () => {
  it("tells a ticket waiting on the person from the rest", () => {
    expect(isWaiting("needs_user_input")).toBe(true);
    for (const status of STATUSES) expect(isWaiting(status)).toBe(false);
  });

  it("holds the active statuses and waiting on the person, since the agent keeps the ticket while it waits", () => {
    expect(isHeld("in_progress")).toBe(true);
    expect(isHeld("agent_review")).toBe(true);
    expect(isHeld("needs_user_input")).toBe(true);
    expect(isHeld("todo")).toBe(false);
    expect(isHeld("done")).toBe(false);
  });
});
