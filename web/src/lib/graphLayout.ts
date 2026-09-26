// Layout for the dependency graph on the home page: ready work on the left,
// deeply blocked work on the right, arrows from a blocker to the ticket it
// blocks. Pure functions over plain data, with no React and no DOM, in two
// stages:
//
//   computeGraphTopology(tickets)       -> columns, row order, edges, counts
//   positionGraph(topology, options)    -> pixel boxes, edge ends and bends
//
// chainFinder(topology), from graphChains.ts and re-exported here, answers
// what is upstream and downstream of a node for hover highlighting.
//
// The page measures its rendered cards and feeds their sizes into the second
// stage, so placement follows real card heights; layoutGraph() runs both with
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
// - A node's dependencyTotal is its distinct dependencies of every kind: done
//   ones, ones drawn as an edge (back edges included), and hidden blockers.
//   Duplicate refs to the same ticket count once, for every count above.
// - Cycles are allowed. A depth-first search marks the edges that close a
//   cycle as back edges; those are returned flagged and ignored for columns
//   and row ordering, which leaves a DAG. The search only starts inside
//   groups of tickets that nothing outside the group blocks, so a ticket
//   whose cycle waits on anything else never lands in Ready.
// - A node's column is the longest path of non-back edges leading into it
//   (raised to 1 by an external blocker). Column 0 is Ready.
// - A Ready ticket with no edge at all, that no agent holds, leaves the
//   column for the grid below the graph (topology.grid, in ticket order). One
//   an agent holds (in_progress or agent_review) stays at the top of Ready
//   with the other held tickets, linked or not. A ticket in a later column
//   with no edge, which only hidden blockers put there, sits at the bottom of
//   its column, below every group of linked tickets. Which tickets are linked
//   is decided on the input alone, so a ticket moves between the grid and the
//   graph as it gains or loses edges.
// - A forward edge that spans more than one column gets a waypoint (a dummy
//   node) in every column it crosses. Waypoints are ordered and placed like
//   cards of no height, so the edge is drawn through a gap of its own rather
//   than behind the cards in between. topology.layers holds each column's
//   cards and waypoints together; topology.columns only the cards.
//
// Row order, the rest of the first stage, works on each connected component
// of cards and waypoints on its own, so a change in one component never
// reorders another. A column lists the components holding a Ready ticket an
// agent holds first, then the rest, each in ticket order of their lowest
// ticket. Within a component, rows start in ticket order (waypoints by
// their blocker), then sweeps alternate down the columns, ordering each by
// its blockers' rows, and back up, ordering by the rows of what it blocks,
// each followed by adjacent swaps (transposes) that remove a crossing. An
// entry with no neighbour on the side being swept keeps its row and the rest
// are sorted around it, so a row index and a barycentre are never compared.
// Sweeping stops once a sweep fails to reduce the crossings, or after
// MAX_SWEEPS, and the ordering with the fewest crossings is kept, after one
// last round of swaps. The held-first and bottom rules above split a column
// into groups that no sort or swap crosses.
//
// The second stage places each column's entries vertically in that order:
// each wants its centre at the median of its neighbours' centres, and a
// least-squares fit under the ordering (isotonic regression, by pool
// adjacent violators) gives the closest placement that keeps rowGap between
// neighbours. Rounds alternate between fitting to blockers, left to right,
// and to what each blocks, right to left; an entry with no neighbours on one
// side uses the other, and one with none at all sits tight below the entry
// above it (at the top of a column, tight above the entry below it); a column
// with no edges at all stays packed from the top.
//
// The rounds fit every component at once, so a wide one can push the next
// one down through the ordering, and nothing pulls it back up. Stacking the
// bands (stackBands) undoes that: each component becomes a band, from the
// top of its highest entry to the bottom of its lowest across every column,
// and the bands are stacked from the origin down in the order the columns
// list them, bandGap apart, each keeping its own placement. The bottom of the
// last band is the linked graph's height. Held tickets linked to nothing go
// with the band of the card above them in Ready, or below at the top. Where
// held tickets of several components top Ready above another card of one of
// them, those components can't be stacked apart and share one band. A card
// with no edge in a later column sits below the last band, bandGap under it.
// The grid starts gridGap below the lowest card or long edge's run, three
// cards across, aligned to the first three columns. Edge ends spread down a
// card side, ordered by the height of what is at the other end, with back
// edges at the top, where they turn up to their lanes.
//
// Everything is ordered by ticket (project prefix, then number, then id)
// before any decision is made, so the output does not depend on the order of
// the input array or of any dependsOn list.

import type { Ticket, TicketRef } from "../api/client";
import { isActive, isDone } from "./status";

