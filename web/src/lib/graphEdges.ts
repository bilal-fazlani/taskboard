// SVG path data for the arrows on the graph page. positionGraph() gives every
// edge a start on the blocker's right edge and an end on the blocked card's
// left edge, spread along the side when several edges share it, and a
// forward edge that spans several columns the points where it crosses each
// column in between; this module turns those points into a drawn shape.
// Three shapes, picked by the columns the two cards sit in rather than by the
// back flag, so a shape always suits where its ends actually are:
//
// - Left to right (every edge that isn't a back edge): a cubic bezier across
//   each gap between columns, leaving and entering horizontally with both
//   control points half way across, and a straight run across each column
//   in between, at the height the layout kept clear for it. Gaps hold no
//   cards, so no part of it passes behind one. Where a gap holds the vertical
//   runs of right-to-left edges, the curve keeps to the room between the two
//   groups of runs and reaches it by straight horizontal leads, so it only
//   ever crosses a run at a right angle, never alongside it where the curve
//   is steep.
// - Right to left (a back edge between two cards, which always points to an
//   earlier column): anything drawn across the columns would cut through the
//   cards in between. It runs in straight lines with small rounded corners
//   instead: right from the blocker into the gap after its column, up that
//   gap to a lane above every card, left along the lane to the gap before the
//   blocked card's column, down that gap and right into the blocked card. The
//   gaps hold no cards and the lanes run above the top of every column, so no
//   part of it crosses a card. Each such edge gets a lane of its own, so two
//   back edges never share a horizontal run, and a slot of its own on each
//   side of a gap it climbs or drops through, so they never share a vertical
//   one either. A side of a gap only numbers the edges that use it, nearest
//   lane nearest the column, and a gap that needs more slots than fit is
//   widened rather than reusing one; planGutters() says how wide.
// - Same card (a self-dependency): a small D-shaped loop on the card's right
//   edge that leaves just above the midpoint and returns just below it, both
//   ends horizontal, so the arrowhead points straight back into the card and
//   the loop stays in the gap next to the card.
//
// The page draws the arrowhead with an SVG marker, so every path ends exactly
// at the point the arrow should touch.

import type { GraphEdge, GraphLayout, GraphTicket, Point } from "./graphLayout";

/** Space between the top of the cards and the lowest back-edge lane. */
export const LANE_CLEARANCE = 12;
/** Vertical distance between neighbouring back-edge lanes. */
export const LANE_SPACING = 8;
/**
 * How far into a gap the nearest vertical run sits from the column edge. It
 * clears the grey arrowheads landing on that column's cards, which reach
 * ARROW_SIZE * EDGE_WIDTH into the gap, by more than half a run's stroke.
 */
export const GUTTER_OFFSET = 14;
/** Horizontal distance between neighbouring vertical runs on one side of a gap. */
export const GUTTER_STEP = 6;
/** Vertical runs per side that the narrowest gaps are sized from. */
export const BASE_SIDE_SLOTS = 6;
/**
 * Narrowest gap between two columns, sized from BASE_SIDE_SLOTS runs on each
 * side. A gap whose runs, and the room its curves keep between them, need
 * more is widened on its own; planGutters() says how wide.
 */
export const MIN_COLUMN_GAP = 2 * sideRoom(BASE_SIDE_SLOTS) + GUTTER_OFFSET;
/** Narrowest room left of Ready and right of the last column. */
export const BACK_EDGE_GUTTER = sideRoom(BASE_SIDE_SLOTS) + GUTTER_OFFSET / 2;
/** Stroke width of an edge that isn't lit. */
export const EDGE_WIDTH = 1.5;
/** Stroke width of an edge on a lit chain. */
export const LIT_EDGE_WIDTH = 2;
/** An arrowhead's length and width, in stroke widths, as the page's markers draw it. */
export const ARROW_SIZE = 7;
/** Room between a forward edge's curve and the farthest vertical run it passes. */
export const CURVE_CLEARANCE = GUTTER_STEP / 2;
/** Narrowest room a gap leaves its forward curves between the runs on either side. */
export const CURVE_ROOM = 32;
/** Radius of a right-to-left edge's corners. */
export const CORNER_RADIUS = 6;
/** How far a self-loop reaches past the card's right edge. */
export const SELF_LOOP_REACH = 24;
/** Half the vertical distance between where a self-loop leaves and returns. */
export const SELF_LOOP_SPREAD = 14;

