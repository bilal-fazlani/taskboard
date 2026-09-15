import { describe, expect, it } from "vitest";
import {
  DOM_DELTA_LINE,
  DOM_DELTA_PAGE,
  DOM_DELTA_PIXEL,
  FIT_PADDING,
  IDENTITY,
  LINE_PIXELS,
  MAX_SCALE,
  MIN_SCALE,
  PAGE_PIXELS,
  ZOOM_LEVELS,
  canZoomIn,
  canZoomOut,
  clampScale,
  fitTransform,
  isDrag,
  nextZoomLevel,
  panBy,
  panIntoView,
  previousZoomLevel,
  wheelPan,
  wheelPixels,
  wheelZoomFactor,
  zoomAround,
  zoomIn,
  zoomOut,
  zoomPercent,
  zoomTo,
  type Point,
  type Transform,
} from "./viewport";

const toScreen = (t: Transform, g: Point): Point => ({ x: t.x + t.k * g.x, y: t.y + t.k * g.y });
const toGraph = (t: Transform, s: Point): Point => ({ x: (s.x - t.x) / t.k, y: (s.y - t.y) / t.k });

describe("clampScale", () => {
  it("keeps scales between 25% and 200%", () => {
    expect(MIN_SCALE).toBe(0.25);
    expect(MAX_SCALE).toBe(2);
    expect(clampScale(0.1)).toBe(0.25);
    expect(clampScale(0.25)).toBe(0.25);
    expect(clampScale(0.8)).toBe(0.8);
    expect(clampScale(2)).toBe(2);
    expect(clampScale(5)).toBe(2);
  });
});

describe("panBy", () => {
  it("moves the translation and keeps the scale", () => {
    expect(panBy({ x: 10, y: 20, k: 1.5 }, -4, 6)).toEqual({ x: 6, y: 26, k: 1.5 });
  });
});

describe("zoomAround", () => {
  it("keeps the graph point under the screen point fixed", () => {
    const start: Transform = { x: 40, y: -30, k: 0.8 };
    const cursor = { x: 612, y: 347 };
    const under = toGraph(start, cursor);
    for (const factor of [1.3, 0.6, 2 ** 0.5, 1 / 1.1]) {
      const next = zoomAround(start, cursor, factor);
      expect(next.k).toBeCloseTo(0.8 * factor, 10);
      const after = toScreen(next, under);
      expect(after.x).toBeCloseTo(cursor.x, 9);
      expect(after.y).toBeCloseTo(cursor.y, 9);
    }
  });

  it("clamps the scale and still keeps the point fixed", () => {
    const start: Transform = { x: 100, y: 50, k: 1.8 };
    const cursor = { x: 300, y: 200 };
    const under = toGraph(start, cursor);
    const big = zoomAround(start, cursor, 10);
    expect(big.k).toBe(MAX_SCALE);
    expect(toScreen(big, under).x).toBeCloseTo(cursor.x, 9);
    expect(toScreen(big, under).y).toBeCloseTo(cursor.y, 9);
    const small = zoomAround(start, cursor, 0.001);
    expect(small.k).toBe(MIN_SCALE);
    expect(toScreen(small, under).x).toBeCloseTo(cursor.x, 9);
    expect(toScreen(small, under).y).toBeCloseTo(cursor.y, 9);
  });

  it("is a no-op when already at the limit", () => {
    const atMax: Transform = { x: 12, y: 34, k: MAX_SCALE };
    expect(zoomAround(atMax, { x: 500, y: 500 }, 1.5)).toEqual(atMax);
  });

  it("zoomTo sets an absolute scale around the point", () => {
    const next = zoomTo(IDENTITY, { x: 100, y: 100 }, 0.5);
    expect(next).toEqual({ x: 50, y: 50, k: 0.5 });
  });
});