// Upstream and downstream chains through a node, for hover highlighting.
export { chainFinder, chainRole, edgeChainRole, edgeKey, graphChains, showsCyclePill } from "./graphChains";
export type { ChainRole, EdgeChainRole, GraphChains } from "./graphChains";

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
  /** Position among the column's cards, 0 at the top; for a card in the grid, its place in the grid. */
  row: number;
  /** A Ready ticket with no edges that no agent holds, drawn in the grid below the graph. */
  inGrid: boolean;
  /** Held by an agent: in_progress or agent_review. Sorted to the top of Ready. */
  active: boolean;
  /** Distinct dependencies that are done. */
  satisfiedDependencyCount: number;
  /** Distinct unfinished dependencies that are not in the input set, so have no edge. */
  externalBlockerCount: number;
  /**
   * All of the ticket's distinct dependencies: done ones, ones drawn as an
   * edge (back edges included), and hidden blockers. satisfiedDependencyCount
   * and externalBlockerCount are both no bigger than this.
   */
  dependencyTotal: number;
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

/** A card in a column's stack. */
export interface CardEntry {
  kind: "card";
  id: string;
}

/** Where a forward edge spanning several columns crosses one in between. */
export interface WaypointEntry {
  kind: "waypoint";
  /** Index into topology.edges. */
  edge: number;
}

export type LayerEntry = CardEntry | WaypointEntry;

export interface GraphTopology<T extends GraphTicket = GraphTicket> {
  /** Ordered by column, then row, then the grid's cards in grid order. */
  nodes: GraphNode<T>[];
  /** Ordered by blocker, then blocked ticket, in ticket order. */
  edges: GraphEdge[];
  /**
   * Card ids per column, top to bottom, without the grid. Only column 0 can
   * be empty: when its tickets are all in the grid, or every would-be Ready
   * ticket is held back by an external blocker.
   */
  columns: string[][];
  /** Per column, top to bottom, its cards and the waypoints of the long edges crossing it. */
  layers: LayerEntry[][];
  /** Ids of the Ready tickets placed in the grid below the graph, in ticket order. */
  grid: string[];
  /** Node count per column. Ready's includes the grid, since those tickets are ready too. */
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
  /**
   * Horizontal space between column i and i + 1, per gap, so one gap can be
   * wider than the rest. A gap it has no entry for is columnGap.
   */
  columnGaps?: readonly number[];
  /** Vertical space between cards in a column, and between a card and a long edge passing it. */
  rowGap?: number;
  /** Top-left corner of the graph: where Ready starts and the highest card or long edge sits. */
  origin?: Point;
  /**
   * Vertical space between two bands, each a group of linked tickets, and
   * between the last band and the cards below it that no edge touches.
   */
  bandGap?: number;
  /**
   * Vertical space from the lowest card of the graph, or from the origin when
   * the columns are empty, to the grid's first row. The page puts the grid's
   * header in it.
   */
  gridGap?: number;
}

export const DEFAULT_NODE_SIZE: Size = { width: 280, height: 96 };
export const DEFAULT_COLUMN_GAP = 80;
export const DEFAULT_ROW_GAP = 16;
export const DEFAULT_GRID_GAP = 64;
/** Twice the row gap, so the space between two groups reads as more than the space inside one. */
export const DEFAULT_BAND_GAP = 32;
/** Cards per row of the grid, each under one of the first columns. */
export const GRID_COLUMNS = 3;

export interface PositionedNode<T extends GraphTicket = GraphTicket> extends GraphNode<T> {
  x: number;
  y: number;
  width: number;
  height: number;
}

export interface PositionedEdge extends GraphEdge {
  /**
   * On the blocker's right edge. With several edge ends on that side they
   * spread down its middle half, ordered by the height of what is at the
   * other end; a single end, and a self-dependency, leave from the middle.
   */
  start: Point;
  /** On the blocked card's left edge, spread the same way. */
  end: Point;
  /**
   * Where a forward edge crosses each column between its ends, left to right:
   * an entry point on the column's left side and an exit point on its right,
   * at the same height. Empty for an edge to the next column and for back edges.
   */
  via: Point[];
}

export interface PositionedColumn {
  index: number;
  x: number;
  /** Widest card in the column, or the default width if it is empty. */
  width: number;
  /** topology.columnCounts[index], so Ready's includes the grid. */
  count: number;
}

/** Where the grid of unlinked Ready tickets sits, below the graph. */
export interface PositionedGrid {
  /** Left edge of the first card, which is Ready's left edge. */
  x: number;
  /** Top of the first row of cards. */
  y: number;
  width: number;
  height: number;
  count: number;
}

export interface GraphLayout<T extends GraphTicket = GraphTicket> {
  nodes: PositionedNode<T>[];
  edges: PositionedEdge[];
  columns: PositionedColumn[];
  /** Null when no ticket is in the grid. */
  grid: PositionedGrid | null;
  /** Extent from (0, 0), origin included, to the right edge of the last column or of the grid. */
  width: number;
  /** Extent from (0, 0), origin included, to the lowest card, the grid's included. */
  height: number;
}