// Control-point factor that makes a cubic quarter turn close to a circular one.
const QUARTER_TURN = 0.55;

export interface RoutedEdge extends GraphEdge {
  /** SVG path data, ending where the arrowhead goes. */
  d: string;
  /** The lane a right-to-left edge runs along, 0 nearest the cards; null for other shapes. */
  lane: number | null;
}

/** Distance from a column edge to the farthest of `slots` vertical runs beside it. */
export function sideRoom(slots: number): number {
  return slots === 0 ? 0 : GUTTER_OFFSET + (slots - 1) * GUTTER_STEP;
}

/**
 * How far from a column edge a forward curve starts or ends in a gap whose
 * side holds `slots` vertical runs: just past the farthest, or at the column
 * edge when there are none.
 */
export function curveLead(slots: number): number {
  return slots === 0 ? 0 : sideRoom(slots) + CURVE_CLEARANCE;
}

/** Distance from a column edge to vertical run `slot`, 0 being the nearest. */
export function slotOffset(slot: number): number {
  return GUTTER_OFFSET + slot * GUTTER_STEP;
}

function num(value: number): string {
  // Two decimals are well below a pixel and keep float noise out of the markup.
  return String(Math.round(value * 100) / 100);
}

function pt(x: number, y: number): string {
  return `${num(x)} ${num(y)}`;
}

/** Straight horizontal leads before and after a forward edge's curve across one gap. */
export interface Leads {
  /** Length of the straight run from the gap's left side before the curve starts. */
  left: number;
  /** Length of the straight run into the gap's right side after the curve ends. */
  right: number;
}

/**
 * Left to right: a horizontal-tangent cubic from start to end, or, with
 * `via` points in entry and exit pairs, a cubic to each entry and a straight
 * line across to its exit. `leads[k]`, for the k-th gap the edge crosses,
 * shortens that gap's curve by a straight run at either end.
 */
export function forwardEdgePath(
  start: Point,
  end: Point,
  via: readonly Point[] = [],
  leads: readonly (Leads | undefined)[] = [],
): string {
  const points = [start, ...via, end];
  let d = `M ${pt(start.x, start.y)}`;
  for (let i = 1; i < points.length; i++) {
    const [a, b] = [points[i - 1], points[i]];
    // Odd stretches cross a gap; even ones run across a column.
    if (i % 2 === 0) {
      d += ` L ${pt(b.x, b.y)}`;
      continue;
    }
    const lead = leads[(i - 1) / 2];
    const from = a.x + (lead?.left ?? 0);
    const to = b.x - (lead?.right ?? 0);
    if (from !== a.x) d += ` L ${pt(from, a.y)}`;
    const reach = (to - from) / 2;
    d += ` C ${pt(from + reach, a.y)}, ${pt(to - reach, b.y)}, ${pt(to, b.y)}`;
    if (to !== b.x) d += ` L ${pt(b.x, b.y)}`;
  }
  return d;
}

/**
 * Right to left: out to `outX`, up to `laneY`, left to `inX`, down to the
 * end's height and right into it, with rounded corners. `outX` must lie right
 * of the start and `inX` left of the end by more than CORNER_RADIUS, and
 * `laneY` above both ends by more than twice it.
 */
export function laneEdgePath(start: Point, end: Point, outX: number, inX: number, laneY: number): string {
  const r = CORNER_RADIUS;
  return [
    `M ${pt(start.x, start.y)}`,
    `L ${pt(outX - r, start.y)}`,
    `Q ${pt(outX, start.y)} ${pt(outX, start.y - r)}`,
    `L ${pt(outX, laneY + r)}`,
    `Q ${pt(outX, laneY)} ${pt(outX - r, laneY)}`,
    `L ${pt(inX + r, laneY)}`,
    `Q ${pt(inX, laneY)} ${pt(inX, laneY + r)}`,
    `L ${pt(inX, end.y - r)}`,
    `Q ${pt(inX, end.y)} ${pt(inX + r, end.y)}`,
    `L ${pt(end.x, end.y)}`,
  ].join(" ");
}