describe("zoom steps", () => {
  it("steps through fixed levels from 25% to 200%", () => {
    expect(ZOOM_LEVELS[0]).toBe(MIN_SCALE);
    expect(ZOOM_LEVELS[ZOOM_LEVELS.length - 1]).toBe(MAX_SCALE);
    expect([...ZOOM_LEVELS].sort((a, b) => a - b)).toEqual(ZOOM_LEVELS);
    expect(nextZoomLevel(1)).toBe(1.1);
    expect(previousZoomLevel(1)).toBe(0.9);
    expect(nextZoomLevel(0.25)).toBe(0.33);
    expect(previousZoomLevel(2)).toBe(1.75);
  });

  it("snaps an in-between scale, as fit leaves, to the neighbouring level", () => {
    expect(nextZoomLevel(0.6)).toBe(0.67);
    expect(previousZoomLevel(0.6)).toBe(0.5);
    expect(nextZoomLevel(1.3)).toBe(1.5);
    expect(previousZoomLevel(1.3)).toBe(1.25);
  });

  it("tolerates rounding at a level and stops at the limits", () => {
    // A hair either side of a level counts as that level, so a step from a
    // scale that arithmetic left a little off a level still moves.
    expect(nextZoomLevel(0.5 + 1e-9)).toBe(0.67);
    expect(nextZoomLevel(0.5 - 1e-9)).toBe(0.67);
    expect(previousZoomLevel(0.5 - 1e-9)).toBe(0.33);
    expect(previousZoomLevel(0.5 + 1e-9)).toBe(0.33);
    expect(canZoomIn({ x: 0, y: 0, k: MAX_SCALE - 1e-9 })).toBe(false);
    expect(canZoomOut({ x: 0, y: 0, k: MIN_SCALE + 1e-9 })).toBe(false);
    expect(canZoomIn({ x: 0, y: 0, k: MAX_SCALE - 0.01 })).toBe(true);
    expect(canZoomOut({ x: 0, y: 0, k: MIN_SCALE + 0.01 })).toBe(true);
    expect(nextZoomLevel(MAX_SCALE)).toBe(MAX_SCALE);
    expect(previousZoomLevel(MIN_SCALE)).toBe(MIN_SCALE);
  });

  it("zoomIn and zoomOut step around the given point", () => {
    const centre = { x: 400, y: 300 };
    const start: Transform = { x: -20, y: 10, k: 1 };
    const under = toGraph(start, centre);
    const inner = zoomIn(start, centre);
    expect(inner.k).toBe(1.1);
    expect(toScreen(inner, under).x).toBeCloseTo(centre.x, 9);
    const outer = zoomOut(start, centre);
    expect(outer.k).toBe(0.9);
    expect(toScreen(outer, under).y).toBeCloseTo(centre.y, 9);
  });

  it("walks the whole range in eleven steps each way", () => {
    let t: Transform = { x: 0, y: 0, k: MIN_SCALE };
    const up: number[] = [];
    while (canZoomIn(t)) {
      t = zoomIn(t, { x: 0, y: 0 });
      up.push(t.k);
    }
    expect(up).toEqual(ZOOM_LEVELS.slice(1));
    const down: number[] = [];
    while (canZoomOut(t)) {
      t = zoomOut(t, { x: 0, y: 0 });
      down.push(t.k);
    }
    expect(down).toEqual([...ZOOM_LEVELS].reverse().slice(1));
  });

  it("formats the scale as a whole percentage", () => {
    expect(zoomPercent(1)).toBe("100%");
    expect(zoomPercent(0.25)).toBe("25%");
    expect(zoomPercent(0.6666)).toBe("67%");
  });
});

