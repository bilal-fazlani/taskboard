import { describe, expect, it } from "vitest";
import {
  ARROW_SIZE,
  BACK_EDGE_GUTTER,
  CORNER_RADIUS,
  CURVE_CLEARANCE,
  CURVE_ROOM,
  EDGE_WIDTH,
  GUTTER_OFFSET,
  GUTTER_STEP,
  MIN_COLUMN_GAP,
  LANE_CLEARANCE,
  LANE_SPACING,
  SELF_LOOP_REACH,
  SELF_LOOP_SPREAD,
  forwardEdgePath,
  laneCount,
  laneEdgePath,
  lanesHeight,
  planGutters,
  routeEdges,
  selfLoopPath,
  sideRoom,
  slotOffset,
} from "./graphEdges";
import {
  computeGraphTopology,
  positionGraph,
  type GraphTicket,
  type Point,
  type PositionedNode,
  type Size,
} from "./graphLayout";
import { DEFAULT_STATUS } from "./status";

type Command = { op: "M" | "L" | "Q" | "C"; points: Point[] };

// Splits path data made of M, L, Q and C commands with absolute coordinates.
function parse(d: string): Command[] {
  const tokens = d.replace(/,/g, " ").trim().split(/\s+/);
  const arity = { M: 1, L: 1, Q: 2, C: 3 } as const;
  const commands: Command[] = [];
  let i = 0;
  while (i < tokens.length) {
    const op = tokens[i++] as Command["op"];
    if (!(op in arity)) throw new Error(`unexpected ${op} in ${d}`);
    const points: Point[] = [];
    for (let k = 0; k < arity[op]; k++) {
      const x = Number(tokens[i++]);
      const y = Number(tokens[i++]);
      if (Number.isNaN(x) || Number.isNaN(y)) throw new Error(`bad number in ${d}`);
      points.push({ x, y });
    }
    commands.push({ op, points });
  }
  expect(commands[0].op).toBe("M");
  return commands;
}

function firstPoint(d: string): Point {
  return parse(d)[0].points[0];
}

function lastPoint(d: string): Point {
  const commands = parse(d);
  const points = commands[commands.length - 1].points;
  return points[points.length - 1];
}

// Direction the path travels as it arrives at its end, which is where the
// marker points the arrowhead.
function arrivalDirection(d: string): Point {
  const commands = parse(d);
  const last = commands[commands.length - 1].points;
  const end = last[last.length - 1];
  const before = last.length > 1 ? last[last.length - 2] : commands[commands.length - 2].points.slice(-1)[0];
  return { x: end.x - before.x, y: end.y - before.y };
}

// The path as a polyline, with curves sampled finely enough for a pixel test.
function polyline(d: string): Point[] {
  const out: Point[] = [];
  let at: Point = { x: 0, y: 0 };
  for (const { op, points } of parse(d)) {
    if (op === "M" || op === "L") {
      at = points[0];
      out.push(at);
      continue;
    }
    const from = at;
    for (let s = 1; s <= 32; s++) {
      const t = s / 32;
      const u = 1 - t;
      if (op === "Q") {
        const [c, e] = points;
        out.push({
          x: u * u * from.x + 2 * u * t * c.x + t * t * e.x,
          y: u * u * from.y + 2 * u * t * c.y + t * t * e.y,
        });
      } else {
        const [c1, c2, e] = points;
        out.push({
          x: u * u * u * from.x + 3 * u * u * t * c1.x + 3 * u * t * t * c2.x + t * t * t * e.x,
          y: u * u * u * from.y + 3 * u * u * t * c1.y + 3 * u * t * t * c2.y + t * t * t * e.y,
        });
      }
    }
    at = points[points.length - 1];
  }
  return out;
}

interface Rect {
  x: number;
  y: number;
  width: number;
  height: number;
}

// Whether segment a-b passes through the inside of the rect, by Liang-Barsky
// clipping against the rect shrunk by half a pixel, so touching its border
// (where an arrow leaves or lands) doesn't count.
function segmentHitsRect(a: Point, b: Point, r: Rect): boolean {
  const e = 0.5;
  const [x0, x1, y0, y1] = [r.x + e, r.x + r.width - e, r.y + e, r.y + r.height - e];
  const dx = b.x - a.x;
  const dy = b.y - a.y;
  let lo = 0;
  let hi = 1;
  for (const [p, q] of [
    [-dx, a.x - x0],
    [dx, x1 - a.x],
    [-dy, a.y - y0],
    [dy, y1 - a.y],
  ]) {
    if (p === 0) {
      if (q < 0) return false;
    } else {
      const t = q / p;
      if (p < 0) lo = Math.max(lo, t);
      else hi = Math.min(hi, t);
      if (lo > hi) return false;
    }
  }
  return true;
}

