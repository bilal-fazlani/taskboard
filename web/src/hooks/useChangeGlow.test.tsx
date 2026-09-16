// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { useChangeGlow } from "./useChangeGlow";
import { GLOW_MS, type GlowTicket } from "../lib/changeGlow";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let root: Root;
let container: HTMLDivElement;
let mounted = false;

/** Unmounts once, whether the test did it already or not. */
function unmount() {
  if (!mounted) return;
  mounted = false;
  act(() => root.unmount());
}

// The glowing ids are read back out of the DOM rather than off a variable,
// which is also how the page uses them: a class on the cards that glow.
function Probe({ tickets }: { tickets: readonly GlowTicket[] | null }) {
  const glowing = useChangeGlow(tickets);
  return <output>{[...glowing].join(" ")}</output>;
}

/** The ids glowing right now, in the order the hook lit them. */
function glowing(): string[] {
  const text = container.querySelector("output")?.textContent ?? "";
  return text.length === 0 ? [] : text.split(" ");
}

/** Renders the hook with one fetch's cards, as a refetch would. */
function fetched(tickets: readonly GlowTicket[] | null) {
  act(() => {
    root.render(<Probe tickets={tickets} />);
  });
}

const at = (id: string, updatedAt: string) => ({ id, updatedAt });

beforeEach(() => {
  vi.useFakeTimers();
  container = document.createElement("div");
  document.body.appendChild(container);
  root = createRoot(container);
  mounted = true;
});

afterEach(() => {
  unmount();
  container.remove();
  vi.clearAllTimers();
  vi.useRealTimers();
});

describe("useChangeGlow", () => {
  it("glows nothing before the first fetch has landed", () => {
    fetched(null);
    expect(glowing()).toEqual([]);
  });

  it("glows nothing on the first fetch", () => {
    fetched(null);
    fetched([at("a", "t1"), at("b", "t1")]);
    expect(glowing()).toEqual([]);
  });

  it("glows a card whose updatedAt moved, and stops after two seconds", () => {
    fetched(null);
    fetched([at("a", "t1"), at("b", "t1")]);
    fetched([at("a", "t2"), at("b", "t1")]);
    expect(glowing()).toEqual(["a"]);

    act(() => void vi.advanceTimersByTime(GLOW_MS - 1));
    expect(glowing()).toEqual(["a"]);

    act(() => void vi.advanceTimersByTime(1));
    expect(glowing()).toEqual([]);
  });

  it("glows a card that is new on the graph", () => {
    fetched(null);
    fetched([at("a", "t1")]);
    fetched([at("a", "t1"), at("b", "t1")]);
    expect(glowing()).toEqual(["b"]);
  });

  it("glows nobody when a card left for done", () => {
    fetched(null);
    fetched([at("a", "t1"), at("b", "t1")]);
    fetched([at("a", "t1")]);
    expect(glowing()).toEqual([]);
  });

  it("glows nothing when a refetch changed nothing", () => {
    fetched(null);
    fetched([at("a", "t1")]);
    fetched([at("a", "t1")]);
    expect(glowing()).toEqual([]);
  });

  it("times each card out on its own, so a later change does not cut an earlier glow short", () => {
    fetched(null);
    fetched([at("a", "t1"), at("b", "t1")]);
    fetched([at("a", "t2"), at("b", "t1")]);
    act(() => void vi.advanceTimersByTime(GLOW_MS / 2));
    fetched([at("a", "t2"), at("b", "t2")]);
    expect(glowing().sort()).toEqual(["a", "b"]);

    // `a` is two seconds old here, `b` only one.
    act(() => void vi.advanceTimersByTime(GLOW_MS / 2));
    expect(glowing()).toEqual(["b"]);

    act(() => void vi.advanceTimersByTime(GLOW_MS / 2));
    expect(glowing()).toEqual([]);
  });

  it("restarts the two seconds when the same card changes again", () => {
    fetched(null);
    fetched([at("a", "t1")]);
    fetched([at("a", "t2")]);
    act(() => void vi.advanceTimersByTime(GLOW_MS / 2));
    fetched([at("a", "t3")]);

    act(() => void vi.advanceTimersByTime(GLOW_MS / 2));
    expect(glowing()).toEqual(["a"]);

    act(() => void vi.advanceTimersByTime(GLOW_MS / 2));
    expect(glowing()).toEqual([]);
  });

  it("leaves no timer running after unmount", () => {
    fetched(null);
    fetched([at("a", "t1")]);
    fetched([at("a", "t2")]);
    unmount();
    expect(vi.getTimerCount()).toBe(0);
  });
});