// Row ordering sweeps per component, each down the columns and back up. The
// loop also stops as soon as a sweep doesn't reduce the crossings.
const MAX_SWEEPS = 8;
// Rounds of adjacent swaps after a sweep. Every swap removes crossings, so
// the rounds end on their own; the bound keeps a large graph within a frame.
const MAX_TRANSPOSE_ROUNDS = 8;
// Vertical placement rounds, alternating blockers and blocked tickets. Each
// round carries positions through every column, so a few settle the graph.
const PLACEMENT_ROUNDS = 8;

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
  const total = new Array<number>(count).fill(0);
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
    // Every distinct ref lands in exactly one of the three buckets above.
    total[v] = seen.size;
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
  const columnTotal = column.reduce((max, c) => Math.max(max, c + 1), 0);

  // Each edge once, in blocker then blocked ticket order; waypoints name
  // their edge by its index here.
  const edges: GraphEdge[] = [];
  const edgeFrom: number[] = [];
  const edgeTo: number[] = [];
  const linked = new Uint8Array(count);
  successors.forEach((vs, u) =>
    vs.forEach((v, i) => {
      edges.push({ from: open[u].id, to: open[v].id, back: back.flags[u][i] });
      edgeFrom.push(u);
      edgeTo.push(v);
      linked[u] = linked[v] = 1;
    }),
  );

  const active = open.map((t) => isActive(t.status));
  const inGrid = open.map((_, v) => column[v] === 0 && !linked[v] && !active[v]);

  // The layered graph the ordering works on. Entries below `count` are the
  // nodes, grid ones left unused; the rest are waypoints. `up` and `down`
  // join each entry to its neighbours one column left and right, along the
  // forward edges only.
  const entryColumn = [...column];
  const waypointEdge: number[] = [];
  const up: number[][] = open.map(() => []);
  const down: number[][] = open.map(() => []);
  edges.forEach((edge, e) => {
    if (edge.back) return;
    let previous = edgeFrom[e];
    for (let c = column[edgeFrom[e]] + 1; c < column[edgeTo[e]]; c++) {
      const w = entryColumn.length;
      entryColumn.push(c);
      waypointEdge.push(e);
      up.push([previous]);
      down.push([]);
      down[previous].push(w);
      previous = w;
    }
    down[previous].push(edgeTo[e]);
    up[edgeTo[e]].push(previous);
  });
  const entries = entryColumn.length;

  // Connected components, by union-find that keeps the lowest entry as the
  // root. Every component holds a node, and nodes come first, so a root is
  // always the component's lowest ticket.
  const parent = Int32Array.from({ length: entries }, (_, x) => x);
  const find = (x: number) => {
    while (parent[x] !== x) {
      parent[x] = parent[parent[x]];
      x = parent[x];
    }
    return x;
  };
  down.forEach((ys, x) =>
    ys.forEach((y) => {
      const a = find(x);
      const b = find(y);
      if (a < b) parent[b] = a;
      else if (b < a) parent[a] = b;
    }),
  );

  // In column 0, tickets an agent holds are group 0 and everything else group
  // 1; in later columns a node with no edge is group 1 and everything else,
  // waypoints included, group 0. A group always sorts above the next. Both
  // active statuses share a group, so a ticket bouncing between the
  // implementer and the reviewer keeps its row as it flips.
  const group = new Uint8Array(entries);
  for (let v = 0; v < count; v++) group[v] = column[v] === 0 ? (active[v] ? 0 : 1) : linked[v] ? 0 : 1;
  // Seed rows in ticket order, a waypoint by its edge's blocker and then its
  // blocked ticket. No two entries of one column tie.
  const seed = (x: number) => (x < count ? x : edgeFrom[waypointEdge[x - count]]);
  const seedTie = (x: number) => (x < count ? -1 : edgeTo[waypointEdge[x - count]]);
  const bySeed = (a: number, b: number) => group[a] - group[b] || seed(a) - seed(b) || seedTie(a) - seedTie(b);

  // Each component's entries, ascending, by its root.
  const members = new Map<number, number[]>();
  const held = new Uint8Array(count);
  for (let x = 0; x < entries; x++) {
    if (x < count && inGrid[x]) continue;
    const root = find(x);
    const list = members.get(root);
    if (list) list.push(x);
    else members.set(root, [x]);
    if (x < count && column[x] === 0 && active[x]) held[root] = 1;
  }
  // Components holding a Ready ticket an agent holds come first, since that
  // ticket sits at the top of Ready and its component's other cards belong
  // near it; then ticket order of their lowest ticket, which is their root.
  const components = [...members.keys()].sort((a, b) => held[b] - held[a] || a - b);

  const pos = new Int32Array(entries);
  const rows: number[][] = Array.from({ length: columnTotal }, () => []);
  for (const root of components) {
    const list = members.get(root)!;
    let first = columnTotal;
    let last = 0;
    for (const x of list) {
      first = Math.min(first, entryColumn[x]);
      last = Math.max(last, entryColumn[x]);
    }
    const layers: number[][] = Array.from({ length: last - first + 1 }, () => []);
    for (const x of list) layers[entryColumn[x] - first].push(x);
    for (const layer of layers) layer.sort(bySeed);
    orderComponent(layers, up, down, group, pos).forEach((layer, i) => rows[first + i].push(...layer));
  }
  // Components went in in order; a stable sort by group keeps that within a
  // group, and each component's own order within that.
  for (const row of rows) row.sort((a, b) => group[a] - group[b]);

  const grid = open.map((_, v) => v).filter((v) => inGrid[v]);
  const nodeOf = (v: number, row: number, gridded: boolean): GraphNode<T> => ({
    id: open[v].id,
    ticket: open[v],
    column: column[v],
    row,
    inGrid: gridded,
    active: active[v],
    satisfiedDependencyCount: satisfied[v],
    externalBlockerCount: external[v],
    dependencyTotal: total[v],
  });
  const cards = rows.map((row) => row.filter((x) => x < count));
  const nodes = [
    ...cards.flatMap((vs) => vs.map((v, row) => nodeOf(v, row, false))),
    ...grid.map((v, row) => nodeOf(v, row, true)),
  ];
  const columns = cards.map((vs) => vs.map((v) => open[v].id));
  const layers = rows.map((row) =>
    row.map(
      (x): LayerEntry => (x < count ? { kind: "card", id: open[x].id } : { kind: "waypoint", edge: waypointEdge[x - count] }),
    ),
  );
  const columnCounts = columns.map((ids) => ids.length);
  if (columnTotal > 0) columnCounts[0] += grid.length;
  return { nodes, edges, columns, layers, grid: grid.map((v) => open[v].id), columnCounts };
}