function pathHitsRect(d: string, r: Rect): boolean {
  const points = polyline(d);
  return points.some((p, i) => i > 0 && segmentHitsRect(points[i - 1], p, r));
}

function ticket(key: string, deps: string[] = []): GraphTicket {
  const [prefix, number] = key.split("-");
  return {
    id: key,
    projectPrefix: prefix,
    number: Number(number),
    status: DEFAULT_STATUS,
    dependsOn: deps.map((id) => ({ id, status: DEFAULT_STATUS })),
  };
}

describe("segmentHitsRect (test helper)", () => {
  const r = { x: 10, y: 10, width: 20, height: 20 };
  it("detects crossings and ignores misses and border contact", () => {
    expect(segmentHitsRect({ x: 0, y: 20 }, { x: 40, y: 20 }, r)).toBe(true);
    expect(segmentHitsRect({ x: 20, y: 0 }, { x: 20, y: 15 }, r)).toBe(true);
    expect(segmentHitsRect({ x: 0, y: 5 }, { x: 40, y: 5 }, r)).toBe(false);
    expect(segmentHitsRect({ x: 0, y: 20 }, { x: 10, y: 20 }, r)).toBe(false);
    expect(segmentHitsRect({ x: 0, y: 0 }, { x: 9, y: 40 }, r)).toBe(false);
  });
});

describe("GUTTER_OFFSET", () => {
  it("keeps the nearest vertical run clear of the grey arrowheads landing beside it", () => {
    // An arrowhead's base sits ARROW_SIZE stroke widths left of the card it
    // points into; the run's own stroke reaches half its width towards it.
    const arrowBase = ARROW_SIZE * EDGE_WIDTH;
    const runEdge = slotOffset(0) - EDGE_WIDTH / 2;
    expect(runEdge - arrowBase).toBeGreaterThanOrEqual(2);
  });
});

describe("forwardEdgePath", () => {
  it("draws one horizontal-tangent cubic with control points half way across", () => {
    expect(forwardEdgePath({ x: 100, y: 40 }, { x: 300, y: 120 })).toBe("M 100 40 C 200 40, 200 120, 300 120");
  });

  it("keeps half-pixel coordinates and drops float noise", () => {
    expect(forwardEdgePath({ x: 0.1 + 0.2, y: 10.5 }, { x: 101, y: 20 })).toBe(
      "M 0.3 10.5 C 50.65 10.5, 50.65 20, 101 20",
    );
  });

  it("crosses each gap with a cubic and each column between with a straight run", () => {
    const via = [
      { x: 200, y: 60 },
      { x: 400, y: 60 },
      { x: 500, y: 90 },
      { x: 700, y: 90 },
    ];
    expect(forwardEdgePath({ x: 100, y: 40 }, { x: 800, y: 120 }, via)).toBe(
      "M 100 40 C 150 40, 150 60, 200 60 L 400 60 C 450 60, 450 90, 500 90 L 700 90 C 750 90, 750 120, 800 120",
    );
  });
});

describe("forwardEdgePath with leads", () => {
  it("runs straight to and from a shortened curve in each gap that has leads", () => {
    const via = [
      { x: 200, y: 60 },
      { x: 400, y: 60 },
    ];
    expect(forwardEdgePath({ x: 100, y: 40 }, { x: 500, y: 120 }, via, [{ left: 20, right: 10 }, { left: 0, right: 30 }])).toBe(
      "M 100 40 L 120 40 C 155 40, 155 60, 190 60 L 200 60 L 400 60 C 435 60, 435 120, 470 120 L 500 120",
    );
  });

  it("draws a gap without leads, or leads of zero, as before", () => {
    expect(forwardEdgePath({ x: 100, y: 40 }, { x: 300, y: 120 }, [], [{ left: 0, right: 0 }])).toBe(
      forwardEdgePath({ x: 100, y: 40 }, { x: 300, y: 120 }),
    );
  });
});

