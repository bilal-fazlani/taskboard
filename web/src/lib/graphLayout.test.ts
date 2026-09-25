import { describe, expect, it } from "vitest";
import {
  DEFAULT_COLUMN_GAP,
  DEFAULT_GRID_GAP,
  DEFAULT_NODE_SIZE,
  DEFAULT_ROW_GAP,
  computeGraphTopology,
  layoutGraph,
  matchingBounds,
  positionGraph,
  type GraphLayout,
  type GraphTicket,
  type GraphTopology,
  type LayerEntry,
  type PositionedNode,
} from "./graphLayout";
import { isDone } from "./status";

// [key, status, dependsOn keys]. A key like "A-3" becomes prefix "A", number
// 3, and is also the ticket id. A dependency's status is read from the other
// specs, or from `outside` for tickets not in the input.
type Spec = [key: string, status: string, deps?: string[]];

function tickets(specs: Spec[], outside: Record<string, string> = {}): GraphTicket[] {
  const statusOf = new Map<string, string>(Object.entries(outside));
  for (const [key, status] of specs) statusOf.set(key, status);
  return specs.map(([key, status, deps = []]) => {
    const [prefix, number] = key.split("-");
    return {
      id: key,
      projectPrefix: prefix,
      number: Number(number),
      status,
      dependsOn: deps.map((id) => {
        const depStatus = statusOf.get(id);
        if (depStatus === undefined) throw new Error(`fixture: no status for ${id}`);
        return { id, status: depStatus };
      }),
    };
  });
}

function columnOf(topology: GraphTopology): Record<string, number> {
  return Object.fromEntries(topology.nodes.map((n) => [n.id, n.column]));
}

function edgeList(topology: GraphTopology): string[] {
  return topology.edges.map((e) => `${e.from}->${e.to}${e.back ? " back" : ""}`);
}

// Crossings in the layered graph: pairs of stretches between the same two
// neighbouring columns whose ends are in opposite orders. A long edge's
// stretches run through its waypoints.
function crossingsOf(topology: GraphTopology): number {
  const row = new Map<string, number>();
  const column = new Map<string, number>();
  const key = (entry: LayerEntry, c: number) => (entry.kind === "card" ? entry.id : `${entry.edge}@${c}`);
  topology.layers.forEach((layer, c) =>
    layer.forEach((entry, i) => {
      row.set(key(entry, c), i);
      column.set(key(entry, c), c);
    }),
  );
  const stretches: [string, string][] = [];
  topology.edges.forEach((edge, e) => {
    if (edge.back) return;
    const chain = [edge.from];
    for (let c = column.get(edge.from)! + 1; c < column.get(edge.to)!; c++) chain.push(`${e}@${c}`);
    chain.push(edge.to);
    for (let i = 1; i < chain.length; i++) stretches.push([chain[i - 1], chain[i]]);
  });
  let crossings = 0;
  stretches.forEach(([a, b], i) =>
    stretches.slice(i + 1).forEach(([c, d]) => {
      if (column.get(a) !== column.get(c)) return;
      if ((row.get(a)! - row.get(c)!) * (row.get(b)! - row.get(d)!) < 0) crossings++;
    }),
  );
  return crossings;
}

// Deterministic PRNG so the shuffle and performance fixtures are reproducible.
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

function shuffled<T>(items: readonly T[], random: () => number): T[] {
  const out = [...items];
  for (let i = out.length - 1; i > 0; i--) {
    const j = Math.floor(random() * (i + 1));
    [out[i], out[j]] = [out[j], out[i]];
  }
  return out;
}

// An independent model of the graph for the property test, kept deliberately
// naive: strongly connected components from plain reachability, and a
// recursive search. Only for small graphs with one project prefix.
function referenceModel(input: GraphTicket[]) {
  const inputIds = new Set(input.map((t) => t.id));
  const open = input.filter((t) => !isDone(t.status)).sort((a, b) => a.number - b.number);
  const ids = open.map((t) => t.id);
  const indexOf = new Map(ids.map((id, i) => [id, i]));
  const refs = open.map((t) => [...new Map((t.dependsOn ?? []).map((r) => [r.id, r])).values()]);
  const blockers = refs.map((rs) => rs.filter((r) => indexOf.has(r.id)).map((r) => indexOf.get(r.id)!));
  const external = refs.map((rs) => rs.filter((r) => !inputIds.has(r.id) && !isDone(r.status)).length);
  const successors: number[][] = ids.map(() => []);
  blockers.forEach((us, v) => us.forEach((u) => successors[u].push(v)));
  successors.forEach((vs) => vs.sort((a, b) => a - b));

  const reaches = ids.map((_, start) => {
    const seen = new Set([start]);
    const todo = [start];
    while (todo.length > 0) {
      for (const v of successors[todo.pop()!]) {
        if (!seen.has(v)) {
          seen.add(v);
          todo.push(v);
        }
      }
    }
    return seen;
  });
  // Ascending, and always includes v.
  const componentOf = (v: number) => ids.map((_, u) => u).filter((u) => reaches[v].has(u) && reaches[u].has(v));

  const referenceBackEdges = () => {
    const state = ids.map(() => 0);
    const found: string[] = [];
    const visit = (u: number) => {
      state[u] = 1;
      for (const v of successors[u]) {
        if (state[v] === 1) found.push(`${ids[u]}->${ids[v]}`);
        else if (state[v] === 0) visit(v);
      }
      state[u] = 2;
    };
    ids.forEach((_, v) => {
      const component = componentOf(v);
      const isSource = component.every((w) => blockers[w].every((u) => component.includes(u)));
      if (component[0] !== v || !isSource) return;
      visit(component.find((w) => external[w] > 0) ?? v);
    });
    if (state.some((s) => s !== 2)) throw new Error("reference search left tickets unvisited");
    return found;
  };

  return { ids, refs, blockers, external, componentOf, referenceBackEdges };
}

