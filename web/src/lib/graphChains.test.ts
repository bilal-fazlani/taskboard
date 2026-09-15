import { describe, expect, it } from "vitest";
// Through graphLayout, which re-exports the chain API.
import {
  chainFinder,
  chainRole,
  computeGraphTopology,
  edgeChainRole,
  edgeKey,
  graphChains,
  type GraphChains,
  type GraphTicket,
  type GraphTopology,
} from "./graphLayout";
import { DEFAULT_STATUS, DONE_STATUS } from "./status";

// [key, dependsOn keys]. A key like "A-3" becomes prefix "A", number 3, and is
// also the ticket id. Every ticket is open unless listed in `done`.
type Spec = [key: string, deps?: string[]];

function topologyOf(specs: Spec[], done: string[] = []): GraphTopology {
  const input: GraphTicket[] = specs.map(([key, deps = []]) => {
    const [prefix, number] = key.split("-");
    return {
      id: key,
      projectPrefix: prefix,
      number: Number(number),
      status: done.includes(key) ? DONE_STATUS : DEFAULT_STATUS,
      dependsOn: deps.map((id) => ({ id, status: done.includes(id) ? DONE_STATUS : DEFAULT_STATUS })),
    };
  });
  return computeGraphTopology(input);
}

function chains(topology: GraphTopology, id: string): GraphChains {
  const found = graphChains(topology, id);
  if (!found) throw new Error(`fixture: ${id} is not a node`);
  return found;
}

const sorted = (values: Iterable<string>) => [...values].sort();

// Every node's role, as { role: [ids] }, leaving out "none".
function nodeRoles(topology: GraphTopology, c: GraphChains): Record<string, string[]> {
  const out: Record<string, string[]> = {};
  for (const n of topology.nodes) {
    const role = chainRole(c, n.id);
    if (role !== "none") (out[role] ??= []).push(n.id);
  }
  for (const ids of Object.values(out)) ids.sort();
  return out;
}

// Every edge's role, as { role: ["from->to"] }, leaving out "none".
function edgeRoles(topology: GraphTopology, c: GraphChains): Record<string, string[]> {
  const out: Record<string, string[]> = {};
  for (const e of topology.edges) {
    const role = edgeChainRole(c, e);
    if (role !== "none") (out[role] ??= []).push(edgeKey(e));
  }
  for (const keys of Object.values(out)) keys.sort();
  return out;
}

function backEdges(topology: GraphTopology): string[] {
  return topology.edges.filter((e) => e.back).map(edgeKey);
}