describe("fitTransform", () => {
  const viewport = { width: 1000, height: 600 };

  it("scales the graph to the tighter axis and centres it inside the padding", () => {
    const t = fitTransform({ width: 1904, height: 552 }, viewport);
    // Width is the tighter axis: (1000 - 48) / 1904 = 0.5.
    expect(t.k).toBeCloseTo(0.5, 10);
    expect(t.x).toBeCloseTo(FIT_PADDING, 10);
    // 552 * 0.5 = 276 tall, centred in 552 of room below the top padding.
    expect(t.y).toBeCloseTo(FIT_PADDING + (552 - 276) / 2, 10);
    // The whole graph is on screen, inside the padding.
    const bottomRight = toScreen(t, { x: 1904, y: 552 });
    expect(bottomRight.x).toBeLessThanOrEqual(viewport.width - FIT_PADDING + 1e-9);
    expect(bottomRight.y).toBeLessThanOrEqual(viewport.height - FIT_PADDING + 1e-9);
  });

  it("does not zoom a tiny graph past 200%", () => {
    const t = fitTransform({ width: 100, height: 50 }, viewport);
    expect(t.k).toBe(MAX_SCALE);
    expect(t.x).toBeCloseTo((1000 - 200) / 2, 10);
    expect(t.y).toBeCloseTo((600 - 100) / 2, 10);
  });

  it("clamps a huge graph at 25% and starts it at the padding on overflowing axes", () => {
    const t = fitTransform({ width: 20000, height: 400 }, viewport);
    expect(t.k).toBe(MIN_SCALE);
    // 20000 * 0.25 overflows the width: the left edge (Ready) stays in view.
    expect(t.x).toBe(FIT_PADDING);
    // 400 * 0.25 = 100 fits the height and is centred.
    expect(t.y).toBeCloseTo(FIT_PADDING + (552 - 100) / 2, 10);
    const tall = fitTransform({ width: 300, height: 30000 }, viewport);
    expect(tall.k).toBe(MIN_SCALE);
    expect(tall.y).toBe(FIT_PADDING);
    expect(tall.x).toBeCloseTo(FIT_PADDING + (952 - 75) / 2, 10);
  });

  it("fits inside per-side insets, centring in the room between them", () => {
    // 58px kept clear at the bottom, e.g. for a toolbar.
    const insets = { top: 24, right: 24, bottom: 58, left: 24 };
    const t = fitTransform({ width: 400, height: 1036 }, viewport, insets);
    // Height is the tighter axis: (600 - 24 - 58) / 1036 = 0.5.
    expect(t.k).toBeCloseTo(0.5, 10);
    expect(t.y).toBeCloseTo(24, 10);
    expect(toScreen(t, { x: 400, y: 1036 }).y).toBeCloseTo(600 - 58, 9);
    expect(t.x).toBeCloseTo(24 + (952 - 200) / 2, 10);
    // An overflowing axis starts at its own inset.
    const wide = fitTransform({ width: 20000, height: 100 }, viewport, { top: 10, right: 20, bottom: 30, left: 40 });
    expect(wide.k).toBe(MIN_SCALE);
    expect(wide.x).toBe(40);
    expect(wide.y).toBeCloseTo(10 + (560 - 25) / 2, 10);
    // A number is the same inset on every side.
    expect(fitTransform({ width: 700, height: 300 }, viewport, 30)).toEqual(
      fitTransform({ width: 700, height: 300 }, viewport, { top: 30, right: 30, bottom: 30, left: 30 }),
    );
  });

  it("takes a custom padding and survives degenerate sizes", () => {
    const t = fitTransform({ width: 500, height: 500 }, { width: 600, height: 600 }, 50);
    expect(t).toEqual({ x: 50, y: 50, k: 1 });
    expect(fitTransform({ width: 0, height: 0 }, viewport)).toEqual(IDENTITY);
    expect(fitTransform({ width: 100, height: 100 }, { width: 0, height: 0 }).k).toBe(MIN_SCALE);
  });
});

