// The chains the graph page highlights when a card is hovered or focused:
// everything upstream of the card (what blocks it, directly or through other
// tickets) and everything downstream (what it blocks), with the edges on
// those chains. Pure functions over a GraphTopology; graphLayout.ts
// re-exports them.
//
// Definitions, for a card h:
//
// - upstream is every node with a path of one or more edges into h, and
//   downstream every node with a path of one or more edges out of h. Back
//   edges count like any other edge; they only matter to the layout.
// - An edge is on the upstream chain when it ends at h or at an upstream
//   node, so it lies on a path into h. An edge is on the downstream chain when
//   it starts at h or at a downstream node.
//
// Cycles make a node both. A node is upstream and downstream of h exactly
// when it is in a cycle with h, and h itself is in both sets only when it
// lies on a cycle, a self-dependency included. An edge is on both chains
// exactly when both its ends are in a cycle with h (h's own self-dependency
// included). chainRole() and edgeChainRole() name that overlap "cycle", so
// the page gives it a colour of its own rather than letting one chain win.
//
// An edge that skips h, from an upstream node straight to a downstream one,
// is on neither chain: it doesn't lie on a path through h, so it stays dimmed
// even though both its cards are lit.
//
// The searches are iterative, so a long chain or cycle can't overflow the
// call stack. Each query walks the incoming edges of h and its upstream
// nodes, and the outgoing edges of h and its downstream nodes, once each:
// O(nodes + edges). chainFinder() builds the adjacency once per topology, so
// a query does only that walk. Answers aren't kept: holding every card's
// sets costs memory that grows with nodes times edges, hundreds of megabytes
// on a large graph, to save a query of a few milliseconds.

import type { GraphEdge, GraphTicket, GraphTopology } from "./graphLayout";

export interface GraphChains {
  /** The hovered or focused node. */
  id: string;
  /** Node ids with a path into `id`. Includes `id` only if it lies on a cycle. */
  upstream: ReadonlySet<string>;
  /** Node ids with a path out of `id`. Includes `id` only if it lies on a cycle. */
  downstream: ReadonlySet<string>;
  /** edgeKey()s of edges on a path into `id`: those ending at `id` or at an upstream node. */
  upstreamEdges: ReadonlySet<string>;
  /** edgeKey()s of edges on a path out of `id`: those starting at `id` or at a downstream node. */
  downstreamEdges: ReadonlySet<string>;
}

/**
 * How a node or edge relates to the hovered card. "cycle" is both upstream
 * and downstream, which only happens in a cycle with it; "focus" is the
 * hovered card itself, whatever cycles it is on.
 */
export type ChainRole = "focus" | "upstream" | "downstream" | "cycle" | "none";
export type EdgeChainRole = Exclude<ChainRole, "focus">;

/** Identifies an edge; the topology has at most one edge per blocker and blocked ticket. */
export function edgeKey(edge: Pick<GraphEdge, "from" | "to">): string {
  return `${edge.from}->${edge.to}`;
}

export function chainRole(chains: GraphChains, id: string): ChainRole {
  if (id === chains.id) return "focus";
  return role(chains.upstream.has(id), chains.downstream.has(id));
}

/**
 * Whether a card carries the "cycle" pill: it is lit as part of a cycle with
 * the hovered card. The pill is what tells that apart from the upstream chain
 * without relying on hue. The hovered card itself never carries one, and no
 * card does while nothing is lit.
 */
export function showsCyclePill(chains: GraphChains | null, id: string): boolean {
  return chains !== null && chainRole(chains, id) === "cycle";
}

export function edgeChainRole(chains: GraphChains, edge: Pick<GraphEdge, "from" | "to">): EdgeChainRole {
  const key = edgeKey(edge);
  return role(chains.upstreamEdges.has(key), chains.downstreamEdges.has(key));
}

function role(up: boolean, down: boolean): EdgeChainRole {
  if (up && down) return "cycle";
  if (up) return "upstream";
  if (down) return "downstream";
  return "none";
}

/**
 * Returns a lookup of the chains through a node of `topology`, or null for an
 * id that isn't a node. Adjacency is built once here; each call walks the
 * chains afresh and returns new sets.
 */
export function chainFinder<T extends GraphTicket>(
  topology: GraphTopology<T>,
): (id: string) => GraphChains | null {
  const ids = topology.nodes.map((n) => n.id);
  const indexOf = new Map<string, number>();
  ids.forEach((id, i) => indexOf.set(id, i));
  // Per node, the indices into topology.edges of its incoming and outgoing edges.
  const incoming: number[][] = ids.map(() => []);
  const outgoing: number[][] = ids.map(() => []);
  const tails = new Int32Array(topology.edges.length);
  const heads = new Int32Array(topology.edges.length);
  const keys = topology.edges.map(edgeKey);
  topology.edges.forEach((e, i) => {
    const u = indexOf.get(e.from)!;
    const v = indexOf.get(e.to)!;
    tails[i] = u;
    heads[i] = v;
    outgoing[u].push(i);
    incoming[v].push(i);
  });

  // Walks from `start` along `adjacent` edges, stepping to each edge's `far`
  // end. Every edge scanned is on the chain, since it touches `start` or a
  // node reached from it.
  const walk = (start: number, adjacent: number[][], far: Int32Array) => {
    const nodes = new Set<string>();
    const edges = new Set<string>();
    const seen = new Uint8Array(ids.length);
    seen[start] = 1;
    const queue = [start];
    for (let head = 0; head < queue.length; head++) {
      for (const e of adjacent[queue[head]]) {
        edges.add(keys[e]);
        const w = far[e];
        // The start is only added when an edge leads back to it.
        nodes.add(ids[w]);
        if (!seen[w]) {
          seen[w] = 1;
          queue.push(w);
        }
      }
    }
    return { nodes, edges };
  };

  return (id) => {
    const v = indexOf.get(id);
    if (v === undefined) return null;
    const up = walk(v, incoming, tails);
    const down = walk(v, outgoing, heads);
    return { id, upstream: up.nodes, downstream: down.nodes, upstreamEdges: up.edges, downstreamEdges: down.edges };
  };
}

/** The chains through one node, or null if it isn't one. For several queries, use chainFinder(). */
export function graphChains<T extends GraphTicket>(topology: GraphTopology<T>, id: string): GraphChains | null {
  return chainFinder(topology)(id);
}