describe("computeGraphTopology", () => {
  it("returns an empty graph for no tickets", () => {
    expect(computeGraphTopology([])).toEqual({ nodes: [], edges: [], columns: [], layers: [], grid: [], columnCounts: [] });
  });

  it("puts each step of a linear chain in its own column", () => {
    const topology = computeGraphTopology(
      tickets([
        ["A-1", "todo"],
        ["A-2", "todo", ["A-1"]],
        ["A-3", "todo", ["A-2"]],
      ]),
    );
    expect(topology.columns).toEqual([["A-1"], ["A-2"], ["A-3"]]);
    expect(topology.columnCounts).toEqual([1, 1, 1]);
    expect(edgeList(topology)).toEqual(["A-1->A-2", "A-2->A-3"]);
  });

  it("places a diamond's join by its longest path", () => {
    // A-4 depends on A-2 and A-3, which both depend on A-1; A-5 depends on A-1
    // and A-4, so the longer route wins.
    const topology = computeGraphTopology(
      tickets([
        ["A-1", "todo"],
        ["A-2", "todo", ["A-1"]],
        ["A-3", "todo", ["A-1"]],
        ["A-4", "todo", ["A-2", "A-3"]],
        ["A-5", "todo", ["A-1", "A-4"]],
      ]),
    );
    expect(columnOf(topology)).toEqual({ "A-1": 0, "A-2": 1, "A-3": 1, "A-4": 2, "A-5": 3 });
    expect(topology.columns).toEqual([["A-1"], ["A-2", "A-3"], ["A-4"], ["A-5"]]);
    expect(topology.edges.every((e) => !e.back)).toBe(true);
  });

  it("puts unconnected Ready tickets in the grid, in ticket order", () => {
    const topology = computeGraphTopology(
      tickets([
        ["B-2", "todo"],
        ["A-10", "todo"],
        ["A-9", "todo"],
      ]),
    );
    // Number, not string, order within a prefix.
    expect(topology.grid).toEqual(["A-9", "A-10", "B-2"]);
    expect(topology.columns).toEqual([[]]);
    // Ready still counts them.
    expect(topology.columnCounts).toEqual([3]);
    expect(topology.edges).toEqual([]);
    expect(topology.nodes.map((n) => [n.id, n.column, n.row, n.inGrid])).toEqual([
      ["A-9", 0, 0, true],
      ["A-10", 0, 1, true],
      ["B-2", 0, 2, true],
    ]);
  });

  it("drops done tickets and counts dependencies on them as satisfied", () => {
    const topology = computeGraphTopology(
      tickets(
        [
          ["A-1", "done"],
          ["A-2", "done"],
          ["A-3", "todo", ["A-1", "A-2", "X-9"]],
          ["A-4", "todo", ["A-3", "A-1"]],
        ],
        { "X-9": "done" },
      ),
    );
    expect(topology.columns).toEqual([["A-3"], ["A-4"]]);
    expect(edgeList(topology)).toEqual(["A-3->A-4"]);
    const byId = Object.fromEntries(topology.nodes.map((n) => [n.id, n]));
    // Done in the input, and done outside it (X-9), both count.
    expect(byId["A-3"].satisfiedDependencyCount).toBe(3);
    expect(byId["A-4"].satisfiedDependencyCount).toBe(1);
    expect(byId["A-3"].externalBlockerCount).toBe(0);
    expect(byId["A-3"].dependencyTotal).toBe(3);
    expect(byId["A-4"].dependencyTotal).toBe(2);
  });

  it("trusts an input ticket's own status over a stale ref", () => {
    const [done, dependent] = tickets([
      ["A-1", "done"],
      ["A-2", "todo", ["A-1"]],
    ]);
    const stale = { ...dependent, dependsOn: [{ id: "A-1", status: "todo" }] };
    const topology = computeGraphTopology([done, stale]);
    // No edge, so A-2 is in the grid rather than the column.
    expect(topology.grid).toEqual(["A-2"]);
    expect(topology.nodes[0].satisfiedDependencyCount).toBe(1);
    expect(topology.nodes[0].externalBlockerCount).toBe(0);
    expect(topology.nodes[0].dependencyTotal).toBe(1);
  });

  it("counts a repeated dependency once", () => {
    const [a, b] = tickets([
      ["A-1", "todo"],
      ["A-2", "todo", ["A-1", "A-1"]],
    ]);
    const topology = computeGraphTopology([a, b]);
    expect(edgeList(topology)).toEqual(["A-1->A-2"]);
    expect(topology.nodes.find((n) => n.id === "A-2")!.dependencyTotal).toBe(1);
  });

  it("holds a ticket with an unfinished dependency outside the input out of Ready", () => {
    const topology = computeGraphTopology(
      tickets(
        [
          ["A-1", "todo"],
          ["A-2", "todo", ["X-1", "X-2", "X-3"]],
          ["A-3", "todo", ["A-2"]],
          ["A-4", "todo", ["A-1", "X-1"]],
        ],
        { "X-1": "in_progress", "X-2": "todo", "X-3": "done" },
      ),
    );
    const byId = Object.fromEntries(topology.nodes.map((n) => [n.id, n]));
    expect(byId["A-2"].externalBlockerCount).toBe(2);
    expect(byId["A-2"].satisfiedDependencyCount).toBe(1);
    expect(byId["A-4"].externalBlockerCount).toBe(1);
    // No edge to the missing ticket, but one step right of Ready, and that
    // step carries on to its dependents. An in-set path of one step and an
    // external blocker don't add up.
    expect(edgeList(topology)).toEqual(["A-1->A-4", "A-2->A-3"]);
    expect(columnOf(topology)).toEqual({ "A-1": 0, "A-2": 1, "A-3": 2, "A-4": 1 });
    // A-2: X-1, X-2, X-3, all distinct. A-4: A-1 and X-1.
    expect(byId["A-2"].dependencyTotal).toBe(3);
    expect(byId["A-4"].dependencyTotal).toBe(2);
  });

  it("totals every distinct dependency once, across done, edge, back-edge and hidden kinds", () => {
    const topology = computeGraphTopology(
      tickets(
        [
          ["A-1", "done"],
          ["A-2", "todo", ["A-3"]],
          ["A-3", "todo", ["A-2", "A-1", "A-1", "X-1", "X-2"]],
        ],
        { "X-1": "todo", "X-2": "done" },
      ),
    );
    const byId = Object.fromEntries(topology.nodes.map((n) => [n.id, n]));
    // A-3 depends on: A-2 (a back edge, since A-2 and A-3 form a cycle), A-1
    // twice (done, deduped to one), X-1 (hidden: unfinished and off the page)
    // and X-2 (done outside the input): four distinct dependencies.
    expect(edgeList(topology)).toEqual(["A-2->A-3 back", "A-3->A-2"]);
    expect(byId["A-3"].satisfiedDependencyCount).toBe(2);
    expect(byId["A-3"].externalBlockerCount).toBe(1);
    expect(byId["A-3"].dependencyTotal).toBe(4);
    // A-2's only dependency is its edge to A-3.
    expect(byId["A-2"].dependencyTotal).toBe(1);
  });

  it("leaves Ready empty when every otherwise-ready ticket has an external blocker", () => {
    const topology = computeGraphTopology(tickets([["A-1", "todo", ["X-1"]]], { "X-1": "todo" }));
    expect(topology.columns).toEqual([[], ["A-1"]]);
    expect(topology.columnCounts).toEqual([0, 1]);
  });

  it("flags a self-dependency as a back edge and keeps the ticket in Ready", () => {
    const topology = computeGraphTopology(
      tickets([
        ["A-1", "todo", ["A-1"]],
        ["A-2", "todo", ["A-1"]],
      ]),
    );
    expect(edgeList(topology)).toEqual(["A-1->A-1 back", "A-1->A-2"]);
    expect(topology.columns).toEqual([["A-1"], ["A-2"]]);
  });

  it("breaks a 2-cycle with no way in at the edge back to the lowest ticket", () => {
    const topology = computeGraphTopology(
      tickets([
        ["A-2", "todo", ["A-1"]],
        ["A-1", "todo", ["A-2"]],
      ]),
    );
    expect(edgeList(topology)).toEqual(["A-1->A-2", "A-2->A-1 back"]);
    expect(topology.columns).toEqual([["A-1"], ["A-2"]]);
  });

  it("breaks a 3-cycle with no way in, giving every ticket a column", () => {
    const topology = computeGraphTopology(
      tickets([
        ["A-1", "todo", ["A-3"]],
        ["A-2", "todo", ["A-1"]],
        ["A-3", "todo", ["A-2"]],
      ]),
    );
    expect(edgeList(topology)).toEqual(["A-1->A-2", "A-2->A-3", "A-3->A-1 back"]);
    expect(columnOf(topology)).toEqual({ "A-1": 0, "A-2": 1, "A-3": 2 });
  });

  it("breaks a 3-cycle entered from outside at the edge back to its entry", () => {
    // A-5 is Ready and A-3 depends on it. A-3 -> A-4 -> A-2 -> A-3 is the cycle;
    // the lowest ticket in it (A-2) must not decide where it breaks.
    const topology = computeGraphTopology(
      tickets([
        ["A-2", "todo", ["A-4"]],
        ["A-3", "todo", ["A-5", "A-2"]],
        ["A-4", "todo", ["A-3"]],
        ["A-5", "todo"],
      ]),
    );
    expect(edgeList(topology)).toEqual(["A-2->A-3 back", "A-3->A-4", "A-4->A-2", "A-5->A-3"]);
    expect(columnOf(topology)).toEqual({ "A-5": 0, "A-3": 1, "A-4": 2, "A-2": 3 });
  });

  it("only flags edges that close a cycle", () => {
    // Two cycles sharing A-2, plus a chain hanging off it.
    const topology = computeGraphTopology(
      tickets([
        ["A-1", "todo", ["A-2"]],
        ["A-2", "todo", ["A-1", "A-3"]],
        ["A-3", "todo", ["A-2"]],
        ["A-4", "todo", ["A-3"]],
      ]),
    );
    expect(edgeList(topology)).toEqual(["A-1->A-2", "A-2->A-1 back", "A-2->A-3", "A-3->A-2 back", "A-3->A-4"]);
    expect(columnOf(topology)).toEqual({ "A-1": 0, "A-2": 1, "A-3": 2, "A-4": 3 });
  });

  it("starts a cycle's search where another cycle enters it, not at its lowest ticket", () => {
    // {A-1, A-2} is a cycle entered at A-2 from the cycle {A-5, A-6}, which
    // nothing enters. A-1 waits on A-2, which waits on A-6, so A-1 is three
    // steps from Ready, not in it.
    const topology = computeGraphTopology(
      tickets([
        ["A-1", "todo", ["A-2"]],
        ["A-2", "todo", ["A-1", "A-6"]],
        ["A-5", "todo", ["A-6"]],
        ["A-6", "todo", ["A-5"]],
      ]),
    );
    expect(edgeList(topology)).toEqual(["A-1->A-2 back", "A-2->A-1", "A-5->A-6", "A-6->A-2", "A-6->A-5 back"]);
    expect(topology.columns).toEqual([["A-5"], ["A-6"], ["A-2"], ["A-1"]]);
  });

  it("starts a cycle waiting on an external blocker at the ticket that has it", () => {
    // The whole cycle waits on X-1, which isn't in the input, so none of it
    // is Ready: A-2 holds the external blocker and A-1 waits on A-2.
    const topology = computeGraphTopology(
      tickets(
        [
          ["A-1", "todo", ["A-2"]],
          ["A-2", "todo", ["A-1", "X-1"]],
        ],
        { "X-1": "todo" },
      ),
    );
    expect(edgeList(topology)).toEqual(["A-1->A-2 back", "A-2->A-1"]);
    expect(topology.columns).toEqual([[], ["A-2"], ["A-1"]]);
  });

  it("doesn't let a self-dependency stop a ticket starting the search", () => {
    // A-2's only incoming edge is from itself, so nothing else blocks it and
    // the search starts there. The cycle {A-1, A-3} is entered at A-3 from
    // A-2, so it breaks at the edge back to A-3, not at its lowest ticket.
    const topology = computeGraphTopology(
      tickets([
        ["A-1", "todo", ["A-3"]],
        ["A-2", "todo", ["A-2"]],
        ["A-3", "todo", ["A-2", "A-1"]],
      ]),
    );
    expect(edgeList(topology)).toEqual(["A-1->A-3 back", "A-2->A-2 back", "A-2->A-3", "A-3->A-1"]);
    expect(columnOf(topology)).toEqual({ "A-2": 0, "A-3": 1, "A-1": 2 });
  });

  it("lays out a 20,000-ticket cycle without overflowing the stack", () => {
    const count = 20_000;
    const specs: Spec[] = Array.from({ length: count }, (_, i) => [`A-${i + 1}`, "todo", [`A-${i === 0 ? count : i}`]]);
    const layout = layoutGraph(tickets(specs));
    expect(layout.edges.filter((e) => e.back).map((e) => `${e.from}->${e.to}`)).toEqual([`A-${count}->A-1`]);
    expect(layout.columns).toHaveLength(count);
    expect(layout.nodes[count - 1]).toMatchObject({ id: `A-${count}`, column: count - 1 });
  });

  it("orders a column by its blockers' rows to avoid crossings", () => {
    // By ticket order column 1 would be A-3, A-4, crossing the arrows from
    // A-1 and A-2.
    const topology = computeGraphTopology(
      tickets([
        ["A-1", "todo"],
        ["A-2", "todo"],
        ["A-3", "todo", ["A-2"]],
        ["A-4", "todo", ["A-1"]],
      ]),
    );
    expect(topology.columns).toEqual([
      ["A-1", "A-2"],
      ["A-4", "A-3"],
    ]);
  });

  it("reorders a column by the rows of the tickets it blocks", () => {
    // A-4 depends on A-1 and A-3, A-5 on A-2. Ordering column 1 by its
    // blockers can't uncross A-2 -> A-5 and A-3 -> A-4; only moving A-3 up
    // in Ready, towards A-4, does.
    const topology = computeGraphTopology(
      tickets([
        ["A-1", "todo"],
        ["A-2", "todo"],
        ["A-3", "todo"],
        ["A-4", "todo", ["A-1", "A-3"]],
        ["A-5", "todo", ["A-2"]],
      ]),
    );
    expect(topology.columns).toEqual([
      ["A-1", "A-3", "A-2"],
      ["A-4", "A-5"],
    ]);
  });

  it("sorts active tickets to the top of Ready", () => {
    const topology = computeGraphTopology(
      tickets([
        ["A-1", "todo"],
        ["A-2", "in_progress"],
        ["A-3", "todo"],
        ["A-4", "in_progress"],
      ]),
    );
    // Held tickets stay in the column though nothing links them; the rest
    // go to the grid.
    expect(topology.columns).toEqual([["A-2", "A-4"]]);
    expect(topology.grid).toEqual(["A-1", "A-3"]);
    expect(topology.nodes.filter((n) => n.active).map((n) => n.id)).toEqual(["A-2", "A-4"]);
  });

  it("mingles agent_review with in_progress at the top of Ready, in ticket order", () => {
    // A ticket bouncing between the implementer and the reviewer must not
    // change rows as it flips, so both statuses are one group and ticket
    // order decides within it.
    const topology = computeGraphTopology(
      tickets([
        ["A-1", "todo"],
        ["A-2", "agent_review"],
        ["A-3", "in_progress"],
        ["A-4", "todo"],
        ["A-5", "agent_review"],
      ]),
    );
    expect(topology.columns).toEqual([["A-2", "A-3", "A-5"]]);
    expect(topology.grid).toEqual(["A-1", "A-4"]);
    expect(topology.nodes.filter((n) => n.active).map((n) => n.id)).toEqual(["A-2", "A-3", "A-5"]);
  });

  it("leaves the rows unchanged when a ticket flips between in_progress and agent_review", () => {
    const rows = (status: string) => {
      const topology = computeGraphTopology(
        tickets([
          ["A-1", "todo"],
          ["A-2", "in_progress"],
          ["A-3", status],
          ["A-4", "todo"],
        ]),
      );
      return topology.columns;
    };
    expect(rows("agent_review")).toEqual(rows("in_progress"));
  });

  it("keeps active tickets on top even where crossings would put a todo above them", () => {
    // By ticket order A-1's component (A-1, A-3) would lead both columns,
    // and A-1 would sit above A-2 to meet A-3 without a crossing. The
    // active-first rule holds A-2 at the top instead, and its component
    // (A-2, A-4, A-5) leads, so nothing crosses after all.
    const topology = computeGraphTopology(
      tickets([
        ["A-1", "todo"],
        ["A-2", "agent_review"],
        ["A-3", "todo", ["A-1"]],
        ["A-4", "todo", ["A-2", "A-5"]],
        ["A-5", "todo"],
      ]),
    );
    expect(topology.columns).toEqual([
      ["A-2", "A-5", "A-1"],
      ["A-4", "A-3"],
    ]);
    expect(crossingsOf(topology)).toBe(0);
  });

  it("keeps held tickets at the top of Ready, linked or not, and their components first", () => {
    // A-2 is held and links nothing; A-3 is held and blocks A-5. Both stay in
    // the column, above A-1, and A-3's component leads column 1 so its arrow
    // doesn't cross A-1's.
    const topology = computeGraphTopology(
      tickets([
        ["A-1", "todo"],
        ["A-2", "in_progress"],
        ["A-3", "agent_review"],
        ["A-4", "todo", ["A-1"]],
        ["A-5", "todo", ["A-3"]],
        ["A-6", "todo"],
      ]),
    );
    expect(topology.columns).toEqual([
      ["A-2", "A-3", "A-1"],
      ["A-5", "A-4"],
    ]);
    expect(topology.grid).toEqual(["A-6"]);
    expect(topology.columnCounts).toEqual([4, 2]);
    expect(crossingsOf(topology)).toBe(0);
  });

  it("sits a ticket whose only blockers are hidden at the bottom of its column", () => {
    const topology = computeGraphTopology(
      tickets(
        [
          ["A-1", "todo"],
          ["A-2", "todo", ["A-1"]],
          ["A-3", "todo", ["X-1"]],
          ["A-4", "todo", ["A-1"]],
        ],
        { "X-1": "todo" },
      ),
    );
    expect(topology.columns).toEqual([["A-1"], ["A-2", "A-4", "A-3"]]);
    expect(topology.grid).toEqual([]);
  });

  it("moves a ticket from the grid into the graph when it gains a link, and back when it loses it", () => {
    const loose = tickets([
      ["A-1", "todo"],
      ["A-2", "todo"],
    ]);
    const linked = tickets([
      ["A-1", "todo"],
      ["A-2", "todo", ["A-1"]],
    ]);
    expect(computeGraphTopology(loose).grid).toEqual(["A-1", "A-2"]);
    expect(computeGraphTopology(linked).grid).toEqual([]);
    expect(computeGraphTopology(linked).columns).toEqual([["A-1"], ["A-2"]]);
    // Only an edge links: a done blocker is not one.
    expect(computeGraphTopology(tickets([["A-1", "todo", ["A-9"]]], { "A-9": "done" })).grid).toEqual(["A-1"]);
    // A self-dependency is an edge, drawn as a loop on the card, so it stays.
    expect(computeGraphTopology(tickets([["A-1", "todo", ["A-1"]]])).columns).toEqual([["A-1"]]);
  });

  it("gives a forward edge spanning several columns a waypoint in each column it crosses", () => {
    const topology = computeGraphTopology(
      tickets([
        ["A-1", "todo"],
        ["A-2", "todo", ["A-1"]],
        ["A-3", "todo", ["A-2"]],
        ["A-4", "todo", ["A-3", "A-1", "A-5"]],
        ["A-5", "todo", ["A-4"]],
      ]),
    );
    expect(edgeList(topology)).toEqual(["A-1->A-2", "A-1->A-4", "A-2->A-3", "A-3->A-4", "A-4->A-5", "A-5->A-4 back"]);
    const long = 1;
    expect(topology.layers).toEqual([
      [{ kind: "card", id: "A-1" }],
      [{ kind: "waypoint", edge: long }, { kind: "card", id: "A-2" }],
      [{ kind: "waypoint", edge: long }, { kind: "card", id: "A-3" }],
      [{ kind: "card", id: "A-4" }],
      [{ kind: "card", id: "A-5" }],
    ]);
    // Waypoints are not cards; the back edge gets none.
    expect(topology.columns).toEqual([["A-1"], ["A-2"], ["A-3"], ["A-4"], ["A-5"]]);
    expect(topology.nodes.map((n) => n.row)).toEqual([0, 0, 0, 0, 0]);
  });

  it("orders long edges' waypoints with the cards to avoid crossings", () => {
    // A-1 -> A-5 crosses column 1, where A-3 and A-4 sit. A-2 feeds A-4 and
    // A-1 feeds A-3, so the waypoint belongs above A-3 and nothing crosses.
    const topology = computeGraphTopology(
      tickets([
        ["A-1", "todo"],
        ["A-2", "todo"],
        ["A-3", "todo", ["A-1"]],
        ["A-4", "todo", ["A-2"]],
        ["A-5", "todo", ["A-1", "A-3", "A-4"]],
      ]),
    );
    expect(topology.layers[1].map((e) => (e.kind === "card" ? e.id : "waypoint"))).toEqual(["waypoint", "A-3", "A-4"]);
    expect(crossingsOf(topology)).toBe(0);
  });

  it("keeps the row of an entry with no neighbours on the side being sorted, never comparing it with a barycentre", () => {
    // B-2 has only a hidden blocker, so nothing left of it; it is seeded
    // second in column 1. A-1 to A-4 feed D-1, A-5 feeds B-1 and A-6 B-3,
    // and all four feed C-1, which makes them one component. Sorting by
    // blockers puts D-1 (rows 0-3 of Ready) above B-1 (row 4) and B-3 (row
    // 5); B-2 keeps row 1. Treating its row as a barycentre would have put
    // it at the top, on a par with Ready's row 1.
    const topology = computeGraphTopology(
      tickets(
        [
          ["A-1", "todo"],
          ["A-2", "todo"],
          ["A-3", "todo"],
          ["A-4", "todo"],
          ["A-5", "todo"],
          ["A-6", "todo"],
          ["B-1", "todo", ["A-5"]],
          ["B-2", "todo", ["X-1"]],
          ["B-3", "todo", ["A-6"]],
          ["C-1", "todo", ["B-1", "B-2", "B-3", "D-1"]],
          ["D-1", "todo", ["A-1", "A-2", "A-3", "A-4"]],
        ],
        { "X-1": "todo" },
      ),
    );
    expect(topology.columns[1]).toEqual(["D-1", "B-2", "B-1", "B-3"]);
    expect(crossingsOf(topology)).toBe(0);
  });

  it("sweeps on while the crossings drop", () => {
    // One sweep down and up leaves two crossings here; the next removes them.
    const topology = computeGraphTopology(
      tickets([
        ["A-1", "todo"],
        ["A-2", "todo"],
        ["A-3", "todo"],
        ["A-4", "todo", ["A-2"]],
        ["A-5", "todo", ["A-2"]],
        ["A-6", "todo", ["A-3", "A-1"]],
        ["A-7", "todo", ["A-2", "A-1"]],
        ["A-8", "todo", ["A-4", "A-5"]],
        ["A-9", "todo", ["A-6"]],
        ["A-10", "todo", ["A-7"]],
      ]),
    );
    expect(crossingsOf(topology)).toBe(0);
    expect(topology.columns).toEqual([
      ["A-2", "A-1", "A-3"],
      ["A-4", "A-5", "A-7", "A-6"],
      ["A-8", "A-10", "A-9"],
    ]);
  });

  it("finds the fewest crossings where two fixed sweeps stopped short", () => {
    // Two sweeps, which is all the layout used to run, left two crossings.
    const specs: Spec[] = [
      ["A-1", "todo"],
      ["A-2", "todo"],
      ["A-3", "todo", ["A-2"]],
      ["A-4", "todo", ["A-2"]],
      ["A-5", "todo", ["A-1"]],
      ["A-6", "todo", ["A-3", "A-5"]],
      ["A-7", "todo", ["A-3"]],
      ["A-8", "todo", ["A-5", "A-4"]],
    ];
    const topology = computeGraphTopology(tickets(specs));
    // Every ordering of the three columns, 2! * 3! * 3! of them.
    const permutations = (ids: string[]): string[][] =>
      ids.length <= 1 ? [ids] : ids.flatMap((id, i) => permutations([...ids.slice(0, i), ...ids.slice(i + 1)]).map((p) => [id, ...p]));
    let fewest = Infinity;
    for (const c0 of permutations(topology.columns[0]))
      for (const c1 of permutations(topology.columns[1]))
        for (const c2 of permutations(topology.columns[2])) {
          const layers = [c0, c1, c2].map((ids) => ids.map((id): LayerEntry => ({ kind: "card", id })));
          fewest = Math.min(fewest, crossingsOf({ ...topology, layers }));
        }
    expect(fewest).toBe(1);
    expect(crossingsOf(topology)).toBe(1);
  });

  it("leaves no two neighbouring entries that a swap would uncross, across random graphs", () => {
    const random = mulberry32(92);
    let swapsTried = 0;
    for (let graph = 0; graph < 300; graph++) {
      const size = 4 + Math.floor(random() * 10);
      const keys = Array.from({ length: size }, (_, i) => `A-${i + 1}`);
      const specs: Spec[] = keys.map((key, i) => {
        const deps = new Set<string>();
        const n = i === 0 ? 0 : Math.floor(random() * 3);
        for (let d = 0; d < n; d++) deps.add(keys[Math.floor(random() * i)]);
        return [key, random() < 0.15 ? "in_progress" : "todo", [...deps]];
      });
      const topology = computeGraphTopology(tickets(specs));
      const crossings = crossingsOf(topology);
      // Swaps stay within a component and within a group (held or not in
      // Ready, linked or not elsewhere); a waypoint goes with its blocker.
      const component = new Map(topology.nodes.map((n) => [n.id, n.id]));
      const find = (id: string): string => (component.get(id) === id ? id : find(component.get(id)!));
      for (const e of topology.edges) component.set(find(e.to), find(e.from));
      const cardOf = (entry: LayerEntry) => (entry.kind === "card" ? entry.id : topology.edges[entry.edge].from);
      const linked = (id: string) => topology.edges.some((e) => e.from === id || e.to === id);
      const group = (entry: LayerEntry, c: number) => {
        const id = cardOf(entry);
        const kind = entry.kind === "waypoint" ? true : c === 0 ? topology.nodes.find((n) => n.id === id)!.active : linked(id);
        return `${find(id)} ${kind}`;
      };
      topology.layers.forEach((layer, c) =>
        layer.forEach((entry, i) => {
          const next = layer[i + 1];
          if (!next || group(entry, c) !== group(next, c)) return;
          swapsTried++;
          const layers = topology.layers.map((l) => [...l]);
          [layers[c][i], layers[c][i + 1]] = [next, entry];
          expect(crossingsOf({ ...topology, layers }), JSON.stringify(specs)).toBeGreaterThanOrEqual(crossings);
        }),
      );
    }
    expect(swapsTried).toBeGreaterThan(500);
  });

  it("gives identical output whatever the order of the input and of dependsOn", () => {
    const random = mulberry32(7);
    const base = tickets([
      ["B-1", "todo"],
      ["A-1", "in_progress"],
      ["A-2", "todo", ["A-1", "B-1"]],
      ["A-3", "todo", ["A-2", "A-5"]],
      ["A-4", "in_progress", ["A-3"]],
      ["A-5", "todo", ["A-4"]],
      ["A-6", "todo", ["A-6", "A-7"]],
      ["A-7", "done"],
      ["B-2", "todo", ["B-3"]],
      ["B-3", "todo", ["B-2", "A-1"]],
      ["B-4", "todo", ["A-2", "B-1", "A-1"]],
      ["C-1", "in_progress"],
      ["C-2", "todo", ["C-1", "B-4"]],
    ]);
    // Each node carries its input ticket, whose dependsOn order is shuffled
    // too, so compare the layout's own fields.
    const withoutTickets = (topology: GraphTopology) => ({
      ...topology,
      nodes: topology.nodes.map(({ ticket, ...node }) => ({ ...node, ticketId: ticket.id })),
    });
    const expected = withoutTickets(computeGraphTopology(base));
    expect(expected.edges.some((e) => e.back)).toBe(true);
    expect(expected.columns.length).toBeGreaterThan(2);
    for (let i = 0; i < 20; i++) {
      const input = shuffled(base, random).map((t) => ({ ...t, dependsOn: shuffled(t.dependsOn ?? [], random) }));
      expect(withoutTickets(computeGraphTopology(input))).toEqual(expected);
    }
  });

  it("lays out 500 tickets with random dependencies and cycles quickly", () => {
    const random = mulberry32(42);
    const count = 500;
    const keys = Array.from({ length: count }, (_, i) => `P-${i + 1}`);
    const specs: Spec[] = keys.map((key, i) => {
      const deps = new Set<string>();
      const n = Math.floor(random() * 4);
      for (let d = 0; d < n; d++) {
        // Mostly earlier tickets, so the graph is deep; sometimes any ticket,
        // which makes cycles.
        const j = random() < 0.9 ? Math.floor(random() * Math.max(i, 1)) : Math.floor(random() * count);
        deps.add(keys[j]);
      }
      const status = random() < 0.1 ? "done" : random() < 0.2 ? "in_progress" : "todo";
      return [key, status, [...deps]];
    });
    // Guarantee a few explicit cycles on top of any random ones.
    specs[10][2]!.push("P-12");
    specs[11][2]!.push("P-11");
    specs[20][2]!.push("P-21");
    specs[99][2]!.push("P-100");
    specs[100][2]!.push("P-99");
    for (const index of [10, 11, 20, 99, 100]) specs[index][1] = "todo";
    const input = tickets(specs);

    const started = performance.now();
    const layout = layoutGraph(input);
    const elapsed = performance.now() - started;
    expect(elapsed).toBeLessThan(1000);

    const open = input.filter((t) => !isDone(t.status)).length;
    expect(layout.nodes).toHaveLength(open);
    expect(layout.columns.reduce((sum, c) => sum + c.count, 0)).toBe(open);
    expect(layout.edges.filter((e) => e.back).length).toBeGreaterThanOrEqual(3);
    const column = new Map(layout.nodes.map((n) => [n.id, n.column]));
    for (const edge of layout.edges) {
      if (!edge.back) expect(column.get(edge.to)!).toBeGreaterThan(column.get(edge.from)!);
    }
    for (const node of layout.nodes) {
      expect(Number.isFinite(node.x) && Number.isFinite(node.y)).toBe(true);
    }
    const ready = layout.nodes.filter((n) => n.column === 0 && !n.inGrid).sort((a, b) => a.row - b.row);
    expect(ready.some((n) => n.active)).toBe(true);
    // No active ticket sits below one that isn't.
    expect(ready.every((n, i) => i === 0 || !n.active || ready[i - 1].active)).toBe(true);
  });

  it("holds its invariants across random small graphs with cycles, done tickets and external blockers", () => {
    const random = mulberry32(2026);
    const pick = <T,>(items: readonly T[]) => items[Math.floor(random() * items.length)];
    let backEdges = 0;
    let readyInCycles = 0;
    let externallyBlocked = 0;

    for (let graph = 0; graph < 400; graph++) {
      const size = 1 + Math.floor(random() * 8);
      const keys = Array.from({ length: size }, (_, i) => `A-${i + 1}`);
      const outside = { "X-1": pick(["todo", "done"]), "X-2": pick(["todo", "in_progress", "done"]) };
      const specs: Spec[] = keys.map((key) => {
        const deps = Array.from({ length: Math.floor(random() * 4) }, () => pick(keys));
        if (random() < 0.25) deps.push(pick(["X-1", "X-2"]));
        return [key, pick(["todo", "todo", "in_progress", "done"]), deps];
      });
      const input = tickets(specs, outside);
      const topology = computeGraphTopology(input);
      const model = referenceModel(input);
      const { ids, refs, blockers, external } = model;
      const column = columnOf(topology);
      const byId = Object.fromEntries(topology.nodes.map((n) => [n.id, n]));
      const back = new Set(topology.edges.filter((e) => e.back).map((e) => `${e.from}->${e.to}`));
      const label = `graph ${graph}: ${JSON.stringify(specs)}`;
      backEdges += back.size;
      externallyBlocked += external.filter((n) => n > 0).length;

      // No false Ready. For a ticket in column 0, take its strongly connected
      // component: itself plus every ticket it both blocks and is blocked by,
      // directly or not. No ticket in that component may have an external
      // blocker, and every in-input blocker of a ticket in it must be inside
      // it. So a Ready ticket waits on nothing unfinished, either itself or
      // through any cycle it sits on.
      const violations: string[] = [];
      ids.forEach((id, v) => {
        if (column[id] !== 0) return;
        const component = model.componentOf(v);
        if (component.length > 1) readyInCycles++;
        for (const w of component) {
          if (external[w] > 0) violations.push(`${id} in Ready, but ${ids[w]} has an external blocker`);
          for (const u of blockers[w]) {
            if (!component.includes(u)) violations.push(`${id} in Ready, but ${ids[w]} waits on ${ids[u]}`);
          }
        }
      });
      expect(violations, label).toEqual([]);

      // The back edges are exactly the edges into a ticket on the search
      // stack, for a search that starts only in source components, in order
      // of their lowest ticket, from the lowest ticket with an external
      // blocker or else the lowest, and follows edges in ticket order.
      expect([...back].sort(), label).toEqual(model.referenceBackEdges().sort());

      // dependencyTotal is every distinct ref, whatever kind it turns out to
      // be: done, an edge (back edges included), or hidden.
      ids.forEach((id, v) => {
        expect(byId[id].dependencyTotal, `${label} total of ${id}`).toBe(refs[v].length);
      });

      // Every other edge runs left to right, and each column is the longest
      // path in, raised to 1 by an external blocker.
      ids.forEach((id, v) => {
        const incoming = blockers[v].filter((u) => !back.has(`${ids[u]}->${id}`)).map((u) => column[ids[u]] + 1);
        expect(column[id], `${label} column of ${id}`).toBe(Math.max(external[v] > 0 ? 1 : 0, ...incoming));
      });
    }

    // The generator really did exercise the cases above.
    expect(backEdges).toBeGreaterThan(100);
    expect(readyInCycles).toBeGreaterThan(20);
    expect(externallyBlocked).toBeGreaterThan(50);
  });
});

