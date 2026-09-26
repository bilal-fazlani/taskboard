// Which Dependencies-view edges wait only to avoid a conflict. An edge runs
// from the blocker (`from`) to the ticket whose dependsOn names it (`to`);
// the view draws the conflict-only ones dashed and, when it draws any, shows
// the legend that says what a dashed edge means.

import type { DependencyKind } from "../api/client";

interface KindedTicket {
  id: string;
  dependsOn?: readonly { id: string; kind?: DependencyKind }[];
}

/** The key the view uses for the edge from `from` (the blocker) to `to`. */
export function edgeKey(from: string, to: string): string {
  return `${from}->${to}`;
}

/** The keys of every conflict-only dependency the tickets declare. */
export function conflictOnlyEdges(tickets: readonly KindedTicket[]): Set<string> {
  const keys = new Set<string>();
  for (const t of tickets) {
    for (const dep of t.dependsOn ?? []) {
      if (dep.kind === "conflict_only") keys.add(edgeKey(dep.id, t.id));
    }
  }
  return keys;
}

/** The dash pattern of a conflict-only edge, and of its legend sample. */
export const CONFLICT_ONLY_DASH = "5 4";