// Orders one component's layers (its columns, left to right, each seeded in
// order) to reduce crossings, as the header describes, and returns them.
// `pos` is left holding each entry's row within its layer.
function orderComponent(
  layers: number[][],
  up: readonly number[][],
  down: readonly number[][],
  group: Uint8Array,
  pos: Int32Array,
): number[][] {
  const index = (layer: number[]) => layer.forEach((x, i) => (pos[x] = i));
  layers.forEach(index);
  const crossings = () => {
    let sum = 0;
    for (let i = 0; i + 1 < layers.length; i++) sum += crossingsBetween(layers[i], layers[i + 1].length, down, pos);
    return sum;
  };

  let best = crossings();
  let kept = layers.map((layer) => [...layer]);
  for (let sweep = 0; sweep < MAX_SWEEPS && best > 0; sweep++) {
    for (let i = 1; i < layers.length; i++) reorder(layers[i], up, group, pos);
    for (let i = layers.length - 2; i >= 0; i--) reorder(layers[i], down, group, pos);
    transpose(layers, up, down, group, pos);
    const now = crossings();
    if (now >= best) break;
    best = now;
    kept = layers.map((layer) => [...layer]);
  }
  kept.forEach(index);
  // The seed is kept when no sweep beats it, and it hasn't been through the
  // swaps yet; one more pass leaves every ordering where no swap helps.
  transpose(kept, up, down, group, pos);
  return kept;
}

// Sorts each group of a layer by the barycentre of its neighbours' rows on
// one side. An entry with no neighbours there keeps its row, and the others
// fill the rest of the group's rows in barycentre order, so the barycentre
// is only ever compared with another barycentre. Ties keep the current order.
function reorder(layer: number[], neighbours: readonly number[][], group: Uint8Array, pos: Int32Array) {
  const current = [...layer];
  for (let start = 0, end = 0; start < current.length; start = end) {
    while (end < current.length && group[current[end]] === group[current[start]]) end++;
    const movable: { x: number; at: number; row: number }[] = [];
    for (let row = start; row < end; row++) {
      const ns = neighbours[current[row]];
      if (ns.length === 0) continue;
      let sum = 0;
      for (const n of ns) sum += pos[n];
      movable.push({ x: current[row], at: sum / ns.length, row });
    }
    movable.sort((a, b) => a.at - b.at || a.row - b.row);
    let next = 0;
    for (let row = start; row < end; row++) {
      layer[row] = neighbours[current[row]].length === 0 ? current[row] : movable[next++].x;
    }
  }
  layer.forEach((x, i) => (pos[x] = i));
}

// Swaps neighbouring entries of a group wherever that removes crossings with
// the columns on both sides, round after round until none does.
function transpose(
  layers: number[][],
  up: readonly number[][],
  down: readonly number[][],
  group: Uint8Array,
  pos: Int32Array,
) {
  // Crossings between a's and b's edges while a sits directly above b.
  const cost = (a: number, b: number) => {
    let n = 0;
    for (const p of up[a]) for (const q of up[b]) if (pos[p] > pos[q]) n++;
    for (const p of down[a]) for (const q of down[b]) if (pos[p] > pos[q]) n++;
    return n;
  };
  for (let round = 0; round < MAX_TRANSPOSE_ROUNDS; round++) {
    let swapped = false;
    for (const layer of layers) {
      for (let i = 0; i + 1 < layer.length; i++) {
        const a = layer[i];
        const b = layer[i + 1];
        if (group[a] !== group[b] || cost(b, a) >= cost(a, b)) continue;
        layer[i] = b;
        layer[i + 1] = a;
        pos[b] = i;
        pos[a] = i + 1;
        swapped = true;
      }
    }
    if (!swapped) break;
  }
}

