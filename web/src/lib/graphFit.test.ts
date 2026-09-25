import { describe, expect, it } from "vitest";
import { stepFit, type FitEvent, type FitMoment } from "./graphFit";

const ready: FitMoment = { visible: true, hasGraph: true, measured: true, hasViewport: true };
const rendered = (moment: Partial<FitMoment> = {}): FitEvent => ({ type: "rendered", moment: { ...ready, ...moment } });

/** Runs events from `pending`, answering which of them fitted the graph. */
function run(pending: boolean, events: FitEvent[]): { pending: boolean; fits: number[] } {
  const fits: number[] = [];
  events.forEach((event, i) => {
    const step = stepFit(pending, event);
    if (step.fit) fits.push(i);
    pending = step.pending;
  });
  return { pending, fits };
}

describe("stepFit", () => {
  it("fits a pending graph once it can frame it, and only once", () => {
    expect(stepFit(true, rendered())).toEqual({ pending: false, fit: true });
    expect(stepFit(false, rendered())).toEqual({ pending: false, fit: false });
  });

  it.each([
    ["the document is hidden", { visible: false }],
    ["there are no cards", { hasGraph: false }],
    ["a card is not measured yet", { measured: false }],
    ["the viewport has no size yet", { hasViewport: false }],
  ] as const)("keeps waiting while %s", (_what, moment) => {
    expect(stepFit(true, rendered(moment))).toEqual({ pending: true, fit: false });
  });

  it("fits a graph loaded in a background tab when the tab is shown, not on a live refresh before", () => {
    const { pending, fits } = run(true, [
      // Loaded hidden: the tickets arrive, but the page is never measured.
      rendered({ visible: false, measured: false, hasViewport: false }),
      // A live refresh re-renders it, still hidden; even measured, it waits.
      rendered({ visible: false }),
      // The tab is shown.
      rendered(),
      // A live refresh after that.
      rendered(),
    ]);
    expect(fits).toEqual([2]);
    expect(pending).toBe(false);
  });

  it("never fits after the user has panned or zoomed", () => {
    const { pending, fits } = run(true, [
      rendered({ measured: false }),
      { type: "userMoved" },
      rendered(),
      rendered(),
    ]);
    expect(fits).toEqual([]);
    expect(pending).toBe(false);
  });

  it("fits again when the user asks for a different graph, after having moved it", () => {
    const { fits } = run(false, [{ type: "userMoved" }, { type: "requested" }, rendered({ measured: false }), rendered()]);
    expect(fits).toEqual([3]);
  });

  it("lets the user's move settle a request made since", () => {
    expect(stepFit(false, { type: "requested" })).toEqual({ pending: true, fit: false });
    expect(stepFit(true, { type: "userMoved" })).toEqual({ pending: false, fit: false });
    expect(stepFit(false, { type: "userMoved" })).toEqual({ pending: false, fit: false });
  });
});