describe("laneEdgePath", () => {
  const start = { x: 600, y: 200 };
  const end = { x: 300, y: 80 };
  const d = laneEdgePath(start, end, 620, 280, 30);
  const points = polyline(d);

  it("starts at the blocker, ends at the blocked card and arrives moving right", () => {
    expect(firstPoint(d)).toEqual(start);
    expect(lastPoint(d)).toEqual(end);
    const arrival = arrivalDirection(d);
    expect(arrival.x).toBeGreaterThan(0);
    expect(arrival.y).toBe(0);
  });

  it("stays between its two vertical runs and never rises above the lane", () => {
    for (const p of points) {
      expect(p.x).toBeGreaterThanOrEqual(280);
      expect(p.x).toBeLessThanOrEqual(620);
      expect(p.y).toBeGreaterThanOrEqual(30);
    }
    expect(points.some((p) => p.y === 30)).toBe(true);
  });

  it("is straight runs joined by corners of CORNER_RADIUS", () => {
    const commands = parse(d);
    expect(commands.map((c) => c.op).join("")).toBe("MLQLQLQLQL");
    expect(commands[1].points[0]).toEqual({ x: 620 - CORNER_RADIUS, y: 200 });
  });
});

describe("selfLoopPath", () => {
  const start = { x: 256, y: 100 };
  const d = selfLoopPath(start);
  const commands = parse(d);

  it("leaves above and returns below the right-edge midpoint", () => {
    expect(firstPoint(d)).toEqual({ x: 256, y: 100 - SELF_LOOP_SPREAD });
    expect(lastPoint(d)).toEqual({ x: 256, y: 100 + SELF_LOOP_SPREAD });
  });

  it("turns at its reach level with the midpoint, smoothly", () => {
    const [first, second] = [commands[1].points, commands[2].points];
    expect(first[0].y).toBe(100 - SELF_LOOP_SPREAD);
    expect(first[2]).toEqual({ x: start.x + SELF_LOOP_REACH, y: start.y });
    expect(first[1].x).toBe(first[2].x);
    expect(second[0].x).toBe(first[2].x);
  });

  it("stays to the right of the card and points straight back into it", () => {
    for (const p of polyline(d)) expect(p.x).toBeGreaterThanOrEqual(start.x);
    const arrival = arrivalDirection(d);
    expect(arrival.x).toBeLessThan(0);
    expect(arrival.y).toBe(0);
  });
});

