/**
 * Pan and zoom arithmetic for the graph page, with no React and no DOM.
 *
 * A transform maps a point on the graph's canvas to a point in the viewport:
 * screen = (x, y) + k * graph, which is what the CSS
 * `translate(x px, y px) scale(k)` does with its origin at the top left.
 */

export interface Transform {
  x: number;
  y: number;
  k: number;
}

export interface Point {
  x: number;
  y: number;
}

export interface Extent {
  width: number;
  height: number;
}

/** Room to leave on each side of the viewport. */
export interface Insets {
  top: number;
  right: number;
  bottom: number;
  left: number;
}

/** A rectangle in viewport coordinates, as getBoundingClientRect gives. */
export interface Rect {
  left: number;
  top: number;
  right: number;
  bottom: number;
}

export const MIN_SCALE = 0.25;
export const MAX_SCALE = 2;
export const IDENTITY: Transform = { x: 0, y: 0, k: 1 };

/** The scales the toolbar's zoom buttons step through. */
export const ZOOM_LEVELS: readonly number[] = [0.25, 0.33, 0.5, 0.67, 0.75, 0.9, 1, 1.1, 1.25, 1.5, 1.75, 2];

/** Room fit leaves between the graph and the viewport's edges. */
export const FIT_PADDING = 24;

// WheelEvent.deltaMode values.
export const DOM_DELTA_PIXEL = 0;
export const DOM_DELTA_LINE = 1;
export const DOM_DELTA_PAGE = 2;
export const LINE_PIXELS = 16;
export const PAGE_PIXELS = 800;

/**
 * How fast wheel zoom goes: the scale doubles every this many pixels of
 * delta. One event's delta is capped at WHEEL_ZOOM_MAX_PIXELS, so a mouse
 * notch (100px in Chrome, 3 lines in Firefox) zooms by at most 2^0.5, while a
 * trackpad pinch, which sends many small deltas, zooms smoothly.
 */
export const WHEEL_ZOOM_PIXELS_PER_DOUBLING = 100;
export const WHEEL_ZOOM_MAX_PIXELS = 50;

// Tolerates rounding in scales that came from arithmetic, e.g. 0.5 * 1.1 / 1.1.
const EPSILON = 1e-6;

export function clampScale(k: number): number {
  return Math.min(MAX_SCALE, Math.max(MIN_SCALE, k));
}

/** Moves the graph by (dx, dy) screen pixels. */
export function panBy(t: Transform, dx: number, dy: number): Transform {
  return { x: t.x + dx, y: t.y + dy, k: t.k };
}

/**
 * Sets the scale to `k`, clamped, keeping the graph point under the screen
 * point `at` where it is.
 */
export function zoomTo(t: Transform, at: Point, k: number): Transform {
  const next = clampScale(k);
  const gx = (at.x - t.x) / t.k;
  const gy = (at.y - t.y) / t.k;
  return { x: at.x - gx * next, y: at.y - gy * next, k: next };
}

/** Multiplies the scale by `factor` around the screen point `at`. */
export function zoomAround(t: Transform, at: Point, factor: number): Transform {
  return zoomTo(t, at, t.k * factor);
}

/** The next zoom level above `k`, or MAX_SCALE when there is none. */
export function nextZoomLevel(k: number): number {
  return ZOOM_LEVELS.find((level) => level > k + EPSILON) ?? MAX_SCALE;
}

/** The next zoom level below `k`, or MIN_SCALE when there is none. */
export function previousZoomLevel(k: number): number {
  for (let i = ZOOM_LEVELS.length - 1; i >= 0; i--) {
    if (ZOOM_LEVELS[i] < k - EPSILON) return ZOOM_LEVELS[i];
  }
  return MIN_SCALE;
}

export function zoomIn(t: Transform, at: Point): Transform {
  return zoomTo(t, at, nextZoomLevel(t.k));
}

export function zoomOut(t: Transform, at: Point): Transform {
  return zoomTo(t, at, previousZoomLevel(t.k));
}

export function canZoomIn(t: Transform): boolean {
  return t.k < MAX_SCALE - EPSILON;
}

export function canZoomOut(t: Transform): boolean {
  return t.k > MIN_SCALE + EPSILON;
}