// Crossings between the edges from `layer` to the next one, which has `size`
// entries: the pairs whose ends are in opposite orders. Taking the edges in
// row order of their upper ends, each crosses every earlier edge whose lower
// end is further down, which a Fenwick tree over the lower rows counts.
function crossingsBetween(layer: readonly number[], size: number, down: readonly number[][], pos: Int32Array): number {
  const tree = new Int32Array(size + 1);
  let seen = 0;
  let sum = 0;
  for (const x of layer) {
    // An entry's own edges, top to bottom, don't cross each other.
    const ends = down[x].map((y) => pos[y]).sort((a, b) => a - b);
    for (const row of ends) {
      let atOrAbove = 0;
      for (let i = row + 1; i > 0; i -= i & -i) atOrAbove += tree[i];
      sum += seen - atOrAbove;
      for (let i = row + 1; i <= size; i += i & -i) tree[i]++;
      seen++;
    }
  }
  return sum;
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
  const gridGap = options.gridGap ?? DEFAULT_GRID_GAP;
  const bandGap = options.bandGap ?? DEFAULT_BAND_GAP;
  const sizeOf = (id: string) => options.sizes?.get(id) ?? defaultSize;
  const gapAfter = (column: number) => options.columnGaps?.[column] ?? columnGap;

  const columns: PositionedColumn[] = [];
  let x = origin.x;
  topology.columns.forEach((ids, index) => {
    // A loop rather than Math.max(...), which throws on very large arrays.
    let width = ids.length === 0 ? defaultSize.width : 0;
    for (const id of ids) width = Math.max(width, sizeOf(id).width);
    columns.push({ index, x, width, count: topology.columnCounts[index] });
    x += width + gapAfter(index);
  });

  // Number the layers' entries column by column and join them along the
  // forward edges, as the first stage did: a waypoint has no height.
  const cardEntry = new Map<string, number>();
  const waypoints: number[][] = topology.edges.map(() => []);
  const entryColumn: number[] = [];
  const heights: number[] = [];
  const stacks = topology.layers.map((layer, c) =>
    layer.map((entry) => {
      const e = heights.length;
      entryColumn.push(c);
      if (entry.kind === "card") {
        cardEntry.set(entry.id, e);
        heights.push(sizeOf(entry.id).height);
      } else {
        // Columns ascend, so each edge's waypoints come out left to right.
        waypoints[entry.edge].push(e);
        heights.push(0);
      }
      return e;
    }),
  );
  const up: number[][] = heights.map(() => []);
  const down: number[][] = heights.map(() => []);
  topology.edges.forEach((edge, e) => {
    if (edge.back) return;
    const chain = [cardEntry.get(edge.from)!, ...waypoints[e], cardEntry.get(edge.to)!];
    for (let i = 1; i < chain.length; i++) {
      down[chain[i - 1]].push(chain[i]);
      up[chain[i]].push(chain[i - 1]);
    }
  });

  // Tops, starting packed from 0 in every column.
  const y = new Array<number>(heights.length).fill(0);
  const offsets = stacks.map((stack) => {
    let offset = 0;
    return stack.map((e) => {
      const at = offset;
      offset += heights[e] + rowGap;
      y[e] = at;
      return at;
    });
  });
  const centre = (e: number) => y[e] + heights[e] / 2;
  const linked = stacks.map((stack) => stack.some((e) => up[e].length > 0 || down[e].length > 0));
  for (let round = 0; round < PLACEMENT_ROUNDS; round++) {
    const toBlockers = round % 2 === 0;
    for (let i = 0; i < stacks.length; i++) {
      const c = toBlockers ? i : stacks.length - 1 - i;
      if (!linked[c]) continue;
      placeStack(stacks[c], offsets[c], y, (e) => {
        let ns = toBlockers ? up[e] : down[e];
        if (ns.length === 0) ns = toBlockers ? down[e] : up[e];
        return ns.length === 0 ? null : median(ns.map(centre)) - heights[e] / 2;
      });
    }
  }

  // Stack the groups of linked tickets as bands from the origin down, and
  // the cards no edge touches below them. A column with no edges at all
  // keeps its packed stack, from the origin.
  const touched = new Uint8Array(heights.length);
  for (const list of waypoints) for (const e of list) touched[e] = 1;
  for (const edge of topology.edges) touched[cardEntry.get(edge.from)!] = touched[cardEntry.get(edge.to)!] = 1;
  stackBands(stacks, heights, down, linked, touched, y, { top: origin.y, bandGap, rowGap });
  let bottom = -Infinity;
  stacks.forEach((stack, c) =>
    stack.forEach((e) => {
      if (!linked[c]) y[e] += origin.y;
      bottom = Math.max(bottom, y[e] + heights[e]);
    }),
  );

  const byId = new Map(topology.nodes.map((n) => [n.id, n]));
  const placed = new Map<string, PositionedNode<T>>();
  topology.columns.forEach((ids, c) =>
    ids.forEach((id) => {
      const size = sizeOf(id);
      const cardTop = y[cardEntry.get(id)!];
      placed.set(id, { ...byId.get(id)!, x: columns[c].x, y: cardTop, width: size.width, height: size.height });
    }),
  );

  // The grid: three across under the first three columns, or where they
  // would be, each row as tall as its tallest card.
  let grid: PositionedGrid | null = null;
  if (topology.grid.length > 0) {
    const slots: number[] = [];
    for (let i = 0; i < GRID_COLUMNS; i++) {
      const previous = i === 0 ? origin.x : slots[i - 1] + (columns[i - 1]?.width ?? defaultSize.width) + gapAfter(i - 1);
      slots.push(columns[i]?.x ?? previous);
    }
    const gridTop = (bottom === -Infinity ? origin.y : bottom) + gridGap;
    let rowTop = gridTop;
    let right = origin.x;
    for (let start = 0; start < topology.grid.length; start += GRID_COLUMNS) {
      const row = topology.grid.slice(start, start + GRID_COLUMNS);
      let rowHeight = 0;
      row.forEach((id, i) => {
        const size = sizeOf(id);
        placed.set(id, { ...byId.get(id)!, x: slots[i], y: rowTop, width: size.width, height: size.height });
        rowHeight = Math.max(rowHeight, size.height);
        right = Math.max(right, slots[i] + size.width);
      });
      rowTop += rowHeight + rowGap;
    }
    const height = rowTop - rowGap - gridTop;
    grid = { x: slots[0], y: gridTop, width: right - slots[0], height, count: topology.grid.length };
    bottom = gridTop + height;
  }

  const nodes = topology.nodes.map((n) => placed.get(n.id)!);
  const cardCentre = (id: string) => centre(cardEntry.get(id)!);
  const edges = positionEdges(topology.edges, placed, columns, waypoints, entryColumn, y, cardCentre);
  const last = columns[columns.length - 1];
  const columnsRight = last ? last.x + last.width : origin.x;
  return {
    nodes,
    edges,
    columns,
    grid,
    width: Math.max(columnsRight, grid ? grid.x + grid.width : origin.x),
    height: Math.max(origin.y, bottom),
  };
}