describe("routeEdges", () => {
  // A-1 -> A-2 -> A-3 fills the top of columns 0-2. The cycle L-1 .. L-4 spans
  // columns 0-3 below it, closed by the back edge L-4 -> L-1, and the 2-cycles
  // C-1 <-> C-2 and X-1 <-> X-2 share columns 0-1 with it. M-1 waits on C-2,
  // and S-1 depends on itself. Heights vary, so no two columns stack alike.
  const tickets = [
    ticket("A-1"),
    ticket("A-2", ["A-1"]),
    ticket("A-3", ["A-2"]),
    ticket("C-1", ["C-2"]),
    ticket("C-2", ["C-1"]),
    ticket("L-1", ["L-4"]),
    ticket("L-2", ["L-1"]),
    ticket("L-3", ["L-2"]),
    ticket("L-4", ["L-3"]),
    ticket("M-1", ["C-2"]),
    ticket("S-1", ["S-1"]),
    ticket("X-1", ["X-2"]),
    ticket("X-2", ["X-1"]),
  ];
  const topology = computeGraphTopology(tickets);
  const sizes = new Map<string, Size>(tickets.map((t, i) => [t.id, { width: 256, height: 70 + ((i * 37) % 90) }]));
  const lanes = laneCount(topology);
  const headers = 40;
  const top = headers + LANE_CLEARANCE + lanesHeight(lanes);
  const gutters = planGutters(topology);
  const layout = positionGraph(topology, {
    sizes,
    columnGap: Math.max(MIN_COLUMN_GAP, ...gutters.gaps),
    origin: { x: gutters.left, y: top },
  });
  const routed = routeEdges(layout);
  const byId = new Map<string, PositionedNode>(layout.nodes.map((n) => [n.id, n]));
  const laneRouted = routed.filter((e) => e.lane !== null);
  const highest = (d: string) => Math.min(...polyline(d).map((p) => p.y));

  it("builds the graph the test is about", () => {
    expect(routed.filter((e) => e.back).map((e) => `${e.from}->${e.to}`)).toEqual([
      "C-2->C-1",
      "L-4->L-1",
      "S-1->S-1",
      "X-2->X-1",
    ]);
    expect(byId.get("L-4")!.column - byId.get("L-1")!.column).toBe(3);
    // Other cards stand in the columns the long back edge crosses.
    for (const c of [1, 2]) expect(layout.columns[c].count).toBeGreaterThan(1);
    expect(lanes).toBe(3);
    // Three back edges fit the narrowest gutters.
    expect(gutters).toEqual({
      gaps: [MIN_COLUMN_GAP, MIN_COLUMN_GAP, MIN_COLUMN_GAP],
      left: BACK_EDGE_GUTTER,
      shelf: MIN_COLUMN_GAP,
      right: BACK_EDGE_GUTTER,
    });
  });

  it("gives each right-to-left edge its own lane, shortest spans nearest the cards", () => {
    const laneOf = Object.fromEntries(laneRouted.map((e) => [`${e.from}->${e.to}`, e.lane]));
    expect(laneOf).toEqual({ "C-2->C-1": 0, "X-2->X-1": 1, "L-4->L-1": 2 });
    const laneYs = laneRouted.map((e) => highest(e.d));
    expect(new Set(laneYs).size).toBe(laneRouted.length);
    for (const e of laneRouted) expect(highest(e.d)).toBe(top - LANE_CLEARANCE - e.lane! * LANE_SPACING);
    // Every lane stays within the room left for lanes, below the headers.
    for (const y of laneYs) expect(y).toBeGreaterThanOrEqual(headers);
  });

  it("never draws a back edge through a card, its own cards included", () => {
    for (const e of routed.filter((r) => r.back)) {
      for (const node of layout.nodes) {
        expect(pathHitsRect(e.d, node), `${e.from}->${e.to} crosses ${node.id}`).toBe(false);
      }
    }
  });

  it("starts and ends every arrow where the layout says", () => {
    routed.forEach((e, i) => {
      const edge = layout.edges[i];
      expect([e.from, e.to, e.back]).toEqual([edge.from, edge.to, edge.back]);
      if (e.from === e.to) {
        expect(e.d).toBe(selfLoopPath(edge.start));
        return;
      }
      expect(firstPoint(e.d)).toEqual(edge.start);
      expect(lastPoint(e.d)).toEqual(edge.end);
      expect(arrivalDirection(e.d).x).toBeGreaterThan(0);
      if (e.lane === null) expect(arrivalDirection(e.d).y).toBe(0);
    });
  });

  it("keeps every forward curve clear of the gaps' vertical runs", () => {
    expect(curvesGrazingRuns(routed)).toEqual([]);
    // The gap between columns 1 and 2 has runs, so its curves start and end
    // with straight leads.
    const a2 = routed.find((e) => e.from === "A-2" && e.to === "A-3")!;
    expect(parse(a2.d).map((c) => c.op).join("")).toMatch(/^MLC/);
  });

  it("draws a curve across a gap with no runs as before, straight from card to card", () => {
    // Nothing climbs or drops through the gap between columns 2 and 3.
    const l3 = routed.find((e) => e.from === "L-3" && e.to === "L-4")!;
    const edge = layout.edges.find((e) => e.from === "L-3" && e.to === "L-4")!;
    expect(l3.d).toBe(forwardEdgePath(edge.start, edge.end));
  });
});