/**
 * Same card: a loop on the right edge around `start`, the right-edge midpoint.
 * It leaves rightwards, turns at SELF_LOOP_REACH past the edge and comes back
 * moving left, as two quarter-ellipse pairs joined at the rightmost point.
 */
export function selfLoopPath(start: Point): string {
  const top = start.y - SELF_LOOP_SPREAD;
  const bottom = start.y + SELF_LOOP_SPREAD;
  const far = start.x + SELF_LOOP_REACH;
  const near = start.x + SELF_LOOP_REACH * QUARTER_TURN;
  const bend = SELF_LOOP_SPREAD * (1 - QUARTER_TURN);
  return (
    `M ${pt(start.x, top)} ` +
    `C ${pt(near, top)}, ${pt(far, start.y - bend)}, ${pt(far, start.y)} ` +
    `C ${pt(far, start.y + bend)}, ${pt(near, bottom)}, ${pt(start.x, bottom)}`
  );
}

/** What lane and slot planning reads; a topology and a layout both fit. */
export interface Routable {
  nodes: readonly { id: string; column: number }[];
  edges: readonly GraphEdge[];
  columns: readonly unknown[];
}

/** What gutter planning reads: a topology fits, shelf and all. */
export interface GutterPlannable extends Routable {
  /** The Ready tickets on the shelf left of the linked Ready column; none if left out. */
  shelf?: readonly unknown[];
}

interface LanePlan {
  /** Lane by index into topology.edges, for right-to-left edges only. */
  lane: Map<number, number>;
  /** Slot on the right side of the blocker's column, counted from that column. */
  outSlot: Map<number, number>;
  /** Slot on the left side of the blocked card's column, counted from that column. */
  inSlot: Map<number, number>;
  /** Runs on the right side of each column. */
  outCount: number[];
  /** Runs on the left side of each column. */
  inCount: number[];
}

// Right-to-left edges get lanes in order of span, shortest first, so a short
// cycle runs nearest the cards and inside a longer one, then in topology
// order, which is deterministic. Each side of a gap then numbers only the
// edges that climb or drop through it, in lane order, so the nearest lane
// takes the slot nearest the column and its run never crosses the lane turns
// of the runs beyond it.
function planLanes(topology: Routable): LanePlan {
  const column = new Map(topology.nodes.map((n) => [n.id, n.column]));
  const order = topology.edges
    .map((edge, index) => ({ edge, index, span: column.get(edge.from)! - column.get(edge.to)! }))
    .filter(({ edge, span }) => edge.from !== edge.to && span >= 0)
    .sort((a, b) => a.span - b.span || a.index - b.index);
  const columns = topology.columns.length;
  const plan: LanePlan = {
    lane: new Map(),
    outSlot: new Map(),
    inSlot: new Map(),
    outCount: new Array<number>(columns).fill(0),
    inCount: new Array<number>(columns).fill(0),
  };
  order.forEach(({ edge, index }, lane) => {
    const from = column.get(edge.from)!;
    const to = column.get(edge.to)!;
    plan.lane.set(index, lane);
    plan.outSlot.set(index, plan.outCount[from]++);
    plan.inSlot.set(index, plan.inCount[to]++);
  });
  return plan;
}

/** How many lanes the graph's right-to-left edges need: one each. */
export function laneCount(topology: Routable): number {
  return planLanes(topology).lane.size;
}

/** Extra space to leave above the cards for `count` lanes. */
export function lanesHeight(count: number): number {
  return count * LANE_SPACING;
}

