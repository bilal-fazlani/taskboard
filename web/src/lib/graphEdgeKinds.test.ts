import { describe, expect, it } from "vitest";
import { conflictOnlyEdges, edgeKey } from "./graphEdgeKinds";

describe("conflictOnlyEdges", () => {
  it("keys each conflict-only dependency from its blocker to the ticket that declares it", () => {
    const keys = conflictOnlyEdges([
      { id: "t3", dependsOn: [{ id: "t1", kind: "conflict_only" }, { id: "t2", kind: "needs_work" }] },
      { id: "t4", dependsOn: [{ id: "t3" }] },
      { id: "t5" },
    ]);
    expect([...keys]).toEqual([edgeKey("t1", "t3")]);
    expect(keys.has(edgeKey("t3", "t1"))).toBe(false);
  });

  it("finds none when every dependency needs work", () => {
    expect(conflictOnlyEdges([{ id: "t2", dependsOn: [{ id: "t1" }] }]).size).toBe(0);
  });
});
