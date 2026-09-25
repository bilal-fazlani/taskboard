// @vitest-environment jsdom
import { describe, expect, it, vi } from "vitest";
import { pushEscape } from "./escapeStack";

const escape = () => {
  const event = new KeyboardEvent("keydown", { key: "Escape", bubbles: true, cancelable: true });
  document.body.dispatchEvent(event);
  return event;
};

describe("pushEscape", () => {
  it("calls only the topmost handler and marks the event handled", () => {
    const below = vi.fn();
    const top = vi.fn();
    const popBelow = pushEscape(below);
    const popTop = pushEscape(top);
    expect(escape().defaultPrevented).toBe(true);
    expect(top).toHaveBeenCalledTimes(1);
    expect(below).not.toHaveBeenCalled();

    popTop();
    escape();
    expect(below).toHaveBeenCalledTimes(1);
    popBelow();
    expect(escape().defaultPrevented).toBe(false);
  });
});