export interface GutterPlan {
  /** Width each gap between column i and i + 1 needs, at least MIN_COLUMN_GAP. */
  gaps: number[];
  /**
   * Room needed left of everything, at least BACK_EDGE_GUTTER: left of Ready,
   * or of the shelf when there is one.
   */
  left: number;
  /**
   * Width the gap between the shelf and the linked Ready column needs, at
   * least MIN_COLUMN_GAP. The runs into Ready come down in it, so it takes
   * the room they would need left of Ready without a shelf.
   */
  shelf: number;
  /** Room needed right of the last column, at least BACK_EDGE_GUTTER. */
  right: number;
}

/**
 * The horizontal room the vertical runs need, from the topology alone, so it
 * is known before the cards are positioned. A gap holds its left column's
 * outgoing runs and its right column's incoming ones with at least
 * CURVE_ROOM between the two groups, past CURVE_CLEARANCE on either side, for
 * the forward edges' curves. With a shelf, the runs into Ready come down
 * between the shelf and Ready rather than left of everything.
 */
export function planGutters(topology: GutterPlannable): GutterPlan {
  const { outCount, inCount } = planLanes(topology);
  const last = topology.columns.length - 1;
  const gaps: number[] = [];
  for (let c = 0; c < last; c++) {
    gaps.push(Math.max(MIN_COLUMN_GAP, curveLead(outCount[c]) + curveLead(inCount[c + 1]) + CURVE_ROOM));
  }
  const edgeRoom = (slots: number) => Math.max(BACK_EDGE_GUTTER, sideRoom(slots) + GUTTER_OFFSET / 2);
  const intoReady = last >= 0 ? inCount[0] : 0;
  const shelved = (topology.shelf?.length ?? 0) > 0;
  return {
    gaps,
    left: edgeRoom(shelved ? 0 : intoReady),
    shelf: Math.max(MIN_COLUMN_GAP, edgeRoom(intoReady)),
    right: edgeRoom(last >= 0 ? outCount[last] : 0),
  };
}

/**
 * The path of every edge in a laid-out graph, in the layout's edge order.
 * Lane n runs LANE_CLEARANCE + n * LANE_SPACING above the top of the cards
 * (or of a long edge's run, if one is higher).
 * Adding lanesHeight(laneCount(topology)) to the origin's y therefore keeps
 * the highest lane no more than LANE_CLEARANCE above where the cards would
 * start without lanes. Every gap and the room beside the outer columns must
 * be at least what planGutters(topology) asks for.
 */
export function routeEdges<T extends GraphTicket>(layout: GraphLayout<T>): RoutedEdge[] {
  const nodes = new Map(layout.nodes.map((n) => [n.id, n]));
  // A long edge's straight runs can sit above every card, so they count.
  let cardsTop = Infinity;
  for (const n of layout.nodes) cardsTop = Math.min(cardsTop, n.y);
  for (const e of layout.edges) for (const p of e.via) cardsTop = Math.min(cardsTop, p.y);
  const plan = planLanes(layout);
  const gapLeads = layout.columns.slice(1).map(
    (_, c): Leads => ({ left: curveLead(plan.outCount[c]), right: curveLead(plan.inCount[c + 1]) }),
  );

  return layout.edges.map((edge, index): RoutedEdge => {
    const base = { from: edge.from, to: edge.to, back: edge.back };
    if (edge.from === edge.to) return { ...base, d: selfLoopPath(edge.start), lane: null };
    const lane = plan.lane.get(index);
    if (lane === undefined) {
      const leads = gapLeads.slice(nodes.get(edge.from)!.column);
      return { ...base, d: forwardEdgePath(edge.start, edge.end, edge.via, leads), lane: null };
    }

    const fromColumn = layout.columns[nodes.get(edge.from)!.column];
    const toColumn = layout.columns[nodes.get(edge.to)!.column];
    const outX = fromColumn.x + fromColumn.width + slotOffset(plan.outSlot.get(index)!);
    const inX = toColumn.x - slotOffset(plan.inSlot.get(index)!);
    const laneY = cardsTop - LANE_CLEARANCE - lane * LANE_SPACING;
    return { ...base, d: laneEdgePath(edge.start, edge.end, outX, inX, laneY), lane };
  });
}