describe("routeEdges with long forward edges", () => {
  // Chains of different lengths share the columns, and several edges skip
  // columns: L-1 -> L-5 over three, L-1 -> M-4 over two, M-1 -> L-4 over two.
  // Heights vary, so the cards in the way don't line up.
  const tickets = [
    ticket("L-1"),
    ticket("L-2", ["L-1"]),
    ticket("L-3", ["L-2"]),
    ticket("L-4", ["L-3", "M-1"]),
    ticket("L-5", ["L-4", "L-1"]),
    ticket("M-1"),
    ticket("M-2", ["M-1"]),
    ticket("M-3", ["M-2"]),
    ticket("M-4", ["M-3", "L-1"]),
    ticket("N-1"),
    ticket("N-2", ["N-1"]),
    ticket("N-3", ["N-2", "N-3"]),
    ticket("N-4", ["N-3", "L-2"]),
    ticket("C-1", ["C-2"]),
    ticket("C-2", ["C-1", "N-1"]),
  ];
  const topology = computeGraphTopology(tickets);
  const sizes = new Map<string, Size>(tickets.map((t, i) => [t.id, { width: 256, height: 64 + ((i * 41) % 80) }]));
  const gutters = planGutters(topology);
  const top = 40 + LANE_CLEARANCE + lanesHeight(laneCount(topology));
  const layout = positionGraph(topology, {
    sizes,
    columnGap: Math.max(MIN_COLUMN_GAP, ...gutters.gaps),
    origin: { x: gutters.left, y: top },
  });
  const routed = routeEdges(layout);
  const byId = new Map<string, PositionedNode>(layout.nodes.map((n) => [n.id, n]));

  it("builds the graph the test is about", () => {
    const spans = layout.edges.filter((e) => !e.back).map((e) => byId.get(e.to)!.column - byId.get(e.from)!.column);
    expect(spans.filter((span) => span > 1).length).toBeGreaterThanOrEqual(3);
    expect(Math.max(...spans)).toBe(4);
    expect(layout.edges.some((e) => e.back)).toBe(true);
  });

  it("never draws a forward edge behind a card, its own cards included", () => {
    for (const e of routed.filter((r) => !r.back)) {
      for (const node of layout.nodes) {
        expect(pathHitsRect(e.d, node), `${e.from}->${e.to} crosses ${node.id}`).toBe(false);
      }
    }
  });

  it("runs straight across every column it passes, at the height the layout kept for it", () => {
    for (const [i, e] of routed.entries()) {
      const edge = layout.edges[i];
      if (edge.back) continue;
      const commands = parse(e.d);
      for (let j = 0; j < edge.via.length; j += 2) {
        const k = commands.findIndex((c) => c.op === "L" && c.points[0].x === edge.via[j + 1].x && c.points[0].y === edge.via[j + 1].y);
        expect(k, `${e.from}->${e.to} crosses column at ${edge.via[j].x}`).toBeGreaterThan(0);
        expect(commands[k - 1].points.slice(-1)[0]).toEqual(edge.via[j]);
      }
    }
  });

  it("keeps every curve clear of the vertical runs, long edges' too", () => {
    expect(curvesGrazingRuns(routed)).toEqual([]);
  });

  it("still keeps every back edge's lane above the cards and the long edges' runs", () => {
    const highest = Math.min(...layout.nodes.map((n) => n.y), ...layout.edges.flatMap((e) => e.via.map((p) => p.y)));
    expect(highest).toBe(top);
    for (const e of routed.filter((r) => r.lane !== null)) {
      const lane = Math.min(...polyline(e.d).map((p) => p.y));
      expect(lane).toBe(top - LANE_CLEARANCE - e.lane! * LANE_SPACING);
      for (const node of layout.nodes) expect(pathHitsRect(e.d, node), `${e.from}->${e.to} crosses ${node.id}`).toBe(false);
    }
  });
});

// The vertical runs of a path: straight segments whose ends share an x.
function verticalRuns(d: string): { x: number; top: number; bottom: number }[] {
  const runs: { x: number; top: number; bottom: number }[] = [];
  let at: Point = { x: 0, y: 0 };
  for (const { op, points } of parse(d)) {
    const to = points[points.length - 1];
    if (op === "L" && to.x === at.x && to.y !== at.y) {
      runs.push({ x: to.x, top: Math.min(at.y, to.y), bottom: Math.max(at.y, to.y) });
    }
    at = to;
  }
  return runs;
}

// Forward curves whose horizontal extent reaches within 2px of a back edge's
// vertical run, where the two would run alongside each other. Leads, the
// straight parts, may cross a run: they do so at a right angle.
function curvesGrazingRuns(routed: readonly { from: string; to: string; d: string; lane: number | null }[]): string[] {
  const runs = routed.filter((e) => e.lane !== null).flatMap((e) => verticalRuns(e.d));
  const found: string[] = [];
  for (const e of routed) {
    if (e.lane !== null || e.from === e.to) continue;
    let at: Point = { x: 0, y: 0 };
    for (const { op, points } of parse(e.d)) {
      const to = points[points.length - 1];
      if (op === "C") {
        for (const run of runs) {
          if (run.x > at.x - 2 && run.x < to.x + 2) found.push(`${e.from}->${e.to} curve ${at.x}..${to.x} by run at ${run.x}`);
        }
      }
      at = to;
    }
  }
  return found;
}

