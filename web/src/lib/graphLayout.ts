// Layout for the dependency graph on the home page: ready work on the left,
// deeply blocked work on the right, arrows from a blocker to the ticket it
// blocks. Pure functions over plain data, with no React and no DOM, in two
// stages:
//
//   computeGraphTopology(tickets)       -> columns, row order, edges, counts
//   positionGraph(topology, options)    -> pixel boxes and edge endpoints
//
// The page measures its rendered cards and feeds their sizes into the second
// stage, so stacking follows real card heights; layoutGraph() runs both with
// default sizes for callers that have no measurements yet.
//
// The model:
//
// - Every ticket whose status is not done is a node. Ticket ids are assumed
//   to be unique.
// - A dependency on another node is an edge from the dependency (the blocker)
//   to the dependent. A dependency on a done ticket is not an edge; it counts
//   towards the node's satisfiedDependencyCount.
// - A dependency on an unfinished ticket that is not in the input (the page
//   filtered it out, say) is not an edge either. It counts towards the node's
//   externalBlockerCount, and the node is placed at least one column right of
//   Ready, since it is blocked by something even if that thing isn't drawn.
// - Cycles are allowed. A depth-first search marks the edges that close a
//   cycle as back edges; those are returned flagged and ignored for columns
//   and row ordering, which leaves a DAG. The search only starts inside
//   groups of tickets that nothing outside the group blocks, so a ticket
//   whose cycle waits on anything else never lands in Ready.
// - A node's column is the longest path of non-back edges leading into it
//   (raised to 1 by an external blocker). Column 0 is Ready.
// - Rows start in ticket order, with in-progress tickets at the top of
//   column 0, then a few bounded barycentre sweeps reorder each column
//   towards its neighbours to reduce crossings. In column 0 the sweeps only
//   reorder within the in-progress group and within the rest, so the
//   in-progress-first rule always holds.
//
// Everything is ordered by ticket (project prefix, then number, then id)
// before any decision is made, so the output does not depend on the order of
// the input array or of any dependsOn list.

import type { Ticket, TicketRef } from "../api/client";
import { isDone, isInProgress } from "./status";

/** The part of a TicketRef the layout reads. */
export type GraphTicketRef = Pick<TicketRef, "id" | "status">;

/** The part of a Ticket the layout reads. A full Ticket satisfies it. */
export type GraphTicket = Pick<Ticket, "id" | "projectPrefix" | "number" | "status"> & {
  dependsOn?: readonly GraphTicketRef[];
};

export interface GraphNode<T extends GraphTicket = GraphTicket> {
  id: string;
  ticket: T;
  /** Unfinished-dependency steps from Ready; 0 is the Ready column. */
  column: number;
  /** Position within the column, 0 at the top. */
  row: number;
  inProgress: boolean;
  /** Distinct dependencies that are done. */
  satisfiedDependencyCount: number;
  /** Distinct unfinished dependencies that are not in the input set, so have no edge. */
  externalBlockerCount: number;
}

export interface GraphEdge {
  /** Id of the blocker, the ticket depended on. */
  from: string;
  /** Id of the blocked ticket, the one whose dependsOn names `from`. */
  to: string;
  /**
   * True for an edge removed to break a cycle; draw it as a back edge. It
   * plays no part in columns or row order. A self-dependency is always one.
   */
  back: boolean;
}

export interface GraphTopology<T extends GraphTicket = GraphTicket> {
  /** Ordered by column, then row. */
  nodes: GraphNode<T>[];
  /** Ordered by blocker, then blocked ticket, in ticket order. */
  edges: GraphEdge[];
  /**
   * Node ids per column, top to bottom. Only column 0 can be empty, when every
   * would-be Ready ticket is held back by an external blocker.
   */
  columns: string[][];
  /** Node count per column; columnCounts[i] === columns[i].length. */
  columnCounts: number[];
}

export interface Size {
  width: number;
  height: number;
}

export interface Point {
  x: number;
  y: number;
}

export interface PositionOptions {
  /** Measured card sizes by ticket id. Missing entries use defaultSize. */
  sizes?: ReadonlyMap<string, Size>;
  defaultSize?: Size;
  /** Horizontal space between columns. */
  columnGap?: number;
  /** Vertical space between cards in a column. */
  rowGap?: number;
  /** Top-left corner of column 0's first card, e.g. to leave room for headers. */
  origin?: Point;
}