// Stacks the groups of linked tickets as bands, one under another, and
// returns the bottom of the lowest band: the height of the linked graph. It
// places every entry of the columns that have an edge (`linked`) and leaves
// the other columns alone.
//
// A group is a connected component of cards and waypoints, joined along the
// forward edges (`down`); a back edge always closes a cycle inside one, and a
// card whose only edge is a self-dependency is a group of its own. A band is
// a group, or several merged as below, and keeps the placement rounds'
// positions within it: its extent, from the top of its highest entry to the
// bottom of its lowest across every column, moves as one, the first band to
// `top` and each next one bandGap below the one before.
//
// Cards no edge touches belong to no group of their own. In Ready they are
// held tickets, kept on top by the first stage, and join the group of the
// nearest card above them in the column, or below at the top, which is where
// the rounds left them. In later columns they sit below the last band,
// bandGap under it and rowGap apart, in their column's order.
//
// Bands are stacked in the order the columns list them, which never reorders
// a column: a group goes before every group listed below it in some column.
// Groups listed in both orders, which only held tickets from several groups
// at the top of Ready cause, can't be stacked apart and share one band.
// Where the columns leave a choice, the group listed first, column by column,
// goes first.
function stackBands(
  stacks: readonly number[][],
  heights: readonly number[],
  down: readonly number[][],
  linked: readonly boolean[],
  touched: Uint8Array,
  y: number[],
  gaps: { top: number; bandGap: number; rowGap: number },
): number {
  const parent = Int32Array.from(heights, (_, x) => x);
  const find = (x: number) => {
    while (parent[x] !== x) {
      parent[x] = parent[parent[x]];
      x = parent[x];
    }
    return x;
  };
  const union = (a: number, b: number) => {
    parent[find(a)] = find(b);
  };
  down.forEach((ends, x) => ends.forEach((z) => union(x, z)));

  // The cards no edge touches: in Ready, joined to a group; later, set aside.
  const trailing: number[][] = stacks.map(() => []);
  stacks.forEach((stack, c) => {
    if (!linked[c]) return;
    if (c > 0) {
      for (const e of stack) if (!touched[e]) trailing[c].push(e);
      return;
    }
    // A linked column has a touched entry.
    let above = stack.find((e) => touched[e])!;
    for (const e of stack) {
      if (touched[e]) above = e;
      else union(e, above);
    }
  });

  // Groups numbered by their first listing, column by column, their entries,
  // and the groups each column lists directly below each one.
  const groupOf = new Map<number, number>();
  const members: number[][] = [];
  const below: Set<number>[] = [];
  stacks.forEach((stack, c) => {
    if (!linked[c]) return;
    let previous = -1;
    for (const e of stack) {
      if (c > 0 && !touched[e]) continue;
      const root = find(e);
      let g = groupOf.get(root);
      if (g === undefined) {
        g = members.length;
        groupOf.set(root, g);
        members.push([]);
        below.push(new Set());
      }
      members[g].push(e);
      if (previous !== -1 && previous !== g) below[previous].add(g);
      previous = g;
    }
  });

  // Groups listed in both orders are strongly connected; each such set is
  // one band. A band is named by its first group, and taken once every band
  // listed above it is placed, the lowest named first.
  const bandOf = stronglyConnectedComponents(below.map((gs) => [...gs]));
  const bands = new Map<number, number[]>();
  members.forEach((_, g) => {
    const list = bands.get(bandOf[g]);
    if (list) list.push(g);
    else bands.set(bandOf[g], [g]);
  });
  const waiting = new Map<number, number>();
  const next = new Map<number, Set<number>>();
  for (const b of bands.keys()) {
    waiting.set(b, 0);
    next.set(b, new Set());
  }
  below.forEach((gs, g) =>
    gs.forEach((h) => {
      const from = bandOf[g];
      const to = bandOf[h];
      if (from === to || next.get(from)!.has(to)) return;
      next.get(from)!.add(to);
      waiting.set(to, waiting.get(to)! + 1);
    }),
  );
  // Groups ascend in each band's list, so its first is its name.
  const ready = new MinHeap();
  for (const [b, n] of waiting) if (n === 0) ready.push(bands.get(b)![0]);

  let cursor = gaps.top;
  let bottom = gaps.top;
  while (ready.size > 0) {
    const b = bandOf[ready.pop()];
    const entries = bands.get(b)!.flatMap((g) => members[g]);
    let top = Infinity;
    let low = -Infinity;
    for (const e of entries) {
      top = Math.min(top, y[e]);
      low = Math.max(low, y[e] + heights[e]);
    }
    const shift = cursor - top;
    for (const e of entries) y[e] += shift;
    bottom = low + shift;
    cursor = bottom + gaps.bandGap;
    for (const d of next.get(b)!) {
      const n = waiting.get(d)! - 1;
      waiting.set(d, n);
      if (n === 0) ready.push(bands.get(d)![0]);
    }
  }

  for (const column of trailing) {
    let at = cursor;
    for (const e of column) {
      y[e] = at;
      at += heights[e] + gaps.rowGap;
    }
  }
  return bottom;
}