describe("routeEdges with crowded gaps", () => {
  // Seven 2-cycles W: back edges from column 1 into Ready, so seven runs left
  // of Ready and seven on the right side of column 1. Seven cycles Q entered
  // from a Ready ticket: back edges from column 2 into column 1. Seven cycles R
  // entered through a chain: back edges from column 3 into column 2, so the
  // gap between columns 1 and 2 holds seven runs on each side.
  const tickets: GraphTicket[] = [];
  let w = 0;
  let q = 0;
  let r = 0;
  for (let k = 0; k < 7; k++) {
    const [w1, w2] = [`W-${++w}`, `W-${++w}`];
    tickets.push(ticket(w1, [w2]), ticket(w2, [w1]));
    const [q1, q2, q3] = [`Q-${++q}`, `Q-${++q}`, `Q-${++q}`];
    tickets.push(ticket(q1), ticket(q2, [q1, q3]), ticket(q3, [q2]));
    const [r1, r2, r3, r4] = [`R-${++r}`, `R-${++r}`, `R-${++r}`, `R-${++r}`];
    tickets.push(ticket(r1), ticket(r2, [r1]), ticket(r3, [r2, r4]), ticket(r4, [r3]));
  }
  const topology = computeGraphTopology(tickets);
  const sizes = new Map<string, Size>(tickets.map((t, i) => [t.id, { width: 256, height: 60 + ((i * 29) % 70) }]));
  const gutters = planGutters(topology);
  // Each gap as wide as its own runs need, as the page lays it out.
  const layout = positionGraph(topology, {
    sizes,
    columnGap: MIN_COLUMN_GAP,
    columnGaps: gutters.gaps,
    origin: { x: gutters.left, y: 40 + LANE_CLEARANCE + lanesHeight(laneCount(topology)) },
  });
  const back = routeEdges(layout).filter((e) => e.lane !== null);
  const byId = new Map<string, PositionedNode>(layout.nodes.map((n) => [n.id, n]));
  const into = (c: number) => back.filter((e) => byId.get(e.to)!.column === c);
  const outOf = (c: number) => back.filter((e) => byId.get(e.from)!.column === c);

  it("builds the crowding the test is about", () => {
    expect(back).toHaveLength(21);
    expect(into(0)).toHaveLength(7);
    expect(outOf(1)).toHaveLength(7);
    expect(into(1)).toHaveLength(7);
    expect(outOf(2)).toHaveLength(7);
    expect(into(2)).toHaveLength(7);
    expect(outOf(3)).toHaveLength(7);
  });

  it("widens the room a side needs past its six slots instead of reusing one", () => {
    const seven = GUTTER_OFFSET + 6 * GUTTER_STEP;
    expect(gutters.left).toBe(seven + GUTTER_OFFSET / 2);
    // A gap with runs on both sides also leaves its curves CURVE_ROOM between them.
    const lead = seven + CURVE_CLEARANCE;
    expect(gutters.gaps).toEqual([
      Math.max(MIN_COLUMN_GAP, lead + CURVE_ROOM),
      2 * lead + CURVE_ROOM,
      Math.max(MIN_COLUMN_GAP, lead + CURVE_ROOM),
    ]);
    expect(gutters.gaps[1]).toBeGreaterThan(MIN_COLUMN_GAP);
    // Laid out per gap, only the crowded gap is wider than the narrowest.
    const widths = layout.columns.slice(1).map((c, i) => c.x - layout.columns[i].x - layout.columns[i].width);
    expect(widths).toEqual(gutters.gaps);
    expect(widths.filter((w) => w > MIN_COLUMN_GAP)).toEqual([gutters.gaps[1]]);
    // The R cycles' back edges leave the last column, so its right side grows too.
    expect(gutters.right).toBe(seven + GUTTER_OFFSET / 2);
  });

  it("never runs two back edges up or down the same line", () => {
    const runs = back.flatMap((e) => verticalRuns(e.d).map((run) => ({ ...run, edge: `${e.from}->${e.to}` })));
    expect(runs).toHaveLength(2 * back.length);
    const shared: string[] = [];
    runs.forEach((a, i) =>
      runs.slice(i + 1).forEach((b) => {
        const overlap = Math.min(a.bottom, b.bottom) - Math.max(a.top, b.top);
        if (a.edge !== b.edge && Math.abs(a.x - b.x) < 1 && overlap > 0) shared.push(`${a.edge} and ${b.edge} at x=${a.x}`);
      }),
    );
    expect(shared).toEqual([]);
  });

  it("puts the nearest lane nearest the column on every side", () => {
    const inX = (e: (typeof back)[number]) => verticalRuns(e.d)[1].x;
    const outX = (e: (typeof back)[number]) => verticalRuns(e.d)[0].x;
    for (let c = 0; c < layout.columns.length; c++) {
      const incoming = into(c).sort((a, b) => a.lane! - b.lane!).map(inX);
      expect(incoming).toEqual([...incoming].sort((a, b) => b - a));
      const outgoing = outOf(c).sort((a, b) => a.lane! - b.lane!).map(outX);
      expect(outgoing).toEqual([...outgoing].sort((a, b) => a - b));
    }
  });

  it("keeps every forward curve out of the crowded gaps' runs, with CURVE_ROOM to turn in", () => {
    const routed = routeEdges(layout);
    expect(curvesGrazingRuns(routed)).toEqual([]);
    const curves = routed
      .filter((e) => e.lane === null && e.from !== e.to)
      .flatMap((e) => {
        const commands = parse(e.d);
        return commands.flatMap((c, i) => (c.op === "C" ? [c.points[2].x - commands[i - 1].points.slice(-1)[0].x] : []));
      });
    expect(curves.length).toBeGreaterThan(0);
    expect(Math.min(...curves)).toBeGreaterThanOrEqual(CURVE_ROOM);
  });

  it("still never draws a back edge through a card", () => {
    for (const e of back) {
      for (const node of layout.nodes) {
        expect(pathHitsRect(e.d, node), `${e.from}->${e.to} crosses ${node.id}`).toBe(false);
      }
    }
    // Runs left of Ready stay on the canvas.
    for (const e of into(0)) expect(verticalRuns(e.d)[1].x).toBeGreaterThan(0);
  });
});