export const DEFAULT_NODE_SIZE: Size = { width: 280, height: 96 };
export const DEFAULT_COLUMN_GAP = 80;
export const DEFAULT_ROW_GAP = 16;

export interface PositionedNode<T extends GraphTicket = GraphTicket> extends GraphNode<T> {
  x: number;
  y: number;
  width: number;
  height: number;
}

export interface PositionedEdge extends GraphEdge {
  /** Middle of the blocker's right edge. */
  start: Point;
  /** Middle of the blocked card's left edge. */
  end: Point;
}

export interface PositionedColumn {
  index: number;
  x: number;
  /** Widest card in the column, or the default width if it is empty. */
  width: number;
  count: number;
  /** Height of the stacked cards and the gaps between them. */
  height: number;
}

export interface GraphLayout<T extends GraphTicket = GraphTicket> {
  nodes: PositionedNode<T>[];
  edges: PositionedEdge[];
  columns: PositionedColumn[];
  /** Extent from (0, 0), origin included, to the right edge of the last column. */
  width: number;
  /** Extent from (0, 0), origin included, to the bottom of the tallest column. */
  height: number;
}

// Down-and-up barycentre sweeps. A small fixed number keeps the cost bounded
// and the result deterministic; it isn't iterated to convergence.
const BARYCENTRE_SWEEPS = 2;

function compareTickets(a: GraphTicket, b: GraphTicket): number {
  if (a.projectPrefix !== b.projectPrefix) return a.projectPrefix < b.projectPrefix ? -1 : 1;
  if (a.number !== b.number) return a.number - b.number;
  if (a.id !== b.id) return a.id < b.id ? -1 : 1;
  return 0;
}

export function computeGraphTopology<T extends GraphTicket>(tickets: readonly T[]): GraphTopology<T> {
  const byId = new Map<string, T>();
  for (const ticket of tickets) byId.set(ticket.id, ticket);

  // Nodes are indexed in ticket order; every later tiebreak leans on this.
  const open = [...byId.values()].filter((t) => !isDone(t.status)).sort(compareTickets);
  const count = open.length;
  const indexOf = new Map<string, number>();
  open.forEach((t, i) => indexOf.set(t.id, i));

  const successors: number[][] = open.map(() => []);
  const satisfied = new Array<number>(count).fill(0);
  const external = new Array<number>(count).fill(0);
  for (let v = 0; v < count; v++) {
    const seen = new Set<string>();
    for (const ref of open[v].dependsOn ?? []) {
      if (seen.has(ref.id)) continue;
      seen.add(ref.id);
      const u = indexOf.get(ref.id);
      if (u !== undefined) {
        // v ascends, so each successor list comes out sorted.
        successors[u].push(v);
      } else if (byId.has(ref.id) || isDone(ref.status)) {
        // An input ticket that isn't a node is done; trust its own status
        // over the ref's copy.
        satisfied[v]++;
      } else {
        external[v]++;
      }
    }
  }

  const back = breakCycles(successors, external);

  // breakCycles leaves every non-back edge pointing forward in `order`, so one
  // pass computes the longest path.
  const column = external.map((n): number => (n > 0 ? 1 : 0));
  for (const u of back.order) {
    successors[u].forEach((v, i) => {
      if (!back.flags[u][i]) column[v] = Math.max(column[v], column[u] + 1);
    });
  }

  const predecessors: number[][] = open.map(() => []);
  successors.forEach((vs, u) =>
    vs.forEach((v, i) => {
      if (!back.flags[u][i]) predecessors[v].push(u);
    }),
  );
  const forwardSuccessors = successors.map((vs, u) => vs.filter((_, i) => !back.flags[u][i]));

  const columnTotal = column.reduce((max, c) => Math.max(max, c + 1), 0);
  const rows: number[][] = Array.from({ length: columnTotal }, () => []);
  for (let v = 0; v < count; v++) rows[column[v]].push(v);

  const inProgress = open.map((t) => isInProgress(t.status));
  // In column 0, in-progress tickets are group 0 and everything else group 1,
  // and a group always sorts above the next. Other columns are one group.
  const group = (v: number, c: number) => (c === 0 && !inProgress[v] ? 1 : 0);
  const position = new Array<number>(count).fill(0);
  const barycentre = new Array<number>(count).fill(0);
  const reorder = (c: number, neighbours: number[][]) => {
    const nodes = rows[c];
    nodes.forEach((v, i) => {
      const ns = neighbours[v];
      // A node with no neighbours on that side keeps its place.
      barycentre[v] = ns.length === 0 ? i : ns.reduce((sum, n) => sum + position[n], 0) / ns.length;
    });
    // Array.prototype.sort is stable, so ties keep the current order.
    nodes.sort((a, b) => group(a, c) - group(b, c) || barycentre[a] - barycentre[b]);
    nodes.forEach((v, i) => (position[v] = i));
  };

  // Seed rows in ticket order, lifting in-progress tickets in column 0.
  const none: number[][] = open.map(() => []);
  for (let c = 0; c < columnTotal; c++) reorder(c, none);
  for (let sweep = 0; sweep < BARYCENTRE_SWEEPS; sweep++) {
    for (let c = 1; c < columnTotal; c++) reorder(c, predecessors);
    for (let c = columnTotal - 2; c >= 0; c--) reorder(c, forwardSuccessors);
  }

  const nodes: GraphNode<T>[] = rows.flatMap((vs) =>
    vs.map((v) => ({
      id: open[v].id,
      ticket: open[v],
      column: column[v],
      row: position[v],
      inProgress: inProgress[v],
      satisfiedDependencyCount: satisfied[v],
      externalBlockerCount: external[v],
    })),
  );
  const edges: GraphEdge[] = successors.flatMap((vs, u) =>
    vs.map((v, i) => ({ from: open[u].id, to: open[v].id, back: back.flags[u][i] })),
  );
  const columns = rows.map((vs) => vs.map((v) => open[v].id));
  return { nodes, edges, columns, columnCounts: columns.map((c) => c.length) };
}