describe("positionGraph", () => {
  const byId = (layout: GraphLayout) => new Map(layout.nodes.map((n) => [n.id, n]));
  const centre = (n: PositionedNode) => n.y + n.height / 2;
  const bottom = (n: PositionedNode) => n.y + n.height;

  it("lines a chain up on its cards' centres, whatever their measured heights", () => {
    const sizes = new Map([
      ["A-1", { width: 200, height: 50 }],
      ["A-2", { width: 240, height: 120 }],
      ["A-3", { width: 180, height: 70 }],
    ]);
    const layout = positionGraph(
      computeGraphTopology(
        tickets([
          ["A-1", "todo"],
          ["A-2", "todo", ["A-1"]],
          ["A-3", "todo", ["A-2"]],
        ]),
      ),
      { sizes, defaultSize: { width: 100, height: 40 }, columnGap: 60, rowGap: 10, origin: { x: 5, y: 30 } },
    );
    const box = Object.fromEntries(layout.nodes.map((n) => [n.id, [n.x, n.y, n.width, n.height]]));
    // A-2 is the tallest, so it starts at the origin and the others centre on it.
    expect(box).toEqual({
      "A-1": [5, 65, 200, 50],
      "A-2": [265, 30, 240, 120],
      "A-3": [565, 55, 180, 70],
    });
    expect(layout.columns).toEqual([
      { index: 0, x: 5, width: 200, count: 1 },
      { index: 1, x: 265, width: 240, count: 1 },
      { index: 2, x: 565, width: 180, count: 1 },
    ]);
    expect(layout.width).toBe(745);
    expect(layout.height).toBe(150);
    expect(layout.grid).toBeNull();
    // A single edge end leaves or enters the middle of the side.
    expect(layout.edges).toEqual([
      { from: "A-1", to: "A-2", back: false, start: { x: 205, y: 90 }, end: { x: 265, y: 90 }, via: [] },
      { from: "A-2", to: "A-3", back: false, start: { x: 505, y: 90 }, end: { x: 565, y: 90 }, via: [] },
    ]);
    // Topology fields ride along on each positioned node.
    expect(layout.nodes[0]).toMatchObject({ id: "A-1", column: 0, row: 0, inGrid: false });
  });

  it("gives each gap its own width when columnGaps says so, and columnGap to the rest", () => {
    const topology = computeGraphTopology(
      tickets([
        ["A-1", "todo"],
        ["A-2", "todo", ["A-1"]],
        ["A-3", "todo", ["A-2"]],
        ["A-4", "todo", ["A-3"]],
      ]),
    );
    const layout = positionGraph(topology, {
      defaultSize: { width: 100, height: 40 },
      columnGap: 60,
      columnGaps: [60, 150],
      origin: { x: 5, y: 0 },
    });
    // Only the middle gap is wide; the last one has no entry and takes columnGap.
    expect(layout.columns.map((c) => c.x)).toEqual([5, 165, 415, 575]);
    expect(layout.width).toBe(675);
    const edge = layout.edges.find((e) => e.from === "A-2")!;
    expect([edge.start.x, edge.end.x]).toEqual([265, 415]);
  });

  it("slots the grid under the columns a per-gap layout would have, where there are none", () => {
    // One linked pair makes two columns; the grid's third slot is where a
    // third column would start, past the second gap's own width.
    const topology = computeGraphTopology(
      tickets([
        ["A-1", "todo"],
        ["A-2", "todo", ["A-1"]],
        ["A-3", "todo"],
        ["A-4", "todo"],
        ["A-5", "todo"],
      ]),
    );
    const layout = positionGraph(topology, {
      defaultSize: { width: 100, height: 40 },
      columnGap: 60,
      columnGaps: [70, 90],
    });
    const xs = layout.nodes.filter((n) => n.inGrid).map((n) => n.x);
    expect(xs).toEqual([0, 170, 360]);
  });

  it("puts a card at the median of the cards linked to it, and packs those around it", () => {
    const layout = layoutGraph(
      tickets([
        ["A-1", "todo"],
        ["A-2", "todo"],
        ["A-3", "todo"],
        ["A-4", "todo", ["A-1", "A-2", "A-3"]],
      ]),
    );
    const n = byId(layout);
    expect(centre(n.get("A-4")!)).toBe(centre(n.get("A-2")!));
    expect(n.get("A-2")!.y - bottom(n.get("A-1")!)).toBe(DEFAULT_ROW_GAP);
    expect(n.get("A-3")!.y - bottom(n.get("A-2")!)).toBe(DEFAULT_ROW_GAP);
    expect(n.get("A-1")!.y).toBe(0);
  });

  it("keeps the order and rowGap between cards that want one place, and meets them halfway", () => {
    const sizes = new Map([
      ["A-2", { width: 280, height: 60 }],
      ["A-3", { width: 280, height: 100 }],
    ]);
    const layout = layoutGraph(
      tickets([
        ["A-1", "todo"],
        ["A-2", "todo", ["A-1"]],
        ["A-3", "todo", ["A-1"]],
      ]),
      { sizes, rowGap: 10 },
    );
    const n = byId(layout);
    const [a1, a2, a3] = ["A-1", "A-2", "A-3"].map((id) => n.get(id)!);
    expect(a3.y).toBe(bottom(a2) + 10);
    // Least squares: both are as far from A-1's centre as each other.
    expect(Math.abs((centre(a2) + centre(a3)) / 2 - centre(a1))).toBeLessThanOrEqual(1);
    expect(Math.min(a1.y, a2.y)).toBe(0);
  });

  it("stacks held tickets at the top of Ready, linked or not, and a card with only hidden blockers at the bottom of its column", () => {
    const layout = layoutGraph(
      tickets(
        [
          ["A-1", "in_progress"],
          ["A-2", "todo"],
          ["A-3", "todo", ["A-2"]],
          ["A-4", "todo", ["X-1"]],
          ["A-5", "todo", ["A-2"]],
        ],
        { "X-1": "todo" },
      ),
    );
    const n = byId(layout);
    const [a1, a2, a3, a4, a5] = ["A-1", "A-2", "A-3", "A-4", "A-5"].map((id) => n.get(id)!);
    expect(layout.grid).toBeNull();
    // A-1 is linked to nothing and still tops Ready, right above A-2.
    expect(a1.y).toBe(0);
    expect(a2.y).toBe(bottom(a1) + DEFAULT_ROW_GAP);
    // A-4 sits right below the cards A-2 links to.
    expect(a3.y).toBeLessThan(a5.y);
    expect(a4.y).toBe(bottom(a5) + DEFAULT_ROW_GAP);
    expect(a2.x).toBe(a1.x);
  });

  it("lays unlinked Ready tickets out three across below the graph, under the first columns", () => {
    const layout = layoutGraph(
      tickets([
        ["A-1", "in_progress"],
        ["A-2", "todo"],
        ["A-3", "todo", ["A-2"]],
        ["A-4", "todo"],
        ["A-5", "todo"],
        ["A-6", "todo"],
        ["A-7", "todo"],
        ["A-8", "todo"],
      ]),
    );
    const { width, height } = DEFAULT_NODE_SIZE;
    const n = byId(layout);
    const lowest = Math.max(bottom(n.get("A-2")!), bottom(n.get("A-3")!));
    const top = lowest + DEFAULT_GRID_GAP;
    const column1 = width + DEFAULT_COLUMN_GAP;
    // There is no third column, so its slot is where one would be.
    const slots = [0, column1, 2 * column1];
    const rows = [top, top + height + DEFAULT_ROW_GAP];
    expect(["A-4", "A-5", "A-6", "A-7", "A-8"].map((id) => [n.get(id)!.x, n.get(id)!.y])).toEqual([
      [slots[0], rows[0]],
      [slots[1], rows[0]],
      [slots[2], rows[0]],
      [slots[0], rows[1]],
      [slots[1], rows[1]],
    ]);
    expect(layout.grid).toEqual({ x: 0, y: top, width: 2 * column1 + width, height: 2 * height + DEFAULT_ROW_GAP, count: 5 });
    // Ready counts the grid too; the grid's cards come last, so Tab reaches
    // them after the graph.
    expect(layout.columns.map((c) => c.count)).toEqual([7, 1]);
    expect(layout.nodes.map((node) => node.id)).toEqual(["A-1", "A-2", "A-3", "A-4", "A-5", "A-6", "A-7", "A-8"]);
    expect(layout.nodes.filter((node) => node.inGrid).map((node) => node.row)).toEqual([0, 1, 2, 3, 4]);
    // The canvas reaches the grid's right and bottom edges.
    expect(layout.width).toBe(2 * column1 + width);
    expect(layout.height).toBe(rows[1] + height);
  });

  it("starts the grid below the origin when nothing is linked, and takes the gap it is given", () => {
    const layout = layoutGraph(
      tickets([
        ["A-1", "todo"],
        ["A-2", "todo"],
      ]),
      { origin: { x: 12, y: 40 }, gridGap: 30 },
    );
    expect(layout.grid).toMatchObject({ x: 12, y: 70, count: 2 });
    expect(layout.nodes.map((n) => [n.x, n.y])).toEqual([
      [12, 70],
      [12 + DEFAULT_NODE_SIZE.width + DEFAULT_COLUMN_GAP, 70],
    ]);
    expect(layout.columns).toEqual([{ index: 0, x: 12, width: DEFAULT_NODE_SIZE.width, count: 2 }]);
  });

  it("spreads edge ends down a card's side by the other end's height, back edges at the top", () => {
    const layout = layoutGraph(
      tickets([
        ["A-1", "todo"],
        ["A-2", "todo"],
        ["A-3", "todo"],
        ["A-4", "todo", ["A-1", "A-2", "A-3"]],
        ["B-1", "todo", ["B-2"]],
        ["B-2", "todo", ["B-1"]],
        ["B-3", "todo", ["B-2"]],
      ]),
    );
    const n = byId(layout);
    const edge = (from: string, to: string) => layout.edges.find((e) => e.from === from && e.to === to)!;
    const a4 = n.get("A-4")!;
    const quarter = a4.height / 4;
    // Three ends into A-4, from the highest blocker to the lowest.
    expect(["A-1", "A-2", "A-3"].map((id) => edge(id, "A-4").end.y)).toEqual([a4.y + quarter, a4.y + 2 * quarter, a4.y + 3 * quarter]);
    // One end on a side leaves from the middle.
    for (const id of ["A-1", "A-2", "A-3"]) expect(edge(id, "A-4").start.y).toBe(centre(n.get(id)!));
    // B-2 -> B-1 closes the cycle and turns up to its lane, so it leaves
    // B-2 above the forward edge to B-3.
    const b2 = n.get("B-2")!;
    expect(edge("B-2", "B-1").back).toBe(true);
    expect(edge("B-2", "B-1").start).toEqual({ x: b2.x + b2.width, y: b2.y + b2.height / 4 });
    expect(edge("B-2", "B-3").start).toEqual({ x: b2.x + b2.width, y: b2.y + (3 * b2.height) / 4 });
  });

  it("runs a long forward edge through a gap of its own in every column it crosses", () => {
    // A-1 -> A-4 crosses columns 1 and 2, which the chain A-1 -> A-2 -> A-3
    // and the chain B-1 -> B-2 -> B-3 fill.
    const topology = computeGraphTopology(
      tickets([
        ["A-1", "todo"],
        ["A-2", "todo", ["A-1"]],
        ["A-3", "todo", ["A-2"]],
        ["A-4", "todo", ["A-3", "A-1", "B-3"]],
        ["B-1", "todo"],
        ["B-2", "todo", ["B-1"]],
        ["B-3", "todo", ["B-2"]],
      ]),
    );
    const layout = positionGraph(topology);
    const long = layout.edges.find((e) => e.from === "A-1" && e.to === "A-4")!;
    expect(long.via).toHaveLength(4);
    [1, 2].forEach((c, i) => {
      const column = layout.columns[c];
      const [entry, exit] = long.via.slice(2 * i, 2 * i + 2);
      expect([entry.x, exit.x]).toEqual([column.x, column.x + column.width]);
      expect(exit.y).toBe(entry.y);
      // Clear of every card in the column by at least rowGap.
      for (const node of layout.nodes.filter((node) => node.column === c)) {
        expect(entry.y <= node.y - DEFAULT_ROW_GAP || entry.y >= bottom(node) + DEFAULT_ROW_GAP, node.id).toBe(true);
      }
    });
    // An edge to the next column has no waypoints.
    expect(layout.edges.find((e) => e.from === "A-1" && e.to === "A-2")!.via).toEqual([]);
  });

  it("uses the defaults when no options are given", () => {
    const layout = layoutGraph(
      tickets([
        ["A-1", "todo"],
        ["A-2", "todo"],
        ["A-3", "todo", ["A-1"]],
        ["A-4", "todo", ["A-2"]],
      ]),
    );
    const { width, height } = DEFAULT_NODE_SIZE;
    const n = byId(layout);
    expect([n.get("A-2")!.x, n.get("A-2")!.y]).toEqual([0, height + DEFAULT_ROW_GAP]);
    expect([n.get("A-3")!.x, n.get("A-3")!.y]).toEqual([width + DEFAULT_COLUMN_GAP, 0]);
    expect(layout.width).toBe(2 * width + DEFAULT_COLUMN_GAP);
    expect(layout.height).toBe(2 * height + DEFAULT_ROW_GAP);
  });

  it("joins a back edge from the blocker's right edge to the blocked card's left edge", () => {
    const layout = layoutGraph(
      tickets([
        ["A-1", "todo", ["A-2"]],
        ["A-2", "todo", ["A-1"]],
        ["A-3", "todo", ["A-3"]],
      ]),
    );
    const { width, height } = DEFAULT_NODE_SIZE;
    const column1 = width + DEFAULT_COLUMN_GAP;
    const rows = height + DEFAULT_ROW_GAP;
    expect(layout.edges).toEqual([
      { from: "A-1", to: "A-2", back: false, start: { x: width, y: height / 2 }, end: { x: column1, y: height / 2 }, via: [] },
      { from: "A-2", to: "A-1", back: true, start: { x: column1 + width, y: height / 2 }, end: { x: 0, y: height / 2 }, via: [] },
      { from: "A-3", to: "A-3", back: true, start: { x: width, y: rows + height / 2 }, end: { x: 0, y: rows + height / 2 }, via: [] },
    ]);
  });

  it("gives an empty Ready column the default width", () => {
    const layout = layoutGraph(tickets([["A-1", "todo", ["X-1"]]], { "X-1": "todo" }), { columnGap: 20 });
    expect(layout.columns[0]).toEqual({ index: 0, x: 0, width: DEFAULT_NODE_SIZE.width, count: 0 });
    expect(layout.nodes[0].x).toBe(DEFAULT_NODE_SIZE.width + 20);
  });

  it("returns an empty layout for no tickets", () => {
    expect(layoutGraph([], { origin: { x: 4, y: 8 } })).toEqual({ nodes: [], edges: [], columns: [], grid: null, width: 4, height: 8 });
  });
});