// Deterministic PRNG for the random fixtures.
function mulberry32(seed: number): () => number {
  let a = seed;
  return () => {
    a |= 0;
    a = (a + 0x6d2b79f5) | 0;
    let t = Math.imul(a ^ (a >>> 15), 1 | a);
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

function randomSpecs(count: number, random: () => number, maxDeps: number): Spec[] {
  const keys = Array.from({ length: count }, (_, i) => `P-${i + 1}`);
  return keys.map((key, i) => {
    const deps = new Set<string>();
    const n = Math.floor(random() * (maxDeps + 1));
    for (let d = 0; d < n; d++) {
      // Mostly earlier tickets, so chains are long; sometimes any ticket,
      // itself included, which makes cycles and self-dependencies.
      const j = random() < 0.8 ? Math.floor(random() * Math.max(i, 1)) : Math.floor(random() * count);
      deps.add(keys[j]);
    }
    return [key, [...deps]];
  });
}

describe("graphChains", () => {
  it("follows a linear chain both ways from the middle", () => {
    // A-1 blocks A-2 blocks A-3 blocks A-4.
    const topology = topologyOf([["A-1"], ["A-2", ["A-1"]], ["A-3", ["A-2"]], ["A-4", ["A-3"]]]);
    const c = chains(topology, "A-2");
    expect(sorted(c.upstream)).toEqual(["A-1"]);
    expect(sorted(c.downstream)).toEqual(["A-3", "A-4"]);
    expect(nodeRoles(topology, c)).toEqual({ focus: ["A-2"], upstream: ["A-1"], downstream: ["A-3", "A-4"] });
    expect(edgeRoles(topology, c)).toEqual({ upstream: ["A-1->A-2"], downstream: ["A-2->A-3", "A-3->A-4"] });

    const start = chains(topology, "A-1");
    expect(sorted(start.upstream)).toEqual([]);
    expect(sorted(start.downstream)).toEqual(["A-2", "A-3", "A-4"]);
    const end = chains(topology, "A-4");
    expect(sorted(end.upstream)).toEqual(["A-1", "A-2", "A-3"]);
    expect(sorted(end.downstream)).toEqual([]);
  });

  it("reaches upstream through both branches of a diamond", () => {
    // A-1 blocks A-2 and A-3, which both block A-4.
    const topology = topologyOf([["A-1"], ["A-2", ["A-1"]], ["A-3", ["A-1"]], ["A-4", ["A-2", "A-3"]]]);
    const bottom = chains(topology, "A-4");
    expect(nodeRoles(topology, bottom)).toEqual({ focus: ["A-4"], upstream: ["A-1", "A-2", "A-3"] });
    expect(edgeRoles(topology, bottom)).toEqual({
      upstream: ["A-1->A-2", "A-1->A-3", "A-2->A-4", "A-3->A-4"],
    });

    const top = chains(topology, "A-1");
    expect(nodeRoles(topology, top)).toEqual({ focus: ["A-1"], downstream: ["A-2", "A-3", "A-4"] });

    // A side branch sees only its own path; the other branch isn't on it.
    const side = chains(topology, "A-2");
    expect(nodeRoles(topology, side)).toEqual({ focus: ["A-2"], upstream: ["A-1"], downstream: ["A-4"] });
    expect(edgeRoles(topology, side)).toEqual({ upstream: ["A-1->A-2"], downstream: ["A-2->A-4"] });
  });

  it("leaves out unconnected tickets", () => {
    const topology = topologyOf([
      ["A-1"],
      ["A-2", ["A-1"]],
      ["A-3"],
      ["B-1"],
      ["B-2", ["B-1"]],
    ]);
    const c = chains(topology, "A-2");
    expect(nodeRoles(topology, c)).toEqual({ focus: ["A-2"], upstream: ["A-1"] });
    expect(edgeRoles(topology, c)).toEqual({ upstream: ["A-1->A-2"] });
    expect(chainRole(c, "A-3")).toBe("none");
    expect(chainRole(c, "B-1")).toBe("none");
    expect(edgeChainRole(c, { from: "B-1", to: "B-2" })).toBe("none");

    const alone = chains(topology, "A-3");
    expect(alone.upstream.size + alone.downstream.size).toBe(0);
    expect(alone.upstreamEdges.size + alone.downstreamEdges.size).toBe(0);
  });

  it("returns null for an id that isn't a node", () => {
    const topology = topologyOf([["A-1"], ["A-2", ["A-1"]]], ["A-1"]);
    expect(graphChains(topology, "A-1")).toBeNull();
    expect(graphChains(topology, "nope")).toBeNull();
    expect(graphChains(topology, "A-2")).not.toBeNull();
  });

  it("marks the other ticket of a 2-cycle as cycle, with both of its edges", () => {
    // A-1 and A-2 block each other; A-3 blocks A-1 and A-2 blocks A-4.
    const topology = topologyOf([["A-1", ["A-2", "A-3"]], ["A-2", ["A-1"]], ["A-3"], ["A-4", ["A-2"]]]);
    expect(backEdges(topology)).toHaveLength(1);
    const c = chains(topology, "A-1");
    expect(sorted(c.upstream)).toEqual(["A-1", "A-2", "A-3"]);
    expect(sorted(c.downstream)).toEqual(["A-1", "A-2", "A-4"]);
    expect(nodeRoles(topology, c)).toEqual({
      focus: ["A-1"],
      cycle: ["A-2"],
      upstream: ["A-3"],
      downstream: ["A-4"],
    });
    expect(edgeRoles(topology, c)).toEqual({
      cycle: ["A-1->A-2", "A-2->A-1"],
      upstream: ["A-3->A-1"],
      downstream: ["A-2->A-4"],
    });
  });

  it("marks a 3-cycle as cycle from inside it, and as one chain from outside it", () => {
    // A-1 -> A-2 -> A-3 -> A-1, fed by A-4, feeding A-5.
    const topology = topologyOf([
      ["A-1", ["A-3", "A-4"]],
      ["A-2", ["A-1"]],
      ["A-3", ["A-2"]],
      ["A-4"],
      ["A-5", ["A-3"]],
    ]);
    const [back] = backEdges(topology);
    expect(back).toBeDefined();

    const inside = chains(topology, "A-2");
    expect(nodeRoles(topology, inside)).toEqual({
      focus: ["A-2"],
      cycle: ["A-1", "A-3"],
      upstream: ["A-4"],
      downstream: ["A-5"],
    });
    expect(edgeRoles(topology, inside)).toEqual({
      cycle: ["A-1->A-2", "A-2->A-3", "A-3->A-1"],
      upstream: ["A-4->A-1"],
      downstream: ["A-3->A-5"],
    });

    // From the feeder, the whole cycle is downstream, its back edge included.
    const feeder = chains(topology, "A-4");
    expect(nodeRoles(topology, feeder)).toEqual({ focus: ["A-4"], downstream: ["A-1", "A-2", "A-3", "A-5"] });
    expect(edgeChainRole(feeder, topology.edges.find((e) => e.back)!)).toBe("downstream");
    expect(edgeRoles(topology, feeder)).toEqual({
      downstream: ["A-1->A-2", "A-2->A-3", "A-3->A-1", "A-3->A-5", "A-4->A-1"],
    });

    // From the end, it's all upstream.
    const end = chains(topology, "A-5");
    expect(nodeRoles(topology, end)).toEqual({ focus: ["A-5"], upstream: ["A-1", "A-2", "A-3", "A-4"] });
    expect(edgeRoles(topology, end).upstream).toHaveLength(5);
  });

  it("puts a self-dependency on both chains of its own ticket, and on one chain of others", () => {
    // A-2 depends on itself, sits between A-1 and A-3.
    const topology = topologyOf([["A-1"], ["A-2", ["A-1", "A-2"]], ["A-3", ["A-2"]]]);
    expect(backEdges(topology)).toEqual(["A-2->A-2"]);

    const self = chains(topology, "A-2");
    expect(sorted(self.upstream)).toEqual(["A-1", "A-2"]);
    expect(sorted(self.downstream)).toEqual(["A-2", "A-3"]);
    expect(nodeRoles(topology, self)).toEqual({ focus: ["A-2"], upstream: ["A-1"], downstream: ["A-3"] });
    expect(edgeRoles(topology, self)).toEqual({
      cycle: ["A-2->A-2"],
      upstream: ["A-1->A-2"],
      downstream: ["A-2->A-3"],
    });

    // A-2 is only upstream of A-3, so its loop is part of the upstream chain.
    const below = chains(topology, "A-3");
    expect(nodeRoles(topology, below)).toEqual({ focus: ["A-3"], upstream: ["A-1", "A-2"] });
    expect(edgeRoles(topology, below)).toEqual({ upstream: ["A-1->A-2", "A-2->A-2", "A-2->A-3"] });

    const above = chains(topology, "A-1");
    expect(edgeRoles(topology, above)).toEqual({ downstream: ["A-1->A-2", "A-2->A-2", "A-2->A-3"] });
  });

  it("keeps a feeding cycle on one chain and the hovered ticket's own cycle as cycle", () => {
    // A-1 and A-2 block each other; A-2 blocks A-3; A-3 and A-4 block each other.
    const topology = topologyOf([["A-1", ["A-2"]], ["A-2", ["A-1"]], ["A-3", ["A-2", "A-4"]], ["A-4", ["A-3"]]]);
    expect(backEdges(topology)).toHaveLength(2);

    const fed = chains(topology, "A-3");
    expect(nodeRoles(topology, fed)).toEqual({ focus: ["A-3"], cycle: ["A-4"], upstream: ["A-1", "A-2"] });
    expect(edgeRoles(topology, fed)).toEqual({
      cycle: ["A-3->A-4", "A-4->A-3"],
      upstream: ["A-1->A-2", "A-2->A-1", "A-2->A-3"],
    });

    const feeding = chains(topology, "A-1");
    expect(nodeRoles(topology, feeding)).toEqual({ focus: ["A-1"], cycle: ["A-2"], downstream: ["A-3", "A-4"] });
    expect(edgeRoles(topology, feeding)).toEqual({
      cycle: ["A-1->A-2", "A-2->A-1"],
      downstream: ["A-2->A-3", "A-3->A-4", "A-4->A-3"],
    });
  });

  it("leaves an edge that skips the hovered ticket off both chains", () => {
    // A-1 blocks A-2 blocks A-3, and A-1 also blocks A-3 directly.
    const topology = topologyOf([["A-1"], ["A-2", ["A-1"]], ["A-3", ["A-1", "A-2"]]]);
    const c = chains(topology, "A-2");
    expect(nodeRoles(topology, c)).toEqual({ focus: ["A-2"], upstream: ["A-1"], downstream: ["A-3"] });
    expect(edgeChainRole(c, { from: "A-1", to: "A-3" })).toBe("none");
  });

  it("answers a repeated query the same way, with sets of its own", () => {
    const topology = topologyOf([["A-1"], ["A-2", ["A-1"]], ["A-3", ["A-2"]]]);
    const find = chainFinder(topology);
    const first = find("A-2")!;
    const second = find("A-2")!;
    expect(second).toEqual(first);
    expect(second.upstream).not.toBe(first.upstream);
    // A query in between doesn't disturb either answer.
    find("A-3");
    expect(sorted(find("A-2")!.downstream)).toEqual(["A-3"]);
  });

  it("matches a naive reachability model on random graphs with cycles", () => {
    const random = mulberry32(7);
    for (let round = 0; round < 150; round++) {
      const count = 1 + Math.floor(random() * 12);
      const topology = topologyOf(randomSpecs(count, random, 3));
      const ids = topology.nodes.map((n) => n.id);
      // reach[a] = every node with a path of one or more edges from a.
      const reach = new Map<string, Set<string>>();
      for (const a of ids) {
        const found = new Set<string>();
        let frontier = [a];
        while (frontier.length > 0) {
          const next: string[] = [];
          for (const u of frontier) {
            for (const e of topology.edges) {
              if (e.from === u && !found.has(e.to)) {
                found.add(e.to);
                next.push(e.to);
              }
            }
          }
          frontier = next;
        }
        reach.set(a, found);
      }

      const find = chainFinder(topology);
      for (const h of ids) {
        const c = find(h)!;
        const up = ids.filter((u) => reach.get(u)!.has(h));
        const down = [...reach.get(h)!];
        expect(sorted(c.upstream)).toEqual(sorted(up));
        expect(sorted(c.downstream)).toEqual(sorted(down));

        // Edge sets follow from the node sets.
        const upSet = new Set(c.upstream);
        const downSet = new Set(c.downstream);
        const upEdges = topology.edges.filter((e) => e.to === h || upSet.has(e.to)).map(edgeKey);
        const downEdges = topology.edges.filter((e) => e.from === h || downSet.has(e.from)).map(edgeKey);
        expect(sorted(c.upstreamEdges)).toEqual(sorted(upEdges));
        expect(sorted(c.downstreamEdges)).toEqual(sorted(downEdges));

        for (const e of topology.edges) {
          const role = edgeChainRole(c, e);
          const from = chainRole(c, e.from);
          const to = chainRole(c, e.to);
          // Both ends of a lit edge are lit.
          if (role !== "none") {
            expect(from).not.toBe("none");
            expect(to).not.toBe("none");
          }
          // An edge on a path into or out of h is lit, back edges and
          // self-dependencies included.
          const onPathIn = (e.from === h || upSet.has(e.from)) && (e.to === h || upSet.has(e.to));
          const onPathOut = (e.from === h || downSet.has(e.from)) && (e.to === h || downSet.has(e.to));
          if (onPathIn || onPathOut) expect(role).not.toBe("none");
          // Cycle edges are exactly those with both ends in a cycle with h.
          const inCycle = (id: string) => upSet.has(id) && downSet.has(id);
          expect(role === "cycle").toBe(inCycle(e.from) && inCycle(e.to));
        }
        // Cycle nodes are exactly those that reach h and that h reaches.
        for (const id of ids) {
          if (id === h) continue;
          expect(chainRole(c, id) === "cycle").toBe(reach.get(id)!.has(h) && reach.get(h)!.has(id));
        }
      }
    }
  });

  it("answers for every node of a 500-ticket graph with cycles quickly", () => {
    const random = mulberry32(42);
    const specs = randomSpecs(500, random, 3);
    // Guarantee some cycles on top of any random ones.
    specs[10][1]!.push("P-12");
    specs[11][1]!.push("P-11");
    specs[99][1]!.push("P-100");
    specs[100][1]!.push("P-99");
    const topology = topologyOf(specs);
    expect(topology.nodes).toHaveLength(500);
    expect(backEdges(topology).length).toBeGreaterThan(0);

    const started = performance.now();
    const find = chainFinder(topology);
    let total = 0;
    for (const n of topology.nodes) {
      const c = find(n.id)!;
      total += c.upstream.size + c.downstream.size;
    }
    // And again, as the pointer passes back over the same cards; nothing is
    // cached, so this is a second full pass.
    for (const n of topology.nodes) find(n.id);
    const elapsed = performance.now() - started;
    expect(total).toBeGreaterThan(500);
    expect(elapsed).toBeLessThan(2000);
  });

  it("walks a 20,000-ticket cycle without overflowing the stack", () => {
    const count = 20_000;
    const specs: Spec[] = Array.from({ length: count }, (_, i) => [`A-${i + 1}`, [`A-${i === 0 ? count : i}`]]);
    const topology = topologyOf(specs);
    const c = chains(topology, "A-500");
    expect(c.upstream.size).toBe(count);
    expect(c.downstream.size).toBe(count);
    expect(c.upstreamEdges.size).toBe(count);
    expect(chainRole(c, "A-1")).toBe("cycle");
    expect(edgeChainRole(c, { from: `A-${count}`, to: "A-1" })).toBe("cycle");
  });
});