// Depth-first search that flags every edge into a node still on the stack.
// Removing those leaves a DAG, and the reverse of the finishing order is a
// topological order of it.
//
// Where the search starts decides which edge of a cycle is flagged, and so
// which ticket of the cycle lands leftmost. It starts only in source
// components: strongly connected components that no edge from outside the
// component enters. Every ticket is reachable from one, and a ticket outside
// them is always reached through an unflagged edge, so it can't end up in
// Ready. Source components are started in ticket order of their lowest
// ticket. Within one, the search starts from its lowest ticket with an
// external blocker if it has one, which puts that ticket in column 1 and the
// rest of the component to its right, and otherwise from its lowest ticket.
// A cycle entered from outside is therefore broken at the edge that returns
// to where the search came in. Both passes are iterative, so a long chain or
// cycle can't overflow the call stack.
function breakCycles(
  successors: number[][],
  external: readonly number[],
): { flags: boolean[][]; order: number[] } {
  const count = successors.length;
  const component = stronglyConnectedComponents(successors);

  // Component numbers are below the node count.
  const entered = new Uint8Array(count);
  successors.forEach((vs, u) =>
    vs.forEach((v) => {
      if (component[u] !== component[v]) entered[component[v]] = 1;
    }),
  );
  // Nodes ascend, so the first node seen in a component is its lowest.
  const rootOf = new Map<number, number>();
  const starts: number[] = [];
  for (let v = 0; v < count; v++) {
    const c = component[v];
    if (entered[c]) continue;
    if (!rootOf.has(c)) {
      rootOf.set(c, v);
      starts.push(c);
    } else if (external[v] > 0 && external[rootOf.get(c)!] === 0) {
      rootOf.set(c, v);
    }
  }

  const flags = successors.map((vs) => vs.map(() => false));
  const UNSEEN = 0;
  const ON_STACK = 1;
  const FINISHED = 2;
  const state = new Uint8Array(count);
  const finished: number[] = [];

  for (const c of starts) {
    const root = rootOf.get(c)!;
    const stack: [node: number, next: number][] = [[root, 0]];
    state[root] = ON_STACK;
    while (stack.length > 0) {
      const frame = stack[stack.length - 1];
      const [u, i] = frame;
      if (i < successors[u].length) {
        frame[1]++;
        const v = successors[u][i];
        if (state[v] === ON_STACK) {
          flags[u][i] = true;
        } else if (state[v] === UNSEEN) {
          state[v] = ON_STACK;
          stack.push([v, 0]);
        }
      } else {
        state[u] = FINISHED;
        finished.push(u);
        stack.pop();
      }
    }
  }
  return { flags, order: finished.reverse() };
}