// The page with a shelf: Ready tickets nothing links fill balanced columns
// left of the linked Ready column, and 16 two-ticket cycles send 16 back
// edges down into Ready, more than the narrowest gap has room for.
describe("routeEdges beside the shelf", () => {
  const tickets: GraphTicket[] = [];
  for (let i = 1; i <= 16; i++) tickets.push(ticket(`W-${2 * i - 1}`, [`W-${2 * i}`]), ticket(`W-${2 * i}`, [`W-${2 * i - 1}`]));
  for (let i = 1; i <= 20; i++) tickets.push(ticket(`Z-${i}`));
  const topology = computeGraphTopology(tickets);
  const gutters = planGutters(topology);
  const layout = positionGraph(topology, {
    columnGap: MIN_COLUMN_GAP,
    columnGaps: gutters.gaps,
    shelfGap: gutters.shelf,
    origin: { x: gutters.left, y: 40 + LANE_CLEARANCE + lanesHeight(laneCount(topology)) },
  });
  const back = routeEdges(layout).filter((e) => e.lane !== null);
  const shelf = layout.shelf!;
  const ready = layout.columns[0];

  it("builds the graph the test is about", () => {
    expect(topology.shelf).toHaveLength(20);
    expect(back).toHaveLength(16);
    expect(shelf.columns.length).toBeGreaterThan(1);
  });

  it("widens the gap between the shelf and Ready for the runs into Ready, and keeps the left edge narrow", () => {
    expect(gutters.shelf).toBe(sideRoom(16) + GUTTER_OFFSET / 2);
    expect(gutters.shelf).toBeGreaterThan(MIN_COLUMN_GAP);
    expect(gutters.left).toBe(BACK_EDGE_GUTTER);
    expect(shelf.x).toBe(BACK_EDGE_GUTTER);
    expect(ready.x - (shelf.x + shelf.width)).toBe(gutters.shelf);
    // Without a shelf the same runs need the room left of Ready.
    expect(planGutters({ ...topology, shelf: [] }).left).toBe(gutters.shelf);
  });

  it("runs every back edge into Ready down the gap between the shelf and Ready", () => {
    for (const e of back) {
      const x = verticalRuns(e.d)[1].x;
      expect(x).toBeGreaterThan(shelf.x + shelf.width);
      expect(x).toBeLessThan(ready.x);
    }
  });

  it("never draws a back edge through a card, the shelf's included", () => {
    for (const e of back) {
      for (const node of layout.nodes) {
        expect(pathHitsRect(e.d, node), `${e.from}->${e.to} crosses ${node.id}`).toBe(false);
      }
    }
  });
});