// The smallest-first queue stackBands takes bands from.
class MinHeap {
  private items: number[] = [];

  get size(): number {
    return this.items.length;
  }

  push(value: number) {
    const items = this.items;
    let i = items.push(value) - 1;
    while (i > 0) {
      const up = (i - 1) >> 1;
      if (items[up] <= value) break;
      items[i] = items[up];
      i = up;
    }
    items[i] = value;
  }

  pop(): number {
    const items = this.items;
    const top = items[0];
    const last = items.pop()!;
    if (items.length > 0) {
      let i = 0;
      for (;;) {
        let child = 2 * i + 1;
        if (child >= items.length) break;
        if (child + 1 < items.length && items[child + 1] < items[child]) child++;
        if (items[child] >= last) break;
        items[i] = items[child];
        i = child;
      }
      items[i] = last;
    }
    return top;
  }
}

function median(values: number[]): number {
  if (values.length === 1) return values[0];
  const sorted = [...values].sort((a, b) => a - b);
  const mid = sorted.length >> 1;
  return sorted.length % 2 === 1 ? sorted[mid] : (sorted[mid - 1] + sorted[mid]) / 2;
}

// Places one column's stack, top to bottom, as close as the order allows to
// the tops `target` asks for, in least squares. With z = top - offset, where
// offset is the entry's top with the stack packed from 0, keeping rowGap
// between neighbours is exactly z never decreasing down the stack. So the
// fit is an isotonic regression of the targets' z, which pool adjacent
// violators solves in one pass: a run of entries that would overlap moves as
// one block to the mean of its targets. An entry with no target (null) sits
// tight below the entry above it, or above the first entry with one. The
// block means are rounded, which keeps them in order, so every top is a
// whole pixel when the sizes are.
function placeStack(
  stack: readonly number[],
  offsets: readonly number[],
  y: number[],
  target: (e: number) => number | null,
) {
  const wanted: number[] = [];
  const blocks: { sum: number; n: number }[] = [];
  stack.forEach((e, i) => {
    const t = target(e);
    if (t === null) return;
    wanted.push(i);
    let block = { sum: t - offsets[i], n: 1 };
    while (blocks.length > 0 && blocks[blocks.length - 1].sum / blocks[blocks.length - 1].n > block.sum / block.n) {
      const above = blocks.pop()!;
      block = { sum: above.sum + block.sum, n: above.n + block.n };
    }
    blocks.push(block);
  });
  if (wanted.length === 0) return;
  const z = new Array<number>(stack.length);
  let k = 0;
  for (const block of blocks) {
    const mean = Math.round(block.sum / block.n);
    for (let j = 0; j < block.n; j++) z[wanted[k++]] = mean;
  }
  let current = z[wanted[0]];
  stack.forEach((e, i) => {
    if (z[i] !== undefined) current = z[i];
    y[e] = current + offsets[i];
  });
}