// Tarjan's algorithm with an explicit call stack. Returns a component number
// per node; the numbering itself carries no meaning.
function stronglyConnectedComponents(successors: number[][]): Int32Array {
  const count = successors.length;
  const index = new Int32Array(count).fill(-1);
  const low = new Int32Array(count);
  const onStack = new Uint8Array(count);
  const component = new Int32Array(count).fill(-1);
  const stack: number[] = [];
  let nextIndex = 0;
  let nextComponent = 0;

  for (let start = 0; start < count; start++) {
    if (index[start] !== -1) continue;
    const calls: [node: number, next: number][] = [[start, 0]];
    index[start] = low[start] = nextIndex++;
    stack.push(start);
    onStack[start] = 1;
    while (calls.length > 0) {
      const frame = calls[calls.length - 1];
      const [u, i] = frame;
      if (i < successors[u].length) {
        frame[1]++;
        const v = successors[u][i];
        if (index[v] === -1) {
          index[v] = low[v] = nextIndex++;
          stack.push(v);
          onStack[v] = 1;
          calls.push([v, 0]);
        } else if (onStack[v]) {
          low[u] = Math.min(low[u], index[v]);
        }
        continue;
      }
      calls.pop();
      if (low[u] === index[u]) {
        let w: number;
        do {
          w = stack.pop()!;
          onStack[w] = 0;
          component[w] = nextComponent;
        } while (w !== u);
        nextComponent++;
      }
      if (calls.length > 0) {
        const parent = calls[calls.length - 1][0];
        low[parent] = Math.min(low[parent], low[u]);
      }
    }
  }
  return component;
}

export function positionGraph<T extends GraphTicket>(
  topology: GraphTopology<T>,
  options: PositionOptions = {},
): GraphLayout<T> {
  const defaultSize = options.defaultSize ?? DEFAULT_NODE_SIZE;
  const columnGap = options.columnGap ?? DEFAULT_COLUMN_GAP;
  const rowGap = options.rowGap ?? DEFAULT_ROW_GAP;
  const origin = options.origin ?? { x: 0, y: 0 };
  const sizeOf = (id: string) => options.sizes?.get(id) ?? defaultSize;

  const byId = new Map(topology.nodes.map((n) => [n.id, n]));
  const placed = new Map<string, PositionedNode<T>>();
  const columns: PositionedColumn[] = [];
  let x = origin.x;
  topology.columns.forEach((ids, index) => {
    // A loop rather than Math.max(...), which throws on very large arrays.
    let width = ids.length === 0 ? defaultSize.width : 0;
    for (const id of ids) width = Math.max(width, sizeOf(id).width);
    let y = origin.y;
    ids.forEach((id, row) => {
      const size = sizeOf(id);
      if (row > 0) y += rowGap;
      placed.set(id, { ...byId.get(id)!, x, y, width: size.width, height: size.height });
      y += size.height;
    });
    columns.push({ index, x, width, count: ids.length, height: y - origin.y });
    x += width + columnGap;
  });

  const nodes = topology.nodes.map((n) => placed.get(n.id)!);
  const edges = topology.edges.map((e): PositionedEdge => {
    const from = placed.get(e.from)!;
    const to = placed.get(e.to)!;
    return {
      ...e,
      start: { x: from.x + from.width, y: from.y + from.height / 2 },
      end: { x: to.x, y: to.y + to.height / 2 },
    };
  });
  const last = columns[columns.length - 1];
  return {
    nodes,
    edges,
    columns,
    width: last ? last.x + last.width : origin.x,
    height: origin.y + columns.reduce((max, c) => Math.max(max, c.height), 0),
  };
}

/** Both stages in one call, for callers that have no measured sizes to pass in between. */
export function layoutGraph<T extends GraphTicket>(
  tickets: readonly T[],
  options?: PositionOptions,
): GraphLayout<T> {
  return positionGraph(computeGraphTopology(tickets), options);
}