describe("wheel input", () => {
  it("converts line and page deltas to pixels", () => {
    expect(wheelPixels(120, DOM_DELTA_PIXEL)).toBe(120);
    expect(wheelPixels(3, DOM_DELTA_LINE)).toBe(3 * LINE_PIXELS);
    expect(wheelPixels(-1, DOM_DELTA_PAGE)).toBe(-PAGE_PIXELS);
  });

  it("zooms out on a positive deltaY and in on a negative one", () => {
    expect(wheelZoomFactor(10, DOM_DELTA_PIXEL)).toBeLessThan(1);
    expect(wheelZoomFactor(-10, DOM_DELTA_PIXEL)).toBeGreaterThan(1);
    expect(wheelZoomFactor(0, DOM_DELTA_PIXEL)).toBe(1);
    // Opposite deltas undo each other.
    expect(wheelZoomFactor(7, DOM_DELTA_PIXEL) * wheelZoomFactor(-7, DOM_DELTA_PIXEL)).toBeCloseTo(1, 12);
  });

  it("treats a trackpad pinch's small pixel deltas as small steps", () => {
    expect(wheelZoomFactor(-4, DOM_DELTA_PIXEL)).toBeCloseTo(2 ** 0.04, 12);
  });

  it("caps one mouse notch, in pixels or lines, at a factor of 2^0.5", () => {
    expect(wheelZoomFactor(-100, DOM_DELTA_PIXEL)).toBeCloseTo(2 ** 0.5, 12);
    expect(wheelZoomFactor(100, DOM_DELTA_PIXEL)).toBeCloseTo(2 ** -0.5, 12);
    // Firefox reports a notch as 3 lines, 48px: just under the cap.
    expect(wheelZoomFactor(-3, DOM_DELTA_LINE)).toBeCloseTo(2 ** 0.48, 12);
    expect(wheelZoomFactor(-5, DOM_DELTA_LINE)).toBeCloseTo(2 ** 0.5, 12);
    expect(wheelZoomFactor(1, DOM_DELTA_PAGE)).toBeCloseTo(2 ** -0.5, 12);
  });

  it("pans against the scroll direction, in pixels", () => {
    expect(wheelPan({ deltaX: 0, deltaY: 40, deltaMode: DOM_DELTA_PIXEL, shiftKey: false })).toEqual({ x: -0, y: -40 });
    expect(wheelPan({ deltaX: -25, deltaY: 10, deltaMode: DOM_DELTA_PIXEL, shiftKey: false })).toEqual({ x: 25, y: -10 });
    expect(wheelPan({ deltaX: 0, deltaY: 3, deltaMode: DOM_DELTA_LINE, shiftKey: false })).toEqual({ x: -0, y: -48 });
  });

  it("pans horizontally on shift+wheel whether or not the browser swapped the axes", () => {
    // Not swapped: the vertical delta becomes horizontal.
    expect(wheelPan({ deltaX: 0, deltaY: 100, deltaMode: DOM_DELTA_PIXEL, shiftKey: true })).toEqual({ x: -100, y: -0 });
    expect(wheelPan({ deltaX: 0, deltaY: -3, deltaMode: DOM_DELTA_LINE, shiftKey: true })).toEqual({ x: 48, y: -0 });
    // Already swapped: left as it is.
    expect(wheelPan({ deltaX: 100, deltaY: 0, deltaMode: DOM_DELTA_PIXEL, shiftKey: true })).toEqual({ x: -100, y: -0 });
  });
});

describe("panIntoView", () => {
  const viewport = { left: 100, top: 50, right: 1100, bottom: 650 };

  it("leaves a target that is already inside the margin alone", () => {
    expect(panIntoView({ left: 300, top: 200, right: 556, bottom: 300 }, viewport)).toEqual({ x: 0, y: 0 });
  });

  it("brings a target in from each side to the margin", () => {
    expect(panIntoView({ left: -400, top: 200, right: -144, bottom: 300 }, viewport)).toEqual({ x: 524, y: 0 });
    expect(panIntoView({ left: 1200, top: 200, right: 1456, bottom: 300 }, viewport)).toEqual({ x: -380, y: 0 });
    expect(panIntoView({ left: 300, top: -90, right: 556, bottom: 10 }, viewport)).toEqual({ x: 0, y: 164 });
    expect(panIntoView({ left: 300, top: 700, right: 556, bottom: 800 }, viewport)).toEqual({ x: 0, y: -174 });
  });

  it("lines up the top left of a target bigger than the viewport", () => {
    expect(panIntoView({ left: 500, top: 50, right: 1700, bottom: 150 }, viewport, 24)).toEqual({ x: -376, y: 24 });
  });
});

describe("isDrag", () => {
  it("is a drag once the pointer is more than 4px from where it went down", () => {
    expect(isDrag({ x: 10, y: 10 }, { x: 10, y: 10 })).toBe(false);
    expect(isDrag({ x: 10, y: 10 }, { x: 12, y: 12 })).toBe(false);
    // Exactly 4px is still a click.
    expect(isDrag({ x: 10, y: 10 }, { x: 14, y: 10 })).toBe(false);
    expect(isDrag({ x: 10, y: 10 }, { x: 10, y: 6 })).toBe(false);
    expect(isDrag({ x: 10, y: 10 }, { x: 14.01, y: 10 })).toBe(true);
    expect(isDrag({ x: 10, y: 10 }, { x: 13, y: 13 })).toBe(true);
  });
});