// Edge ends and bends. Each side of a card lists the edges that end there:
// back edges first, in the order their lanes are numbered (shortest span
// first, then edge order), since they turn up to the lanes above the cards,
// then forward edges by the height of the next point along the edge, the card
// or waypoint at the other end of their first or last stretch. Self-loops
// keep the middle of the right side and are left out.
function positionEdges<T extends GraphTicket>(
  edges: readonly GraphEdge[],
  placed: ReadonlyMap<string, PositionedNode<T>>,
  columns: readonly PositionedColumn[],
  waypoints: readonly number[][],
  entryColumn: readonly number[],
  y: readonly number[],
  cardCentre: (id: string) => number,
): PositionedEdge[] {
  type End = { edge: number; back: boolean; key: number };
  const outs = new Map<string, End[]>();
  const ins = new Map<string, End[]>();
  const add = (side: Map<string, End[]>, id: string, end: End) => {
    const list = side.get(id);
    if (list) list.push(end);
    else side.set(id, [end]);
  };
  edges.forEach((edge, e) => {
    if (edge.from === edge.to) return;
    if (edge.back) {
      const span = placed.get(edge.from)!.column - placed.get(edge.to)!.column;
      add(outs, edge.from, { edge: e, back: true, key: span });
      add(ins, edge.to, { edge: e, back: true, key: span });
      return;
    }
    const via = waypoints[e];
    add(outs, edge.from, { edge: e, back: false, key: via.length > 0 ? y[via[0]] : cardCentre(edge.to) });
    add(ins, edge.to, { edge: e, back: false, key: via.length > 0 ? y[via[via.length - 1]] : cardCentre(edge.from) });
  });

  // The height of each edge's end on each side.
  const spread = (side: Map<string, End[]>) => {
    const at = new Map<number, number>();
    for (const [id, list] of side) {
      const card = placed.get(id)!;
      list.sort((a, b) => Number(b.back) - Number(a.back) || a.key - b.key || a.edge - b.edge);
      list.forEach((end, i) => {
        const share = list.length === 1 ? 0.5 : 0.25 + (0.5 * i) / (list.length - 1);
        at.set(end.edge, card.y + card.height * share);
      });
    }
    return at;
  };
  const startY = spread(outs);
  const endY = spread(ins);

  return edges.map((edge, e): PositionedEdge => {
    const from = placed.get(edge.from)!;
    const to = placed.get(edge.to)!;
    const via = waypoints[e].flatMap((w) => {
      const column = columns[entryColumn[w]];
      return [
        { x: column.x, y: y[w] },
        { x: column.x + column.width, y: y[w] },
      ];
    });
    return {
      ...edge,
      start: { x: from.x + from.width, y: startY.get(e) ?? from.y + from.height / 2 },
      end: { x: to.x, y: endY.get(e) ?? to.y + to.height / 2 },
      via,
    };
  });
}

/** A rectangle on the canvas: a size at a top-left corner. */
export interface Bounds extends Point, Size {}

/** The part of a positioned card a bounding box reads. */
export type CardBox = Bounds & { id: string };

/**
 * The smallest rectangle covering the cards whose ids are in `ids`, which is
 * what the Fit button frames while filters narrow the graph down.
 *
 * The answer is null whenever there is nothing narrower than the canvas to
 * frame: `ids` is null because no filter is set, no card matches, or every
 * card does. The caller falls back to the whole canvas then, which is more
 * than the cards' own box — it starts at the origin, so the column headers
 * and the back edges' lanes above the cards come with it, and it includes the
 * gutter those edges run down on the right.
 */
export function matchingBounds(cards: readonly CardBox[], ids: ReadonlySet<string> | null): Bounds | null {
  if (ids === null) return null;
  let matched = 0;
  let left = Infinity;
  let top = Infinity;
  let right = -Infinity;
  let bottom = -Infinity;
  for (const card of cards) {
    if (!ids.has(card.id)) continue;
    matched++;
    left = Math.min(left, card.x);
    top = Math.min(top, card.y);
    right = Math.max(right, card.x + card.width);
    bottom = Math.max(bottom, card.y + card.height);
  }
  if (matched === 0 || matched === cards.length) return null;
  return { x: left, y: top, width: right - left, height: bottom - top };
}

/** Both stages in one call, for callers that have no measured sizes to pass in between. */
export function layoutGraph<T extends GraphTicket>(
  tickets: readonly T[],
  options?: PositionOptions,
): GraphLayout<T> {
  return positionGraph(computeGraphTopology(tickets), options);
}