describe("layout stability", () => {
  // Board-like graphs: a few chains and fans with long edges, a cycle, held
  // tickets, hidden blockers and a crowd of unlinked Ready tickets.
  function board(random: () => number, size: number, prefix = "A"): Spec[] {
    const keys = Array.from({ length: size }, (_, i) => `${prefix}-${i + 1}`);
    return keys.map((key, i) => {
      const deps = new Set<string>();
      if (random() < 0.6 && i > 0) {
        const n = 1 + Math.floor(random() * 2);
        for (let d = 0; d < n; d++) deps.add(keys[Math.floor(random() * i)]);
      }
      if (random() < 0.05) deps.add("X-1");
      if (random() < 0.03 && i + 1 < size) deps.add(keys[i + 1]);
      const status = random() < 0.1 ? "in_progress" : random() < 0.05 ? "agent_review" : "todo";
      return [key, status, [...deps]];
    });
  }
  const outside = { "X-1": "todo" };
  const strip = (layout: GraphLayout) => ({
    ...layout,
    nodes: layout.nodes.map(({ ticket, ...node }) => ({ ...node, ticketId: ticket.id })),
  });

  it("gives identical positions whatever the order of the input and of dependsOn", () => {
    const random = mulberry32(11);
    for (let graph = 0; graph < 20; graph++) {
      const base = tickets(board(random, 30), outside);
      const expected = strip(layoutGraph(base));
      const input = shuffled(base, random).map((t) => ({ ...t, dependsOn: shuffled(t.dependsOn ?? [], random) }));
      expect(strip(layoutGraph(input))).toEqual(expected);
    }
  });

  it("moves no card of the graph when an unlinked ticket arrives, which goes to the grid", () => {
    const random = mulberry32(12);
    for (let graph = 0; graph < 20; graph++) {
      const specs = board(random, 30);
      const before = layoutGraph(tickets(specs, outside));
      const after = layoutGraph(tickets([...specs, ["A-99", "todo"]], outside));
      const placed = (layout: GraphLayout) =>
        layout.nodes.filter((n) => !n.inGrid).map((n) => [n.id, n.x, n.y]);
      expect(placed(after)).toEqual(placed(before));
      expect(after.edges).toEqual(before.edges);
      expect(after.grid!.count).toBe((before.grid?.count ?? 0) + 1);
    }
  });

  it("doesn't reorder the cards of other components when a ticket or an edge comes or goes", () => {
    // Two boards side by side, A and B, never linked to each other. A
    // ticket joins A, then one of A's edges goes; B's rows stay as they were.
    const random = mulberry32(13);
    let changed = 0;
    for (let graph = 0; graph < 30; graph++) {
      const a = board(random, 20, "A");
      const b = board(random, 20, "B");
      // A board's cards per column, top to bottom, up to its last column.
      const rowsOf = (specs: Spec[], prefix: string) => {
        const rows = computeGraphTopology(tickets(specs, outside)).columns.map((ids) => ids.filter((id) => id.startsWith(prefix)));
        while (rows.length > 0 && rows[rows.length - 1].length === 0) rows.pop();
        return rows;
      };
      const base = rowsOf([...a, ...b], "B");
      const blocker = a[Math.floor(random() * a.length)][0];
      const grown: Spec[] = [...a, ["A-21", "todo", [blocker]]];
      const linkedIndex = a.findIndex(([, , deps]) => deps!.some((d) => d.startsWith("A-")));
      const shrunk: Spec[] = a.map((spec, i) => (i === linkedIndex ? [spec[0], spec[1], spec[2]!.slice(1)] : spec));
      for (const variant of [grown, shrunk]) {
        const aRows = rowsOf(variant, "A");
        if (JSON.stringify(aRows) !== JSON.stringify(rowsOf(a, "A"))) changed++;
        expect(rowsOf([...variant, ...b], "B")).toEqual(base);
      }
    }
    // The changes really did move A's own cards.
    expect(changed).toBeGreaterThan(10);
  });

  it("lays out about 100 tickets well within a frame", () => {
    const random = mulberry32(100);
    const input = tickets(board(random, 100), outside);
    const times: number[] = [];
    for (let run = 0; run < 25; run++) {
      const started = performance.now();
      layoutGraph(input);
      times.push(performance.now() - started);
    }
    times.sort((a, b) => a - b);
    expect(times[Math.floor(times.length / 2)]).toBeLessThan(16);
  });
});

