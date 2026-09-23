import { describe, expect, it } from "vitest";
import {
  BACK_EDGE_GUTTER,
  CORNER_RADIUS,
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
    expect(gutters).toEqual({ gaps: [MIN_COLUMN_GAP, MIN_COLUMN_GAP, MIN_COLUMN_GAP], left: BACK_EDGE_GUTTER, right: BACK_EDGE_GUTTER });
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
      if (e.lane === null) expect(e.d).toBe(forwardEdgePath(edge.start, edge.end, edge.via));
    });
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
      expect(e.d).toBe(forwardEdgePath(edge.start, edge.end, edge.via));
      const straight = parse(e.d).filter((c) => c.op === "L");
      expect(straight).toHaveLength(edge.via.length / 2);
    }
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
  const layout = positionGraph(topology, {
    sizes,
    columnGap: Math.max(MIN_COLUMN_GAP, ...gutters.gaps),
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
    expect(gutters.gaps).toEqual([
      Math.max(MIN_COLUMN_GAP, seven + GUTTER_OFFSET),
      2 * seven + GUTTER_OFFSET,
      Math.max(MIN_COLUMN_GAP, seven + GUTTER_OFFSET),
    ]);
    expect(gutters.gaps[1]).toBeGreaterThan(MIN_COLUMN_GAP);
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