/** The scale as the toolbar shows it, e.g. "67%". */
export function zoomPercent(k: number): string {
  return `${Math.round(k * 100)}%`;
}

/**
 * The transform that shows the whole graph in the viewport, leaving `padding`
 * free on each side (one number for all four, or per side), at a scale
 * clamped to [MIN_SCALE, MAX_SCALE]. On an axis where the graph fits it is
 * centred in the room between the paddings. On an axis where it doesn't,
 * because the scale stopped at MIN_SCALE, it starts at the padding instead,
 * so the column headers and the Ready column stay in view rather than the
 * middle of the graph.
 */
export function fitTransform(graph: Extent, viewport: Extent, padding: number | Insets = FIT_PADDING): Transform {
  if (graph.width <= 0 || graph.height <= 0) return IDENTITY;
  const inset = typeof padding === "number" ? { top: padding, right: padding, bottom: padding, left: padding } : padding;
  const room = {
    width: Math.max(1, viewport.width - inset.left - inset.right),
    height: Math.max(1, viewport.height - inset.top - inset.bottom),
  };
  const k = clampScale(Math.min(room.width / graph.width, room.height / graph.height));
  const place = (size: number, available: number, start: number) =>
    size * k <= available + EPSILON ? start + (available - size * k) / 2 : start;
  return { x: place(graph.width, room.width, inset.left), y: place(graph.height, room.height, inset.top), k };
}

/** A wheel delta in pixels, whatever unit the event reported it in. */
export function wheelPixels(delta: number, deltaMode: number): number {
  if (deltaMode === DOM_DELTA_LINE) return delta * LINE_PIXELS;
  if (deltaMode === DOM_DELTA_PAGE) return delta * PAGE_PIXELS;
  return delta;
}

/**
 * The zoom factor for a ctrl or meta wheel event, which is also how Chrome
 * and Firefox on macOS report a trackpad pinch. Scrolling down (a positive
 * deltaY) zooms out.
 */
export function wheelZoomFactor(deltaY: number, deltaMode: number): number {
  const pixels = wheelPixels(deltaY, deltaMode);
  const capped = Math.max(-WHEEL_ZOOM_MAX_PIXELS, Math.min(WHEEL_ZOOM_MAX_PIXELS, pixels));
  return 2 ** (-capped / WHEEL_ZOOM_PIXELS_PER_DOUBLING);
}

export interface WheelLike {
  deltaX: number;
  deltaY: number;
  deltaMode: number;
  shiftKey: boolean;
}

/**
 * How far a plain wheel event moves the graph, in screen pixels. Scrolling
 * down or right moves the graph up or left, as scrolling a page would. With
 * shift held, a mouse wheel that still reports a vertical delta (the browser
 * didn't swap it) pans horizontally.
 */
export function wheelPan(e: WheelLike): Point {
  let dx = wheelPixels(e.deltaX, e.deltaMode);
  let dy = wheelPixels(e.deltaY, e.deltaMode);
  if (e.shiftKey && dx === 0) {
    dx = dy;
    dy = 0;
  }
  return { x: -dx, y: -dy };
}

/**
 * How far to pan so that `target` sits inside `viewport` with `margin` to
 * spare, or (0, 0) when it already does. A target bigger than the viewport
 * lines up its top left.
 */
export function panIntoView(target: Rect, viewport: Rect, margin = FIT_PADDING): Point {
  const axis = (start: number, end: number, min: number, max: number) => {
    if (start < min + margin) return min + margin - start;
    if (end > max - margin) return Math.max(max - margin - end, min + margin - start);
    return 0;
  };
  return {
    x: axis(target.left, target.right, viewport.left, viewport.right),
    y: axis(target.top, target.bottom, viewport.top, viewport.bottom),
  };
}

/**
 * A pointer is a drag once it has moved more than DRAG_THRESHOLD pixels from
 * where it went down; up to that, it is a click.
 */
export const DRAG_THRESHOLD = 4;

export function isDrag(from: Point, to: Point, threshold = DRAG_THRESHOLD): boolean {
  return Math.hypot(to.x - from.x, to.y - from.y) > threshold;
}