describe("matchingBounds", () => {
  const cards = [
    { id: "A-1", x: 0, y: 0, width: 256, height: 96 },
    { id: "A-2", x: 0, y: 112, width: 256, height: 140 },
    { id: "A-3", x: 336, y: 40, width: 200, height: 96 },
  ];

  it("covers the cards whose ids match and leaves the rest out", () => {
    expect(matchingBounds(cards, new Set(["A-2", "A-3"]))).toEqual({ x: 0, y: 40, width: 536, height: 212 });
    // One short of the whole set is still a narrower box than the canvas.
    expect(matchingBounds(cards, new Set(["A-1", "A-2"]))).toEqual({ x: 0, y: 0, width: 256, height: 252 });
  });

  it("gives a single match its own card", () => {
    expect(matchingBounds(cards, new Set(["A-3"]))).toEqual({ x: 336, y: 40, width: 200, height: 96 });
  });

  it("answers null with no ids to match, so the caller frames the whole graph", () => {
    expect(matchingBounds(cards, null)).toBeNull();
  });

  it("answers null when no card matches", () => {
    expect(matchingBounds(cards, new Set())).toBeNull();
    expect(matchingBounds(cards, new Set(["Z-9"]))).toBeNull();
    expect(matchingBounds([], new Set(["A-1"]))).toBeNull();
  });

  it("answers null when every card matches, so a no-op filter frames the canvas", () => {
    expect(matchingBounds(cards, new Set(["A-1", "A-2", "A-3"]))).toBeNull();
    // Ids that name no card don't stop the set from covering every card.
    expect(matchingBounds(cards, new Set(["A-1", "A-2", "A-3", "Z-9"]))).toBeNull();
  });

  it("reads a layout's positioned nodes, whose box is not the canvas", () => {
    // The page lays the graph out below the column headers and the back
    // edges' lanes, and leaves a gutter right of the last column, so the
    // cards' box is smaller than the canvas on every side but the left.
    const origin = { x: 12, y: 84 };
    const layout = layoutGraph(
      tickets([
        ["A-1", "todo"],
        ["A-2", "todo"],
        ["A-3", "todo", ["A-1"]],
        ["A-4", "todo", ["A-2"]],
      ]),
      { origin },
    );
    const { width, height } = DEFAULT_NODE_SIZE;
    expect(matchingBounds(layout.nodes, new Set(["A-2", "A-3"]))).toEqual({
      x: origin.x,
      y: origin.y,
      width: 2 * width + DEFAULT_COLUMN_GAP,
      height: 2 * height + DEFAULT_ROW_GAP,
    });
    // Every node matching falls back to null rather than to this box, which
    // starts below the headers and stops short of the right-hand gutter.
    expect(matchingBounds(layout.nodes, new Set(layout.nodes.map((n) => n.id)))).toBeNull();
  });
});
